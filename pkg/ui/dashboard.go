package ui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/osint-technologies/golem/pkg/core"
)

var (
	red          = lipgloss.Color("#ff4757")
	orange       = lipgloss.Color("#ff6348")
	yellow       = lipgloss.Color("#ffa502")
	green        = lipgloss.Color("#2ed573")
	cyan         = lipgloss.Color("#00d2d3")
	blue         = lipgloss.Color("#54a0ff")
	purple       = lipgloss.Color("#a29bfe")
	white        = lipgloss.Color("#ffffff")
	gray         = lipgloss.Color("#636e72")
	successStyle = lipgloss.NewStyle().Foreground(green).Bold(true)
	failStyle    = lipgloss.NewStyle().Foreground(red).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(yellow).Bold(true)
	mutedStyle   = lipgloss.NewStyle().Foreground(gray)
	cyanStyle    = lipgloss.NewStyle().Foreground(cyan)
	purpleStyle  = lipgloss.NewStyle().Foreground(purple)
	orangeStyle  = lipgloss.NewStyle().Foreground(orange).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(red)
	asciiBorder  = lipgloss.Border{
		Top:         "-",
		Bottom:      "-",
		Left:        "|",
		Right:       "|",
		TopLeft:     "+",
		TopRight:    "+",
		BottomLeft:  "+",
		BottomRight: "+",
	}

	boxStyle = lipgloss.NewStyle().
			Border(asciiBorder).
			BorderForeground(lipgloss.Color("#2a2a3a")).
			Padding(1, 2)
)

type Dashboard struct {
	target    string
	model     string
	profile   string
	verbose   bool
	findings  []core.AttackResult
	mu        sync.Mutex
	startTime time.Time
	stopped   bool
	attackNum int32
	totalNum  int32
}

func NewDashboard(target, model, profile string) *Dashboard {
	return &Dashboard{
		target:    target,
		model:     model,
		profile:   profile,
		startTime: time.Now(),
	}
}

func (d *Dashboard) SetVerbose(v bool) {
	d.verbose = v
}

func (d *Dashboard) SetTotal(total int) {
	atomic.StoreInt32(&d.totalNum, int32(total))
}

func (d *Dashboard) Run() {}

func (d *Dashboard) LogAttack(technique, prompt, response string, success bool, confidence float64) {
	current := atomic.AddInt32(&d.attackNum, 1)

	if !d.verbose {
		return
	}

	elapsed := time.Since(d.startTime).Round(time.Second)

	var status string
	if strings.HasPrefix(response, "ERROR:") {
		status = errorStyle.Render("[ERR]")
	} else if success {
		if confidence >= 0.8 {
			status = failStyle.Render("[CRIT]")
		} else {
			status = warnStyle.Render("[HIT]")
		}
	} else {
		status = mutedStyle.Render("[SAFE]")
	}

	total := atomic.LoadInt32(&d.totalNum)
	counter := fmt.Sprintf("[%d/%d]", current, total)

	fmt.Printf("\n%s %s %s %s\n",
		mutedStyle.Render(fmt.Sprintf("[%s]", elapsed)),
		status,
		mutedStyle.Render(counter),
		cyanStyle.Render(technique),
	)
	fmt.Printf("  %s %s\n",
		purpleStyle.Render("->"),
		truncate(prompt, 130),
	)

	respPreview := response
	if strings.HasPrefix(response, "ERROR:") {
		respPreview = errorStyle.Render(truncate(response, 150))
	} else {
		respPreview = truncate(response, 200)
	}

	fmt.Printf("  %s %s\n",
		mutedStyle.Render("<-"),
		respPreview,
	)

	if success {
		fmt.Printf("  %s confidence: %.0f%%\n",
			warnStyle.Render("[!]"),
			confidence*100,
		)
	}
}

func (d *Dashboard) AddFinding(r core.AttackResult) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.findings = append(d.findings, r)

	if r.Success && !d.verbose {
		elapsed := time.Since(d.startTime).Round(time.Second)
		icon := "[!]"
		sevStyle := failStyle
		if r.Severity == core.SeverityMedium {
			icon = "[?]"
			sevStyle = warnStyle
		} else if r.Severity == core.SeverityLow {
			icon = "[i]"
			sevStyle = cyanStyle
		}

		fmt.Printf("\n  %s %s %s - %s\n",
			icon,
			mutedStyle.Render(fmt.Sprintf("[%s]", elapsed)),
			sevStyle.Render(strings.ToUpper(string(r.Severity))),
			r.Technique,
		)
		fmt.Printf("     %s %s\n",
			mutedStyle.Render("Prompt:"),
			truncate(r.Prompt, 100),
		)
		fmt.Printf("     %s %s\n",
			mutedStyle.Render("Response:"),
			truncate(r.Response, 150),
		)
	}
}

func (d *Dashboard) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
}

