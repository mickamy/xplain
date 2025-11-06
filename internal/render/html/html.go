package html

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"

	"github.com/mickamy/xplain/internal/analyzer"
	"github.com/mickamy/xplain/internal/insight"
)

// Options configures the HTML renderer.
type Options struct {
	Title         string
	IncludeStyles bool
}

// Render writes an HTML report containing a plan summary and annotated tree.
func Render(w io.Writer, analysis *analyzer.PlanAnalysis, opts Options) error {
	if analysis == nil || analysis.Root == nil {
		return fmt.Errorf("html render: empty analysis")
	}
	if opts.Title == "" {
		opts.Title = "xplain report"
	}
	data := buildTemplateData(analysis, opts)
	tpl, err := template.New("report").Funcs(template.FuncMap{"join": strings.Join}).Parse(reportTemplate)
	if err != nil {
		return fmt.Errorf("html render: compile template: %w", err)
	}
	if err := tpl.Execute(w, data); err != nil {
		return fmt.Errorf("html render: execute template: %w", err)
	}
	return nil
}

type templateData struct {
	Title         string
	IncludeStyles bool
	Summary       summaryView
	Root          *nodeView
	HotNodes      []listView
	Divergent     []listView
	Insights      []insightView
}

type summaryView struct {
	ExecutionTime string
	PlanningTime  string
	NodeCount     int
	HotCount      int
	Divergent     int
	Buffers       string
}

type listView struct {
	Label string
	Self  string
	Share string
	Extra string
}

type insightView struct {
	Icon     string
	Severity string
	Text     string
	Anchor   string
}

type nodeView struct {
	Label      string
	Anchor     string
	Self       string
	Share      string
	BarWidth   float64
	Heat       float64
	Rows       string
	Buffers    string
	Warnings   []string
	Children   []*nodeView
	HasWarning bool
}

func buildTemplateData(analysis *analyzer.PlanAnalysis, opts Options) templateData {
	root := buildNodeView(analysis.Root)
	messages := insight.BuildMessages(analysis)
	insights := make([]insightView, 0, len(messages))
	for _, msg := range messages {
		insights = append(insights, insightView{
			Icon:     severityIcon(msg.Severity),
			Severity: string(msg.Severity),
			Text:     msg.Text,
			Anchor:   msg.Anchor,
		})
	}

	hot := make([]listView, 0, len(analysis.HotNodes))
	for _, node := range analysis.HotNodes {
		hot = append(hot, listView{
			Label: insight.NodeLabel(node),
			Self:  fmt.Sprintf("%.2f ms", node.ExclusiveTimeMs),
			Share: fmt.Sprintf("%.1f%%", node.PercentExclusive*100),
			Extra: formatRows(node),
		})
	}

	divergent := make([]listView, 0, len(analysis.DivergentNodes))
	for _, node := range analysis.DivergentNodes {
		divergent = append(divergent, listView{
			Label: insight.NodeLabel(node),
			Self:  fmt.Sprintf("%.2f ms", node.ExclusiveTimeMs),
			Share: fmt.Sprintf("x%.2f", node.RowEstimateFactor),
			Extra: formatRows(node),
		})
	}

	return templateData{
		Title:         opts.Title,
		IncludeStyles: opts.IncludeStyles,
		Summary: summaryView{
			ExecutionTime: fmt.Sprintf("%.3f ms", analysis.TotalTimeMs),
			PlanningTime:  fmt.Sprintf("%.3f ms", analysis.PlanningTimeMs),
			NodeCount:     analysis.NodeCount,
			HotCount:      len(analysis.HotNodes),
			Divergent:     len(analysis.DivergentNodes),
			Buffers:       insight.SummarizeTotalBuffers(analysis.TotalBuffers),
		},
		Root:      root,
		HotNodes:  hot,
		Divergent: divergent,
		Insights:  insights,
	}
}

func buildNodeView(node *analyzer.NodeStats) *nodeView {
	view := &nodeView{
		Label:    insight.NodeLabel(node),
		Anchor:   insight.AnchorID(node),
		Self:     fmt.Sprintf("%.2f ms (workers)", node.ExclusiveTimeMs),
		Share:    fmt.Sprintf("%.1f%%", node.PercentExclusive*100),
		BarWidth: math.Min(100, math.Max(0, node.PercentExclusive*100)),
		Heat:     clamp(node.PercentExclusive*2.5, 0, 1),
		Rows:     formatRows(node),
		Buffers:  formatBuffers(node),
		Warnings: append([]string(nil), node.Warnings...),
	}
	if len(view.Warnings) > 0 {
		view.HasWarning = true
	}
	for _, child := range node.Children {
		view.Children = append(view.Children, buildNodeView(child))
	}
	return view
}

func formatRows(node *analyzer.NodeStats) string {
	if node.EstimatedRows == 0 && node.ActualTotalRows == 0 {
		return ""
	}
	if math.IsInf(node.RowEstimateFactor, 1) {
		return fmt.Sprintf("rows %.0f / %.0f (∞)", node.ActualTotalRows, node.EstimatedRows)
	}
	return fmt.Sprintf("rows %.0f / %.0f (x%.2f)", node.ActualTotalRows, node.EstimatedRows, node.RowEstimateFactor)
}

