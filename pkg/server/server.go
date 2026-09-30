package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/osint-technologies/golem/pkg/attacks"
	"github.com/osint-technologies/golem/pkg/attacks/neural"
	"github.com/osint-technologies/golem/pkg/budget"
	"github.com/osint-technologies/golem/pkg/core"
	"github.com/osint-technologies/golem/pkg/evaluator"
	"github.com/osint-technologies/golem/pkg/generation"
	"github.com/osint-technologies/golem/pkg/providers"
	"github.com/osint-technologies/golem/pkg/report"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	port      int
	mu        sync.Mutex
	scanState *ScanState
	naeState  *NAEState
	wsClients map[chan []byte]bool
	wsMu      sync.Mutex
}

type ScanState struct {
	Status     string            `json:"status"`
	StartTime  time.Time         `json:"start_time"`
	Progress   int               `json:"progress"`
	Total      int               `json:"total"`
	CurrentLog []LogEntry        `json:"current_log"`
	Result     *core.SuiteResult `json:"result"`
	Config     ScanConfig        `json:"config"`
	Error      string            `json:"error,omitempty"`
}

type NAEState struct {
	Status    string         `json:"status"`
	StartTime time.Time      `json:"start_time"`
	Config    NAEConfig      `json:"config"`
	Events    []neural.Event `json:"events"`
	Result    *neural.Result `json:"result"`
	Error     string         `json:"error,omitempty"`
}

type ScanConfig struct {
	Provider            string   `json:"provider"`
	BaseURL             string   `json:"base_url"`
	Model               string   `json:"model"`
	APIKey              string   `json:"api_key"`
	MaxTokens           int      `json:"max_tokens"`
	Timeout             int      `json:"timeout"`
	Concurrency         int      `json:"concurrency"`
	Categories          []string `json:"categories"`
	CustomPrompts       []string `json:"custom_prompts"`
	Pyramid             bool     `json:"pyramid"`
	Streaming           bool     `json:"streaming"`
	ConfidenceThreshold float64  `json:"confidence_threshold"`
	MutationIntensity   int      `json:"mutation_intensity"`
	MaxAttacksTotal     int      `json:"max_attacks_total"`
	MaxAttacksCategory  int      `json:"max_attacks_category"`
	AttackOrder         string   `json:"attack_order"`
	EarlyStopCritical   bool     `json:"early_stop_critical"`
	RateLimit           int      `json:"rate_limit"`
	RequestDelay        int      `json:"request_delay"`
	MaxRetries          int      `json:"max_retries"`
	ErrorThreshold      int      `json:"error_threshold"`
	Temperature         float64  `json:"temperature"`
	TopP                float64  `json:"top_p"`
	Seed                int      `json:"seed"`
	StopSequences       []string `json:"stop_sequences"`
	MinSeverity         string   `json:"min_severity"`
	AutoExport          bool     `json:"auto_export"`
	IncludeRaw          bool     `json:"include_raw"`
	IncludeFailed       bool     `json:"include_failed"`
}

type NAEConfig struct {
	Target   ModelConfig `json:"target"`
	Attacker ModelConfig `json:"attacker"`
	Analyzer ModelConfig `json:"analyzer"`

	Goal      string `json:"goal"`
	MaxRounds int    `json:"max_rounds"`

	SuccessThreshold     float64 `json:"success_threshold"`
	AttackerSystemPrompt string  `json:"attacker_system_prompt"`
	AnalyzerSystemPrompt string  `json:"analyzer_system_prompt"`
}

type ModelConfig struct {
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key"`
	MaxTokens   int     `json:"max_tokens"`
	Timeout     int     `json:"timeout"`
	Streaming   bool    `json:"streaming"`
	Temperature float64 `json:"temperature"`
}

type LogEntry struct {
	ID         int     `json:"id"`
	Timestamp  string  `json:"timestamp"`
	Technique  string  `json:"technique"`
	Category   string  `json:"category"`
	Prompt     string  `json:"prompt"`
	Response   string  `json:"response"`
	Success    bool    `json:"success"`
	Confidence float64 `json:"confidence"`
	Duration   string  `json:"duration"`
	Status     string  `json:"status"`
}