func (d *Dashboard) ShowResults(result *core.SuiteResult) {
	fmt.Println()
	fmt.Println(renderSeparator("GOLEM - FINAL REPORT"))
	fmt.Println()

	scoreColor := green
	if result.OverallScore < 75 {
		scoreColor = yellow
	}
	if result.OverallScore < 50 {
		scoreColor = orange
	}
	if result.OverallScore < 25 {
		scoreColor = red
	}

	scoreBox := lipgloss.NewStyle().
		Border(asciiBorder).
		BorderForeground(scoreColor).
		Padding(1, 3).
		Align(lipgloss.Center)

	scoreContent := fmt.Sprintf(
		"%s\n\n%s\n%s\n%s",
		lipgloss.NewStyle().Foreground(scoreColor).Bold(true).Render(
			fmt.Sprintf("SCORE: %d/100  GRADE: %s", result.OverallScore, result.Grade),
		),
		fmt.Sprintf("Target:   %s / %s", result.Target.Name, result.Target.Model),
		fmt.Sprintf("Profile:  %s", result.Profile),
		fmt.Sprintf("Duration: %s  |  Cost: $%.2f", result.Duration.Round(time.Second), result.TotalCost),
	)
	fmt.Println(scoreBox.Render(scoreContent))
	fmt.Println()

	fmt.Println(renderSeparator("DISTRIBUTION"))
	fmt.Println()

	sevBar := func(label string, count int, color lipgloss.Color, width int) string {
		bar := ""
		if count > 0 {
			bar = "[" + strings.Repeat("=", count) + "]"
		} else {
			bar = "[-]"
		}
		return fmt.Sprintf("  %-10s %s %d",
			lipgloss.NewStyle().Foreground(color).Bold(true).Render(label),
			lipgloss.NewStyle().Foreground(color).Render(bar),
			count,
		)
	}

	fmt.Println(sevBar("CRITICAL", result.BySeverity["critical"], red, 40))
	fmt.Println(sevBar("HIGH", result.BySeverity["high"], orange, 40))
	fmt.Println(sevBar("MEDIUM", result.BySeverity["medium"], yellow, 40))
	fmt.Println(sevBar("LOW", result.BySeverity["low"], green, 40))
	fmt.Println(sevBar("INFO", result.BySeverity["info"], blue, 40))
	fmt.Println()

	fmt.Printf("  Total attacks:    %d\n", result.TotalAttacks)
	fmt.Printf("  Vulnerabilities:  %s\n",
		lipgloss.NewStyle().Foreground(red).Bold(true).Render(fmt.Sprintf("%d", result.Successful)),
	)
	fmt.Printf("  Success rate:     %.1f%%\n", result.SuccessRate)
	fmt.Printf("  Risk level:       %s\n", riskStyled(result.RiskLevel))
	fmt.Println()

	if result.Successful > 0 {
		fmt.Println(renderSeparator("TOP FINDINGS"))
		fmt.Println()

		shown := 0
		for _, r := range result.Results {
			if !r.Success {
				continue
			}
			if shown >= 10 {
				fmt.Printf("  ... and %d more (see full report)\n", result.Successful-10)
				break
			}

			sevColor := red
			if r.Severity == core.SeverityHigh {
				sevColor = orange
			} else if r.Severity == core.SeverityMedium {
				sevColor = yellow
			}

			fmt.Printf("  %s %s\n",
				lipgloss.NewStyle().Foreground(sevColor).Bold(true).Render(
					fmt.Sprintf("[%s]", strings.ToUpper(string(r.Severity)))),
				cyanStyle.Render(r.Technique),
			)
			fmt.Printf("    %-12s %s\n", mutedStyle.Render("OWASP:"), r.OWASP)
			fmt.Printf("    %-12s %s\n", mutedStyle.Render("Prompt:"), truncate(r.Prompt, 90))
			fmt.Printf("    %-12s %s\n", mutedStyle.Render("Response:"), truncate(r.Response, 120))
			fmt.Printf("    %-12s %.0f%%\n", mutedStyle.Render("Confidence:"), r.Confidence*100)
			fmt.Println()
			shown++
		}
	} else {
		fmt.Println(renderSeparator("NO VULNERABILITIES FOUND"))
		fmt.Println()
		fmt.Println("  The model resisted all tested attack vectors.")
		fmt.Println()
	}

	fmt.Println(renderSeparator(""))
}

func renderSeparator(title string) string {
	if title == "" {
		return mutedStyle.Render(strings.Repeat("-", 78))
	}
	line := strings.Repeat("-", 3)
	pad := 78 - len(title) - len(line) - 4
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("%s %s %s",
		mutedStyle.Render(line),
		lipgloss.NewStyle().Bold(true).Foreground(white).Render(title),
		mutedStyle.Render(strings.Repeat("-", pad)),
	)
}

func riskStyled(level string) string {
	switch level {
	case "CRITICAL":
		return failStyle.Render("[!] " + level)
	case "HIGH":
		return orangeStyle.Render("[!] " + level)
	case "MEDIUM":
		return warnStyle.Render("[?] " + level)
	default:
		return successStyle.Render("[+] " + level)
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