func formatBuffers(node *analyzer.NodeStats) string {
	total := node.Buffers.Total()
	if total == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("total %d (~%s)", total, insight.HumanizeBuffers(total))}
	if node.Buffers.SharedRead > 0 {
		parts = append(parts, fmt.Sprintf("shared read %d", node.Buffers.SharedRead))
	}
	if node.Buffers.SharedHit > 0 {
		parts = append(parts, fmt.Sprintf("shared hit %d", node.Buffers.SharedHit))
	}
	if node.Buffers.TempRead > 0 || node.Buffers.TempWritten > 0 {
		parts = append(parts, fmt.Sprintf("temp %d/%d", node.Buffers.TempRead, node.Buffers.TempWritten))
	}
	return "buffers " + strings.Join(parts, ", ")
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func severityIcon(sev insight.Severity) string {
	switch sev {
	case insight.SeverityCritical:
		return "🔥"
	case insight.SeverityWarning:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<title>{{.Title}}</title>
	{{- if .IncludeStyles }}
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; margin: 0; padding: 0; background: #f7f7f8; color: #202124; }
		main { max-width: 960px; margin: 0 auto; padding: 32px 24px 48px; }
		header { background: #212a3b; color: #f7f7f8; padding: 32px 24px; }
		header h1 { margin: 0 0 8px; font-size: 28px; }
		header p { margin: 4px 0; opacity: 0.8; }
		section { margin-top: 32px; }
		section h2 { margin-bottom: 12px; font-size: 20px; }
		.summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; }
		.summary-tile { background: #fff; border-radius: 10px; padding: 16px; box-shadow: 0 6px 18px rgba(13,28,39,0.12); }
		.summary-tile strong { display: block; font-size: 14px; text-transform: uppercase; letter-spacing: 0.04em; color: #5b7083; margin-bottom: 6px; }
		.summary-tile span { font-size: 18px; font-weight: 600; }
		.flex-list { display: flex; flex-direction: column; gap: 10px; }
		.list-card { background: #fff; border-radius: 12px; padding: 16px; box-shadow: 0 4px 12px rgba(13,28,39,0.10); }
		.list-card header { display: flex; justify-content: space-between; align-items: baseline; }
		.list-card header h3 { margin: 0; font-size: 16px; color: #253043; }
		.list-card header span { font-size: 13px; color: #5b7083; }
		.list-card ul { list-style: none; padding: 0; margin: 12px 0 0; }
		.list-card li { display: grid; grid-template-columns: 1fr auto auto; gap: 12px; font-size: 14px; padding: 8px 0; border-bottom: 1px solid rgba(91,112,131,0.16); }
		.list-card li:last-child { border-bottom: none; }
		.plan-tree { list-style: none; margin: 0; padding: 0; }
		.plan-tree > li { margin-bottom: 12px; }
		.node-card { background: #fff; border-radius: 12px; margin-bottom: 12px; position: relative; padding: 16px 18px 14px 18px; box-shadow: 0 8px 20px rgba(16,37,58,0.12); border-left: 6px solid rgba(33,42,59,0.1); }
		.node-card::after { content: ""; position: absolute; inset: 0; border-radius: inherit; background: linear-gradient(90deg, rgba(244,71,71,var(--heat)) 0%, rgba(244,71,71,0) 72%); opacity: 0.35; pointer-events: none; }
		.node-header { position: relative; z-index: 1; display: flex; justify-content: space-between; gap: 12px; align-items: baseline; }
		.node-label { font-weight: 600; font-size: 15px; }
		.node-metrics { font-size: 13px; color: #5b7083; }
		.node-bar { position: relative; z-index: 1; margin-top: 10px; background: rgba(33,42,59,0.08); border-radius: 999px; height: 8px; overflow: hidden; }
		.node-bar span { display: block; height: 100%; border-radius: inherit; background: linear-gradient(90deg, #f44747 0%, #faae32 100%); width: calc(var(--width) * 1%); }
		.node-meta { position: relative; z-index: 1; margin-top: 10px; font-size: 13px; color: #364a63; display: flex; flex-wrap: wrap; gap: 12px 18px; }
		.node-warning { color: #b25600; font-weight: 600; }
		.node-children { margin-left: 24px; border-left: 1px dashed rgba(33,42,59,0.15); padding-left: 20px; }
		.insight-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
		.insight-list li { background: #fff; border-radius: 12px; padding: 14px 16px; box-shadow: 0 4px 12px rgba(13,28,39,0.10); font-size: 14px; color: #253043; display: flex; align-items: center; gap: 10px; }
		.insight-list li span.icon { font-size: 18px; }
		.insight-list li span.insight-text a { color: inherit; text-decoration: none; position: relative; }
		.insight-list li span.insight-text a::after { content: ""; position: absolute; left: 0; bottom: -2px; width: 100%; height: 1px; background: currentColor; opacity: 0.35; transition: opacity 0.2s; }
		.insight-list li span.insight-text a:hover::after { opacity: 0.65; }
		.insight-list li.severity-critical { border-left: 4px solid #f44747; }
		.insight-list li.severity-warning { border-left: 4px solid #faae32; }
		.insight-list li.severity-info { border-left: 4px solid rgba(33,42,59,0.15); }
		@media (max-width: 640px) {
			main { padding: 24px 16px 32px; }
			.list-card li { grid-template-columns: 1fr auto; grid-template-areas: "label share" "extra extra"; }
			.list-card li span:nth-child(3) { grid-area: share; }
			.list-card li span:nth-child(4) { grid-area: extra; }
		}
	</style>
	{{- end }}
</head>
<body>
	<header>
		<h1>{{.Title}}</h1>
		<p>Execution {{.Summary.ExecutionTime}} · Planning {{.Summary.PlanningTime}}</p>
		<p>Nodes {{.Summary.NodeCount}} · Hot {{.Summary.HotCount}} · Divergent {{.Summary.Divergent}}{{if .Summary.Buffers}} · Buffers {{.Summary.Buffers}}{{end}}</p>
	</header>
	<main>
		<section>
			<h2>Highlights</h2>
			<div class="summary-grid">
				<div class="summary-tile">
					<strong>Execution time</strong>
					<span>{{.Summary.ExecutionTime}}</span>
				</div>
				<div class="summary-tile">
					<strong>Planning time</strong>
					<span>{{.Summary.PlanningTime}}</span>
				</div>
				<div class="summary-tile">
					<strong>Plan nodes</strong>
					<span>{{.Summary.NodeCount}}</span>
				</div>
				<div class="summary-tile">
					<strong>Hot / Divergent</strong>
					<span>{{.Summary.HotCount}} / {{.Summary.Divergent}}</span>
				</div>
				{{- if .Summary.Buffers }}
				<div class="summary-tile">
					<strong>Total buffers</strong>
					<span>{{.Summary.Buffers}}</span>
				</div>
				{{- end }}
			</div>
		</section>

		{{- if .Insights }}
		<section>
			<h2>Insights</h2>
			<ul class="insight-list">
				{{- range .Insights }}
				<li class="severity-{{.Severity}}"><span class="icon">{{.Icon}}</span><span class="insight-text">
					{{- if .Anchor -}}
						<a href="#{{.Anchor}}">{{.Text}}</a>
					{{- else -}}
						{{.Text}}
					{{- end -}}
				</span></li>
				{{- end }}
			</ul>
		</section>
		{{- end }}

		<section>
			<h2>Signals</h2>
			<div class="flex-list">
				<div class="list-card">
					<header>
						<h3>Hot nodes</h3>
						<span>Highest self time share</span>
					</header>
					<ul>
						{{- if .HotNodes }}
							{{- range .HotNodes }}
							<li id="signals-{{.Label | urlquery}}">
								<span>{{.Label}}</span>
								<span>{{.Self}}</span>
								<span>{{.Share}}</span>
								<span>{{.Extra}}</span>
							</li>
							{{- end }}
						{{- else }}
							<li><span>No hot nodes above threshold</span></li>
						{{- end }}
					</ul>
				</div>
				<div class="list-card">
					<header>
						<h3>Estimate drift</h3>
						<span>Actual vs expected rows</span>
					</header>
					<ul>
						{{- if .Divergent }}
							{{- range .Divergent }}
							<li id="signals-{{.Label | urlquery}}">
								<span>{{.Label}}</span>
								<span>{{.Self}}</span>
								<span>{{.Share}}</span>
								<span>{{.Extra}}</span>
							</li>
							{{- end }}
						{{- else }}
							<li><span>No significant row estimate gaps</span></li>
						{{- end }}
					</ul>
				</div>
			</div>
		</section>

		<section>
			<h2>Plan Tree</h2>
			<ul class="plan-tree">
				{{ template "node" .Root }}
			</ul>
		</section>
	</main>

	{{ define "node" }}
	<li>
		<div class="node-card" id="{{.Anchor}}" style="--heat: {{printf "%.3f" .Heat}};">
		<div class="node-header">
			<span class="node-label">{{.Label}}</span>
			<span class="node-metrics">{{.Self}} · {{.Share}}</span>
		</div>
			<div class="node-bar"><span style="--width: {{printf "%.2f" .BarWidth}};"></span></div>
			<div class="node-meta">
				{{- if .Rows }}<span>{{.Rows}}</span>{{- end }}
				{{- if .Buffers }}<span>{{.Buffers}}</span>{{- end }}
				{{- if .HasWarning }}<span class="node-warning">{{ join .Warnings "; " }}</span>{{- end }}
			</div>
		</div>
		{{- if .Children }}
		<ul class="node-children">
			{{- range .Children }}
				{{ template "node" . }}
			{{- end }}
		</ul>
		{{- end }}
	</li>
	{{ end }}
</body>
</html>
`
