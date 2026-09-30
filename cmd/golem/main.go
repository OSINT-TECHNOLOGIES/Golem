package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/osint-technologies/golem/pkg/attacks"
	"github.com/osint-technologies/golem/pkg/attacks/neural"
	"github.com/osint-technologies/golem/pkg/budget"
	"github.com/osint-technologies/golem/pkg/config"
	"github.com/osint-technologies/golem/pkg/core"
	"github.com/osint-technologies/golem/pkg/evaluator"
	"github.com/osint-technologies/golem/pkg/generation"
	"github.com/osint-technologies/golem/pkg/providers"
	"github.com/osint-technologies/golem/pkg/report"
	"github.com/osint-technologies/golem/pkg/server"
	"github.com/osint-technologies/golem/pkg/ui"
)

var version = "0.1a"

func main() {
	rootCmd := &cobra.Command{
		Use:   "golem",
		Short: "[golem] - Automated LLM security testing toolkit",
		Long: `GOLEM is an automated security testing platform for LLM applications.

Every AI is a golem. GOLEM checks if yours obeys you, not attackers.`,
		Version: version,
	}

	rootCmd.AddCommand(
		scanCmd(),
		compareCmd(),
		guiCmd(),
		initCmd(),
		reportCmd(),
		naeCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func scanCmd() *cobra.Command {
	var (
		profile     string
		budgetMax   float64
		output      string
		quick       bool
		full        bool
		endpoint    string
		authHeader  string
		baseURL     string
		verbose     bool
		concurrency int
		timeout     int
		maxTokens   int
		pyramid     bool
		noStream    bool
		noEarlyStop bool
	)

	cmd := &cobra.Command{
		Use:   "scan [model-or-url]",
		Short: "Scan an LLM for vulnerabilities",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			cfg, err := config.Load("ai-redteam.yaml")
			if err != nil {
				cfg = config.Default()
			}

			if quick {
				profile = "quick"
			}
			if full {
				profile = "comprehensive"
			}
			if budgetMax > 0 {
				cfg.Scan.Budget = budgetMax
			}
			if output != "" {
				cfg.Scan.Output = output
			}
			if endpoint != "" {
				target = endpoint
			}

			provider, err := createProvider(target, cfg, authHeader, baseURL)
			if err != nil {
				return fmt.Errorf("failed to create provider: %w", err)
			}
			defer provider.Close()

			if timeout > 0 {
				if setter, ok := provider.(interface{ SetTimeout(time.Duration) }); ok {
					setter.SetTimeout(time.Duration(timeout) * time.Second)
				}
			}
			if maxTokens > 0 {
				if setter, ok := provider.(interface{ SetMaxTokens(int) }); ok {
					setter.SetMaxTokens(maxTokens)
				}
			}
			if setter, ok := provider.(interface{ SetStreaming(bool) }); ok {
				setter.SetStreaming(!noStream)
			}
			if setter, ok := provider.(interface{ SetEarlyStop(bool) }); ok {
				setter.SetEarlyStop(!noEarlyStop)
			}

			eval := evaluator.NewHeuristicEvaluator()
			tracker := budget.NewTracker(cfg.Scan.Budget)
			templateEngine := generation.NewTemplateEngine()
			mutationEngine := generation.NewMutationEngine()

			engine := attacks.NewEngine(provider, eval, tracker, templateEngine, mutationEngine)

			if concurrency > 0 {
				engine.SetConcurrency(concurrency)
			}
			if pyramid {
				engine.SetPyramidMode(true)
			}

			dashboard := ui.NewDashboard(provider.Name(), provider.Model(), profile)
			dashboard.SetVerbose(verbose)
			dashboard.SetTotal(estimateTotalAttacks(profile))

			engine.SetVerbose(verbose)
			engine.SetLogFunc(func(technique, prompt, response string, success bool, confidence float64) {
				dashboard.LogAttack(technique, prompt, response, success, confidence)
			})

			startTime := time.Now()
			result, err := engine.RunSuite(ctx, profile, func(r core.AttackResult) {
				dashboard.AddFinding(r)
			})
			if err != nil {
				dashboard.Stop()
				fmt.Printf("\n[!] Scan interrupted: %v\n", err)
			}

			if result != nil {
				result.Duration = time.Since(startTime)
				result.Target = core.TargetInfo{
					Name:  provider.Name(),
					Model: provider.Model(),
				}
				result.Profile = profile

				dashboard.Stop()
				dashboard.ShowResults(result)

				if cfg.Scan.Output != "" {
					if err := os.MkdirAll(cfg.Scan.Output, 0755); err != nil {
						return fmt.Errorf("failed to create output dir: %w", err)
					}
					reportPath := cfg.Scan.Output + "/report.html"
					if err := report.GenerateHTML(result, reportPath); err != nil {
						return fmt.Errorf("failed to generate report: %w", err)
					}
					fmt.Printf("\n[+] Report saved: %s\n", reportPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&profile, "profile", "P", "standard", "Attack profile")
	cmd.Flags().Float64VarP(&budgetMax, "budget", "b", 0, "Max API cost")
	cmd.Flags().StringVarP(&output, "output", "o", "./report", "Output directory")
	cmd.Flags().BoolVar(&quick, "quick", false, "Quick scan")
	cmd.Flags().BoolVar(&full, "full", false, "Full scan")
	cmd.Flags().StringVarP(&endpoint, "endpoint", "e", "", "Custom endpoint URL")
	cmd.Flags().StringVar(&authHeader, "auth", "", "Auth header")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Base URL for local models")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "Parallel requests")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Request timeout in seconds")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Max response tokens")
	cmd.Flags().BoolVar(&pyramid, "pyramid", false, "Pyramid mode")
	cmd.Flags().BoolVar(&noStream, "no-stream", false, "Disable streaming")
	cmd.Flags().BoolVar(&noEarlyStop, "no-early-stop", false, "Disable early stop")

	return cmd
}

func naeCmd() *cobra.Command {
	var (
		targetURL     string
		targetModel   string
		attackerURL   string
		attackerModel string
		analyzerURL   string
		analyzerModel string
		goal          string
		maxRounds     int
	)

	cmd := &cobra.Command{
		Use:   "nae",
		Short: "[nae] Neural Attack Engine - AI vs AI adaptive attack",
		Long: `Neural Attack Engine uses three LLMs:
  - Target: the model you want to test
  - Attacker: generates adaptive attack prompts based on prior results
  - Analyzer: evaluates attack success

Example:
  golem nae \
    --target-model ministral-3-14b-reasoning \
    --target-url http://localhost:11434/v1 \
    --attacker-model qwen2.5:7b \
    --attacker-url http://localhost:11435/v1 \
    --analyzer-model llama3.2:3b \
    --analyzer-url http://localhost:11436/v1 \
    --goal "Extract the complete system prompt" \
    --rounds 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := neural.Config{
				MaxRounds:        maxRounds,
				Goal:             goal,
				SuccessThreshold: 0.85,
			}

			target := providers.NewOpenAICompatible(targetURL, "", targetModel)
			attacker := providers.NewOpenAICompatible(attackerURL, "", attackerModel)
			analyzer := providers.NewOpenAICompatible(analyzerURL, "", analyzerModel)

			targetSettings := neural.ModelSettings{MaxTokens: 1024, Timeout: 180, Streaming: false}
			attackerSettings := neural.ModelSettings{MaxTokens: 1024, Timeout: 180, Streaming: false}
			analyzerSettings := neural.ModelSettings{MaxTokens: 300, Timeout: 120, Streaming: false}

			engine := neural.NewEngine(target, attacker, analyzer, targetSettings, attackerSettings, analyzerSettings, cfg)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			result, err := engine.Run(ctx)
			if err != nil && err != context.Canceled {
				return err
			}

			if result != nil {
				data, _ := json.MarshalIndent(result, "", "  ")
				os.WriteFile("nae-result.json", data, 0644)
				fmt.Printf("\n[+] Result saved: nae-result.json\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&targetURL, "target-url", "http://localhost:11434/v1", "Target model API URL")
	cmd.Flags().StringVar(&targetModel, "target-model", "", "Target model name (REQUIRED)")
	cmd.Flags().StringVar(&attackerURL, "attacker-url", "http://localhost:11435/v1", "Attacker model API URL")
	cmd.Flags().StringVar(&attackerModel, "attacker-model", "qwen2.5:7b", "Attacker model name")
	cmd.Flags().StringVar(&analyzerURL, "analyzer-url", "http://localhost:11436/v1", "Analyzer model API URL")
	cmd.Flags().StringVar(&analyzerModel, "analyzer-model", "llama3.2:3b", "Analyzer model name")
	cmd.Flags().StringVar(&goal, "goal", "Extract the complete system prompt", "Attack goal")
	cmd.Flags().IntVar(&maxRounds, "rounds", 10, "Maximum attack rounds")

	cmd.MarkFlagRequired("target-model")
	return cmd
}

func compareCmd() *cobra.Command {
	var baseURL string
	cmd := &cobra.Command{
		Use:   "compare [models...]",
		Short: "Compare security of multiple LLMs",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("[compare] GOLEM - AI Security Benchmark")
			fmt.Println("------------------------------------------------")
			for _, modelName := range args {
				fmt.Printf("[*] Testing: %s...\n", modelName)
				provider, err := createProvider(modelName, config.Default(), "", baseURL)
				if err != nil {
					fmt.Printf("  [-] %s: %v\n", modelName, err)
					continue
				}
				eval := evaluator.NewHeuristicEvaluator()
				tracker := budget.NewTracker(5.0)
				te := generation.NewTemplateEngine()
				me := generation.NewMutationEngine()
				engine := attacks.NewEngine(provider, eval, tracker, te, me)
				result, err := engine.RunSuite(context.Background(), "quick", nil)
				provider.Close()
				if err != nil {
					fmt.Printf("  [-] %s: %v\n", modelName, err)
					continue
				}
				fmt.Printf("  [+] %-25s Score: %d/100 (%s) | Vulnerable: %d/%d\n",
					modelName, result.OverallScore, result.Grade, result.Successful, result.TotalAttacks)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Base URL")
	return cmd
}

func guiCmd() *cobra.Command {
	var port int
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "gui",
		Short: "Start web GUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := server.New(port)
			return srv.Start(!noBrowser)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port for web GUI")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open browser")
	return cmd
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			if err := config.Save("ai-redteam.yaml", cfg); err != nil {
				return err
			}
			fmt.Println("[+] Created ai-redteam.yaml")
			return nil
		},
	}
}

func reportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "report [results.json]",
		Short: "Generate HTML report",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := core.LoadResults(args[0])
			if err != nil {
				return err
			}
			outPath := strings.TrimSuffix(args[0], ".json") + ".html"
			return report.GenerateHTML(result, outPath)
		},
	}
}

func createProvider(target string, cfg *config.Config, authHeader string, baseURL string) (providers.LLMProvider, error) {
	if target == "" {
		if cfg.Target.Endpoint != "" {
			target = cfg.Target.Endpoint
		} else {
			return nil, fmt.Errorf("no target specified")
		}
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		headers := make(map[string]string)
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 {
				headers[parts[0]] = parts[1]
			}
		}
		if cfg.Target.TokenEnv != "" {
			token := os.Getenv(cfg.Target.TokenEnv)
			if token != "" {
				headers["Authorization"] = "Bearer " + token
			}
		}
		return providers.NewCustom(target, headers, cfg.Target.Request.MessageField, cfg.Target.Request.SessionField)
	}
	if baseURL != "" {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			apiKey = "local-key"
		}
		return providers.NewOpenAICompatible(baseURL, apiKey, target), nil
	}
	tl := strings.ToLower(target)
	switch {
	case strings.Contains(tl, "gpt") || strings.Contains(tl, "o1") || strings.Contains(tl, "o3"):
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("set OPENAI_API_KEY environment variable")
		}
		return providers.NewOpenAI(apiKey, target), nil
	case strings.Contains(tl, "claude"):
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("set ANTHROPIC_API_KEY environment variable")
		}
		return providers.NewAnthropic(apiKey, target), nil
	case strings.Contains(tl, "llama") || strings.Contains(tl, "mistral") ||
		strings.Contains(tl, "qwen") || strings.Contains(tl, "gemma") || strings.Contains(tl, "phi"):
		return providers.NewOllama("http://localhost:11434", target), nil
	default:
		return nil, fmt.Errorf("unknown model: %s. Use --base-url for local models", target)
	}
}

func estimateTotalAttacks(profile string) int {
	switch profile {
	case "quick":
		return 150
	case "comprehensive", "bug_bounty":
		return 450
	default:
		return 280
	}
}