type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func New(port int) *Server {
	return &Server{
		port:      port,
		scanState: &ScanState{Status: "idle"},
		naeState:  &NAEState{Status: "idle"},
		wsClients: make(map[chan []byte]bool),
	}
}

func (s *Server) Start(openBrowser bool) error {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to init static fs: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/scan/start", s.handleStartScan)
	mux.HandleFunc("/api/scan/stop", s.handleStopScan)
	mux.HandleFunc("/api/test-connection", s.handleTestConnection)
	mux.HandleFunc("/api/report", s.handleReport)
	mux.HandleFunc("/api/ws", s.handleWebSocket)

	mux.HandleFunc("/api/nae/start", s.handleStartNAE)
	mux.HandleFunc("/api/nae/stop", s.handleStopNAE)
	mux.HandleFunc("/api/nae/status", s.handleNAEStatus)

	addr := fmt.Sprintf(":%d", s.port)
	url := fmt.Sprintf("http://localhost:%d", s.port)

	fmt.Printf("\n[golem] Web GUI\n")
	fmt.Printf("------------------------------------------------\n")
	fmt.Printf("[+] Server running on: %s\n", url)
	fmt.Printf("[!] Press Ctrl+C to stop\n\n")

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\n[!] Shutting down...")
		if scanCancel != nil {
			scanCancel()
		}
		if naeCancel != nil {
			naeCancel()
		}
		os.Exit(0)
	}()

	if openBrowser {
		go openURL(url)
	}

	return http.ListenAndServe(addr, mux)
}

func openURL(url string) {
	time.Sleep(500 * time.Millisecond)
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}

func (s *Server) broadcast(msg WSMessage) {
	data, _ := json.Marshal(msg)
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for ch := range s.wsClients {
		select {
		case ch <- data:
		default:
		}
	}
}

var scanCancel context.CancelFunc
var naeCancel context.CancelFunc

func estimateTotal(cfg ScanConfig) int {
	perCategory := map[string]int{
		"injection": 45, "jailbreak": 35, "extraction": 25,
		"encoding": 20, "hallucination": 10, "language": 10,
		"manyshot": 5, "crescendo": 8, "cipher": 8,
		"asciiart": 5, "sidechannel": 12, "toolabuse": 13,
	}
	total := 0
	for _, cat := range cfg.Categories {
		if n, ok := perCategory[cat]; ok {
			total += n
		}
	}
	total += len(cfg.CustomPrompts)
	if total == 0 {
		total = 50
	}
	return total
}

