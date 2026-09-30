package attacks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/osint-technologies/golem/pkg/budget"
	"github.com/osint-technologies/golem/pkg/core"
	"github.com/osint-technologies/golem/pkg/evaluator"
	"github.com/osint-technologies/golem/pkg/generation"
	"github.com/osint-technologies/golem/pkg/providers"
)

type FindingCallback func(core.AttackResult)

type Engine struct {
	provider          providers.LLMProvider
	evaluator         *evaluator.HeuristicEvaluator
	tracker           *budget.Tracker
	templates         *generation.TemplateEngine
	mutator           *generation.MutationEngine
	verbose           bool
	logFunc           func(technique, prompt, response string, success bool, confidence float64)
	concurrency       int
	pyramidMode       bool
	maxErrors         int
	allowedCategories []string
}

func NewEngine(
	provider providers.LLMProvider,
	eval *evaluator.HeuristicEvaluator,
	tracker *budget.Tracker,
	templates *generation.TemplateEngine,
	mutator *generation.MutationEngine,
) *Engine {
	concurrency := 5
	if r, ok := provider.(interface{ IsReasoning() bool }); ok && r.IsReasoning() {
		concurrency = 1
	}

	return &Engine{
		provider:    provider,
		evaluator:   eval,
		tracker:     tracker,
		templates:   templates,
		mutator:     mutator,
		concurrency: concurrency,
		maxErrors:   15,
	}
}

func (e *Engine) SetVerbose(v bool)     { e.verbose = v }
func (e *Engine) SetPyramidMode(v bool) { e.pyramidMode = v }
func (e *Engine) SetConcurrency(n int) {
	if n > 0 {
		e.concurrency = n
	}
}
func (e *Engine) SetLogFunc(f func(string, string, string, bool, float64)) { e.logFunc = f }

func (e *Engine) SetAllowedCategories(cats []string) {
	e.allowedCategories = cats
}

func (e *Engine) isCategoryAllowed(cat string) bool {
	if len(e.allowedCategories) == 0 {
		return true
	}
	for _, allowed := range e.allowedCategories {
		if strings.EqualFold(allowed, cat) {
			return true
		}
	}
	return false
}

func (e *Engine) RunSuite(ctx context.Context, profile string, callback FindingCallback) (*core.SuiteResult, error) {
	allGroups := e.buildAttacks(profile)
	var filteredGroups []attackGroup
	for _, g := range allGroups {
		if e.isCategoryAllowed(g.category) {
			filteredGroups = append(filteredGroups, g)
		}
	}

	if len(filteredGroups) == 0 {
		return nil, fmt.Errorf("no attacks build: all categories filtered out")
	}

	if e.pyramidMode {
		return e.runPyramid(ctx, filteredGroups, callback)
	}

	return e.runFull(ctx, filteredGroups, callback)
}

func (e *Engine) runFull(ctx context.Context, attackGroups []attackGroup, callback FindingCallback) (*core.SuiteResult, error) {
	result := core.NewSuiteResult()

	totalAttacks := 0
	for _, a := range attackGroups {
		totalAttacks += len(a.payloads)
	}

	fmt.Printf("\n[golem] AI Red Team Assessment\n")
	fmt.Printf("------------------------------------------------\n")
	fmt.Printf("Target:      %s %s\n", e.provider.Name(), e.provider.Model())
	fmt.Printf("Attacks:     %d\n", totalAttacks)
	fmt.Printf("Concurrency: %d\n", e.concurrency)
	if r, ok := e.provider.(interface{ IsReasoning() bool }); ok && r.IsReasoning() {
		fmt.Printf("[!] Reasoning model detected. Streaming + Early Stop enabled.\n")
	}
	fmt.Println()

	e.runGroups(ctx, attackGroups, result, callback)
	result.Finalize()
	e.printSummary(result)
	return result, nil
}

func (e *Engine) runPyramid(ctx context.Context, allGroups []attackGroup, callback FindingCallback) (*core.SuiteResult, error) {
	result := core.NewSuiteResult()

	fmt.Printf("\n[golem] Pyramid Mode (3-Phase Adaptive)\n")
	fmt.Printf("------------------------------------------------\n")
	fmt.Printf("Target:  %s %s\n", e.provider.Name(), e.provider.Model())
	fmt.Printf("Mode:    3-phase pyramid (recon -> deep -> exploit)\n\n")
	fmt.Printf("=== PHASE 1: RECONNAISSANCE ===\n")
	var reconGroups []attackGroup
	for _, g := range allGroups {
		limit := g.minInt(3, len(g.payloads))
		reconGroups = append(reconGroups, attackGroup{
			name:     g.name,
			category: g.category,
			payloads: g.payloads[:limit],
		})
	}

	e.runGroups(ctx, reconGroups, result, callback)

	hitCategories := make(map[string]bool)
	for _, r := range result.Results {
		if r.Success {
			hitCategories[string(r.Category)] = true
		}
	}

	if len(hitCategories) == 0 {
		fmt.Printf("\n[+] No categories breached in recon. Model appears well-protected.\n")
		result.Finalize()
		e.printSummary(result)
		return result, nil
	}

	fmt.Printf("\n[!] Breached categories: ")
	for cat := range hitCategories {
		fmt.Printf("%s ", cat)
	}
	fmt.Println()

	fmt.Printf("\n=== PHASE 2: DEEP DIVE ===\n")
	var deepGroups []attackGroup
	for _, g := range allGroups {
		if hitCategories[g.category] {
			deepGroups = append(deepGroups, g)
		}
	}

	e.runGroups(ctx, deepGroups, result, callback)

	fmt.Printf("\n=== PHASE 3: EXPLOITATION ===\n")
	var successPrompts []string
	for _, r := range result.Results {
		if r.Success && r.Confidence >= 0.6 {
			successPrompts = append(successPrompts, r.Prompt)
		}
	}

	if len(successPrompts) > 0 {
		var exploitPayloads []payload
		limit := e.minInt(5, len(successPrompts))
		for _, p := range successPrompts[:limit] {
			mutants := e.mutator.Mutate(p, 5)
			for _, m := range mutants[1:] {
				exploitPayloads = append(exploitPayloads, payload{
					text:      m,
					technique: "exploit_mutation",
					owasp:     "LLM01",
					severity:  core.SeverityHigh,
				})
			}
		}

		if len(exploitPayloads) > 0 {
			exploitGroup := attackGroup{
				name:     "Exploit Mutations",
				category: "prompt_injection",
				payloads: exploitPayloads,
			}
			e.runGroups(ctx, []attackGroup{exploitGroup}, result, callback)
		}
	}

	result.Finalize()
	e.printSummary(result)
	return result, nil
}

