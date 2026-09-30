package report

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"
	"time"

	"github.com/osint-technologies/golem/pkg/core"
)

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>AI Red Team Report вЂ” {{.Target.Name}}</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
         background: #0f0f14; color: #e0e0e6; line-height: 1.6; padding: 20px; }
  .container { max-width: 1200px; margin: 0 auto; }
  header { text-align: center; padding: 30px 0; border-bottom: 2px solid #1e1e2e; margin-bottom: 30px; }
  h1 { font-size: 32px; margin-bottom: 10px; }
  h1 .icon { color: #ff4757; }
  .subtitle { color: #888; font-size: 14px; }
  
  .scorecard { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: 20px; margin-bottom: 30px; }
  .card { background: #1a1a24; border: 1px solid #2a2a3a; border-radius: 12px; padding: 24px; }
  .card-label { font-size: 12px; text-transform: uppercase; color: #888; margin-bottom: 8px; letter-spacing: 1px; }
  .card-value { font-size: 32px; font-weight: bold; }
  .card-detail { font-size: 12px; color: #666; margin-top: 4px; }
  
  .grade-A { color: #2ed573; } .grade-B { color: #7bed9f; }
  .grade-C { color: #ffa502; } .grade-D { color: #ff6348; }
  .grade-F { color: #ff4757; }
  
  .risk-LOW { color: #2ed573; } .risk-MEDIUM { color: #ffa502; }
  .risk-HIGH { color: #ff6348; } .risk-CRITICAL { color: #ff4757; }
  
  .section { background: #1a1a24; border: 1px solid #2a2a3a; border-radius: 12px; 
             padding: 24px; margin-bottom: 20px; }
  .section h2 { margin-bottom: 20px; padding-bottom: 12px; border-bottom: 1px solid #2a2a3a;
                display: flex; align-items: center; gap: 10px; }
  
  .severity-bar { display: grid; grid-template-columns: repeat(5, 1fr); gap: 8px; margin: 20px 0; }
  .sev-item { text-align: center; padding: 16px; border-radius: 8px; }
  .sev-critical { background: rgba(255, 71, 87, 0.15); border: 1px solid #ff4757; }
  .sev-high { background: rgba(255, 99, 72, 0.15); border: 1px solid #ff6348; }
  .sev-medium { background: rgba(255, 165, 2, 0.15); border: 1px solid #ffa502; }
  .sev-low { background: rgba(123, 237, 159, 0.15); border: 1px solid #7bed9f; }
  .sev-info { background: rgba(112, 161, 255, 0.15); border: 1px solid #70a1ff; }
  .sev-count { font-size: 28px; font-weight: bold; }
  .sev-label { font-size: 11px; text-transform: uppercase; color: #aaa; margin-top: 4px; }
  
  .finding { background: #22222e; border-left: 4px solid #444; border-radius: 6px;
             padding: 20px; margin-bottom: 16px; }
  .finding.severity-critical { border-left-color: #ff4757; }
  .finding.severity-high { border-left-color: #ff6348; }
  .finding.severity-medium { border-left-color: #ffa502; }
  .finding.severity-low { border-left-color: #7bed9f; }
  
  .finding-header { display: flex; justify-content: space-between; margin-bottom: 12px; }
  .finding-title { font-weight: 600; font-size: 16px; }
  .finding-badges { display: flex; gap: 8px; }
  .badge { padding: 4px 10px; border-radius: 12px; font-size: 11px; font-weight: 600;
           text-transform: uppercase; }
  .badge-critical { background: #ff4757; color: white; }
  .badge-high { background: #ff6348; color: white; }
  .badge-medium { background: #ffa502; color: white; }
  .badge-low { background: #7bed9f; color: #0f0f14; }
  .badge-owasp { background: #2f3542; color: #a4b0be; }
  
  .finding-body { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
  .finding-field { background: #1a1a24; padding: 12px; border-radius: 6px; }
  .finding-field-label { font-size: 11px; color: #888; margin-bottom: 6px;
                        text-transform: uppercase; letter-spacing: 1px; }
  .finding-field-value { font-family: 'SF Mono', Consolas, monospace; font-size: 13px;
                        color: #e0e0e6; white-space: pre-wrap; word-break: break-word;
                        max-height: 200px; overflow-y: auto; }
  .finding-meta { margin-top: 12px; padding-top: 12px; border-top: 1px solid #2a2a3a;
                  display: flex; gap: 20px; font-size: 12px; color: #888; }
  
  .empty { text-align: center; padding: 40px; color: #666; font-style: italic; }
  footer { text-align: center; padding: 30px; color: #666; font-size: 12px; }
  
  .category-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
                   gap: 12px; }
  .cat-item { background: #22222e; padding: 16px; border-radius: 8px; }
  .cat-name { font-weight: 600; margin-bottom: 8px; }
  .cat-bar { height: 8px; background: #1a1a24; border-radius: 4px; overflow: hidden; margin: 8px 0; }
  .cat-bar-fill { height: 100%; background: linear-gradient(90deg, #ff6348, #ff4757); }
</style>
</head>
<body>
<div class="container">

<header>
  <h1><span class="icon">рџ”ґ</span> AI Red Team Report</h1>
  <div class="subtitle">
    Target: <strong>{{.Target.Name}} вЂ” {{.Target.Model}}</strong> | 
    Profile: {{.Profile}} | 
    Generated: {{.EndTime.Format "2006-01-02 15:04:05 UTC"}}
  </div>
</header>

<div class="scorecard">
  <div class="card">
    <div class="card-label">Security Score</div>
    <div class="card-value grade-{{.Grade}}">{{.OverallScore}}/100</div>
    <div class="card-detail">Grade: {{.Grade}}</div>
  </div>
  <div class="card">
    <div class="card-label">Risk Level</div>
    <div class="card-value risk-{{.RiskLevel}}">{{.RiskLevel}}</div>
    <div class="card-detail">Based on findings severity</div>
  </div>
  <div class="card">
    <div class="card-label">Vulnerabilities</div>
    <div class="card-value">{{.Successful}}</div>
    <div class="card-detail">of {{.TotalAttacks}} attacks ({{printf "%.1f" .SuccessRate}}%)</div>
  </div>
  <div class="card">
    <div class="card-label">Duration</div>
    <div class="card-value">{{formatDuration .Duration}}</div>
    <div class="card-detail">Cost: ${{printf "%.2f" .TotalCost}}</div>
  </div>
</div>

<div class="section">
  <h2>рџ“Љ Severity Distribution</h2>
  <div class="severity-bar">
    <div class="sev-item sev-critical">
      <div class="sev-count">{{index .BySeverity "critical"}}</div>
      <div class="sev-label">Critical</div>
    </div>
    <div class="sev-item sev-high">
      <div class="sev-count">{{index .BySeverity "high"}}</div>
      <div class="sev-label">High</div>
    </div>
    <div class="sev-item sev-medium">
      <div class="sev-count">{{index .BySeverity "medium"}}</div>
      <div class="sev-label">Medium</div>
    </div>
    <div class="sev-item sev-low">
      <div class="sev-count">{{index .BySeverity "low"}}</div>
      <div class="sev-label">Low</div>
    </div>
    <div class="sev-item sev-info">
      <div class="sev-count">{{index .BySeverity "info"}}</div>
      <div class="sev-label">Info</div>
    </div>
  </div>
</div>

<div class="section">
  <h2>рџ“Ѓ Vulnerabilities by Category</h2>
  <div class="category-grid">
    {{range $cat, $count := .ByCategory}}
    <div class="cat-item">
      <div class="cat-name">{{$cat}}</div>
      <div class="cat-bar"><div class="cat-bar-fill" style="width: {{percentOf $count 20}}%"></div></div>
      <div style="font-size:12px;color:#888;">{{$count}} vulnerabilities</div>
    </div>
    {{end}}
  </div>
</div>

<div class="section">
  <h2>рџљЁ Detailed Findings</h2>
  {{$successful := 0}}
  {{range .Results}}
    {{if .Success}}
      {{$successful = 1}}
      <div class="finding severity-{{.Severity}}">
        <div class="finding-header">
          <div class="finding-title">{{.Technique}}</div>
          <div class="finding-badges">
            <span class="badge badge-{{.Severity}}">{{.Severity}}</span>
            <span class="badge badge-owasp">{{.OWASP}}</span>
          </div>
        </div>
        <div class="finding-body">
          <div class="finding-field">
            <div class="finding-field-label">Attack Prompt</div>
            <div class="finding-field-value">{{truncate .Prompt 400}}</div>
          </div>
          <div class="finding-field">
            <div class="finding-field-label">Model Response</div>
            <div class="finding-field-value">{{truncate .Response 400}}</div>
          </div>
        </div>
        <div class="finding-meta">
          <span>рџЋЇ Confidence: {{printf "%.0f" (mul .Confidence 100)}}%</span>
          <span>рџ”Ќ Evidence: {{.Evidence}}</span>
          <span>вЏ±пёЏ Duration: {{formatDuration .Duration}}</span>
          <span>рџ“Ѓ Category: {{.Category}}</span>
        </div>
      </div>
    {{end}}
  {{end}}
  {{if eq $successful 0}}
  <div class="empty">вњ… No vulnerabilities found. Model is well-protected!</div>
  {{end}}
</div>

<footer>
  Generated by <strong>golem</strong> вЂ” Open source AI security toolkit<br>
  Report ID: {{.EndTime.Unix}}
</footer>

</div>
</body>
</html>`

func GenerateHTML(result *core.SuiteResult, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	return GenerateHTMLToWriter(result, f)
}

func GenerateHTMLToWriter(result *core.SuiteResult, w io.Writer) error {
	funcs := template.FuncMap{
		"formatDuration": func(d time.Duration) string {
			return d.Round(time.Second).String()
		},
		"truncate": func(s string, n int) string {
			s = strings.ReplaceAll(s, "\n", " ")
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
		"percentOf": func(count, max int) int {
			p := (count * 100) / max
			if p > 100 {
				return 100
			}
			return p
		},
	}

	tmpl, err := template.New("report").Funcs(funcs).Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	return tmpl.Execute(w, result)
}