func (s *Server) runScan(ctx context.Context, cancel context.CancelFunc, cfg ScanConfig) {
	defer cancel()
	totalEstimate := estimateTotal(cfg)

	s.mu.Lock()
	s.scanState.Status = "running"
	s.scanState.StartTime = time.Now()
	s.scanState.CurrentLog = nil
	s.scanState.Result = nil
	s.scanState.Error = ""
	s.scanState.Config = cfg
	s.scanState.Total = totalEstimate
	s.scanState.Progress = 0
	s.mu.Unlock()

	s.broadcast(WSMessage{Type: "status", Data: "running"})

	var provider providers.LLMProvider
	if cfg.BaseURL != "" {
		provider = providers.NewOpenAICompatible(cfg.BaseURL, cfg.APIKey, cfg.Model)
	} else {
		provider = providers.NewOpenAI(cfg.APIKey, cfg.Model)
	}
	if cfg.MaxTokens > 0 {
		if setter, ok := provider.(interface{ SetMaxTokens(int) }); ok {
			setter.SetMaxTokens(cfg.MaxTokens)
		}
	}
	if cfg.Timeout > 0 {
		if setter, ok := provider.(interface{ SetTimeout(time.Duration) }); ok {
			setter.SetTimeout(time.Duration(cfg.Timeout) * time.Second)
		}
	}
	if setter, ok := provider.(interface{ SetStreaming(bool) }); ok {
		setter.SetStreaming(cfg.Streaming)
	}
	defer provider.Close()

	eval := evaluator.NewHeuristicEvaluator()
	tracker := budget.NewTracker(0)
	te := generation.NewTemplateEngine()
	me := generation.NewMutationEngine()

	engine := attacks.NewEngine(provider, eval, tracker, te, me)
	if cfg.Concurrency > 0 {
		engine.SetConcurrency(cfg.Concurrency)
	}
	if cfg.Pyramid {
		engine.SetPyramidMode(true)
	}

	engine.SetAllowedCategories(cfg.Categories)

	logID := 0
	engine.SetVerbose(true)
	engine.SetLogFunc(func(technique, prompt, response string, success bool, confidence float64) {
		logID++
		status := "safe"
		if success {
			status = "hit"
		}
		if len(response) > 6 && response[:6] == "ERROR:" {
			status = "error"
		}
		entry := LogEntry{
			ID:        logID,
			Timestamp: time.Since(s.scanState.StartTime).Round(time.Second).String(),
			Technique: technique, Prompt: prompt, Response: response,
			Success: success, Confidence: confidence, Status: status,
		}
		s.mu.Lock()
		s.scanState.CurrentLog = append(s.scanState.CurrentLog, entry)
		s.scanState.Progress = logID
		if logID > s.scanState.Total {
			s.scanState.Total = logID + 10
		}
		currentTotal := s.scanState.Total
		s.mu.Unlock()
		s.broadcast(WSMessage{Type: "log", Data: entry})
		s.broadcast(WSMessage{Type: "progress", Data: map[string]int{
			"current": logID, "total": currentTotal,
		}})
	})

	profile := "standard"
	if len(cfg.Categories) <= 3 {
		profile = "quick"
	}

	result, err := engine.RunSuite(ctx, profile, func(r core.AttackResult) {})

	s.mu.Lock()
	if err != nil {
		s.scanState.Status = "error"
		s.scanState.Error = err.Error()
	} else {
		s.scanState.Status = "done"
		s.scanState.Result = result
		if result != nil {
			s.scanState.Total = result.TotalAttacks
		}
	}
	s.mu.Unlock()

	if result != nil {
		s.broadcast(WSMessage{Type: "done", Data: result})
	} else {
		s.broadcast(WSMessage{Type: "error", Data: err.Error()})
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 100)
	s.wsMu.Lock()
	s.wsClients[ch] = true
	s.wsMu.Unlock()
	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, ch)
		s.wsMu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var cfg ScanConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.Lock()
	if s.scanState.Status == "running" {
		s.mu.Unlock()
		http.Error(w, "scan already running", 409)
		return
	}
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	scanCancel = cancel
	go s.runScan(ctx, cancel, cfg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) handleStopScan(w http.ResponseWriter, r *http.Request) {
	if scanCancel != nil {
		scanCancel()
	}
	s.mu.Lock()
	s.scanState.Status = "idle"
	s.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.scanState)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var cfg ScanConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var provider providers.LLMProvider
	if cfg.BaseURL != "" {
		provider = providers.NewOpenAICompatible(cfg.BaseURL, cfg.APIKey, cfg.Model)
	} else {
		provider = providers.NewOpenAI(cfg.APIKey, cfg.Model)
	}
	defer provider.Close()
	start := time.Now()
	resp, err := provider.Chat(context.Background(), []providers.Message{
		{Role: "user", Content: "Say OK"},
	})
	result := map[string]interface{}{
		"success": err == nil,
		"latency": time.Since(start).Round(time.Millisecond).String(),
	}
	if err != nil {
		result["error"] = err.Error()
	} else {
		content := resp.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		result["response"] = strings.TrimSpace(content)
		result["model"] = resp.Model
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	result := s.scanState.Result
	s.mu.Unlock()
	if result == nil {
		http.Error(w, "no results", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	report.GenerateHTMLToWriter(result, w)
}

func (s *Server) handleStartNAE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var cfg NAEConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.Lock()
	if s.naeState.Status == "running" {
		s.mu.Unlock()
		http.Error(w, "NAE already running", 409)
		return
	}
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	naeCancel = cancel
	go s.runNAE(ctx, cancel, cfg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) handleStopNAE(w http.ResponseWriter, r *http.Request) {
	if naeCancel != nil {
		naeCancel()
	}
	s.mu.Lock()
	s.naeState.Status = "idle"
	s.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (s *Server) handleNAEStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.naeState)
}

func createProviderFromModelConfig(mc ModelConfig) (providers.LLMProvider, error) {
	switch mc.Provider {
	case "openai":
		apiKey := mc.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return providers.NewOpenAI(apiKey, mc.Model), nil
	case "anthropic":
		apiKey := mc.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		return providers.NewAnthropic(apiKey, mc.Model), nil
	case "ollama":
		url := mc.BaseURL
		if url == "" {
			url = "http://localhost:11434"
		}
		url = strings.TrimSuffix(url, "/v1")
		url = strings.TrimSuffix(url, "/")
		return providers.NewOllama(url, mc.Model), nil
	case "custom":
		fallthrough
	default:
		return providers.NewOpenAICompatible(mc.BaseURL, mc.APIKey, mc.Model), nil
	}
}

func (s *Server) runNAE(ctx context.Context, cancel context.CancelFunc, cfg NAEConfig) {
	defer cancel()

	s.mu.Lock()
	s.naeState.Status = "running"
	s.naeState.StartTime = time.Now()
	s.naeState.Config = cfg
	s.naeState.Events = nil
	s.naeState.Result = nil
	s.naeState.Error = ""
	s.mu.Unlock()

	s.broadcast(WSMessage{Type: "nae_status", Data: "running"})

	target, err := createProviderFromModelConfig(cfg.Target)
	if err != nil {
		s.handleNAEFail(err.Error())
		return
	}
	defer target.Close()

	attacker, err := createProviderFromModelConfig(cfg.Attacker)
	if err != nil {
		s.handleNAEFail(err.Error())
		return
	}
	defer attacker.Close()

	analyzer, err := createProviderFromModelConfig(cfg.Analyzer)
	if err != nil {
		s.handleNAEFail(err.Error())
		return
	}
	defer analyzer.Close()

	targetSettings := neural.ModelSettings{
		MaxTokens: cfg.Target.MaxTokens,
		Timeout:   cfg.Target.Timeout,
		Streaming: cfg.Target.Streaming,
	}
	attackerSettings := neural.ModelSettings{
		MaxTokens: cfg.Attacker.MaxTokens,
		Timeout:   cfg.Attacker.Timeout,
		Streaming: cfg.Attacker.Streaming,
	}
	analyzerSettings := neural.ModelSettings{
		MaxTokens: cfg.Analyzer.MaxTokens,
		Timeout:   cfg.Analyzer.Timeout,
		Streaming: cfg.Analyzer.Streaming,
	}

	engineCfg := neural.Config{
		MaxRounds:               cfg.MaxRounds,
		Goal:                    cfg.Goal,
		SuccessThreshold:        cfg.SuccessThreshold,
		SystemPromptForAttacker: cfg.AttackerSystemPrompt,
		SystemPromptForAnalyzer: cfg.AnalyzerSystemPrompt,
	}

	engine := neural.NewEngine(
		target, attacker, analyzer,
		targetSettings, attackerSettings, analyzerSettings,
		engineCfg,
	)

	engine.SetLogFunc(func(evt neural.Event) {
		s.mu.Lock()
		s.naeState.Events = append(s.naeState.Events, evt)
		s.mu.Unlock()
		s.broadcast(WSMessage{Type: "nae_event", Data: evt})
	})

	result, err := engine.Run(ctx)

	s.mu.Lock()
	if err != nil && err != context.Canceled {
		s.naeState.Status = "error"
		s.naeState.Error = err.Error()
	} else {
		s.naeState.Status = "done"
		s.naeState.Result = result
	}
	s.mu.Unlock()

	if result != nil {
		s.broadcast(WSMessage{Type: "nae_done", Data: result})
	}
}

func (s *Server) handleNAEFail(errMsg string) {
	s.mu.Lock()
	s.naeState.Status = "error"
	s.naeState.Error = errMsg
	s.mu.Unlock()
	s.broadcast(WSMessage{Type: "nae_error", Data: errMsg})
}