func (e *Engine) minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (e *Engine) runGroups(ctx context.Context, groups []attackGroup, result *core.SuiteResult, callback FindingCallback) {
	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errorCount int32

	seen := make(map[string]bool)

	for _, ag := range groups {
		if ctx.Err() != nil {
			break
		}

		fmt.Printf("[*] %-35s ", ag.name)
		if e.verbose {
			fmt.Println()
		}

		groupSuccess := 0
		groupTotal := 0

		for _, p := range ag.payloads {
			normalized := normalizePrompt(p.text)
			if seen[normalized] {
				continue
			}
			seen[normalized] = true
			groupTotal++

			select {
			case <-ctx.Done():
				break
			default:
			}

			if atomic.LoadInt32(&errorCount) >= int32(e.maxErrors) {
				fmt.Printf("\n[!] Too many errors (%d). Stopping.\n", e.maxErrors)
				return
			}

			wg.Add(1)
			go func(p payload) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				attackResult := e.executeAttack(ctx, p)

				if err := e.tracker.AddCost(attackResult.Cost); err != nil {
					return
				}

				if strings.HasPrefix(attackResult.Response, "ERROR:") {
					atomic.AddInt32(&errorCount, 1)
				} else {
					atomic.StoreInt32(&errorCount, 0)
				}

				if e.logFunc != nil {
					e.logFunc(attackResult.Technique, attackResult.Prompt,
						attackResult.Response, attackResult.Success, attackResult.Confidence)
				}

				mu.Lock()
				result.AddResult(attackResult)
				if attackResult.Success {
					groupSuccess++
				}
				mu.Unlock()

				if callback != nil {
					callback(attackResult)
				}
			}(p)
		}

		wg.Wait()

		if !e.verbose {
			if groupSuccess > 0 {
				fmt.Printf("[VULNERABLE] (%d/%d)\n", groupSuccess, groupTotal)
			} else {
				fmt.Printf("[SAFE] (%d/%d)\n", 0, groupTotal)
			}
		}
	}
}

func (e *Engine) printSummary(result *core.SuiteResult) {
	fmt.Printf("\n------------------------------------------------\n")
	fmt.Printf("RESULTS:\n")
	fmt.Printf("  Total:            %d\n", result.TotalAttacks)
	fmt.Printf("  Vulnerable:       %d (%.1f%%)\n", result.Successful, result.SuccessRate)
	fmt.Printf("  Critical: %d | High: %d | Medium: %d | Low: %d\n",
		result.BySeverity["critical"],
		result.BySeverity["high"],
		result.BySeverity["medium"],
		result.BySeverity["low"])
	fmt.Printf("  Score: %d/100 (%s) | Risk: %s\n",
		result.OverallScore, result.Grade, result.RiskLevel)
	fmt.Printf("  Cost: %s | Duration: %s\n",
		e.tracker.String(), result.Duration.Round(time.Second))
}

func normalizePrompt(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func (e *Engine) executeAttack(ctx context.Context, p payload) core.AttackResult {
	start := time.Now()

	resp, err := e.provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: p.text},
	})

	category := string(p.owasp)
	categoryMap := map[string]string{
		"LLM01": "prompt_injection",
		"LLM06": "data_extraction",
		"LLM09": "hallucination",
		"LLM07": "tool_abuse",
	}
	if mapped, ok := categoryMap[p.owasp]; ok {
		category = mapped
	}

	if err != nil {
		return core.AttackResult{
			ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
			AttackName: p.technique,
			Category:   core.AttackCategory(category),
			Technique:  p.technique,
			OWASP:      p.owasp,
			Severity:   p.severity,
			Prompt:     p.text,
			Response:   "ERROR: " + err.Error(),
			Success:    false,
			Duration:   time.Since(start),
			Timestamp:  time.Now(),
		}
	}

	success, confidence, evidence := e.evaluator.Evaluate(p.text, resp.Content, category)

	return core.AttackResult{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		AttackName: p.technique,
		Category:   core.AttackCategory(category),
		Technique:  p.technique,
		OWASP:      p.owasp,
		Severity:   p.severity,
		Prompt:     p.text,
		Response:   resp.Content,
		Success:    success,
		Confidence: confidence,
		Evidence:   evidence,
		Duration:   resp.Duration,
		Cost:       resp.Cost,
		Model:      resp.Model,
		Timestamp:  time.Now(),
	}
}
