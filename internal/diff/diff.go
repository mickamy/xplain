package diff

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mickamy/xplain/internal/analyzer"
	"github.com/mickamy/xplain/internal/config"
)

// Options configures the diff sensitivity.
type Options struct {
	MinSelfTimeDeltaMs float64
	MinPercentChange   float64
	MaxItems           int
}

// Report summarises the delta between two plan analyses.
type Report struct {
	Summary      SummaryDiff      `json:"summary"`
	Regressions  []Entry          `json:"regressions"`
	Improvements []Entry          `json:"improvements"`
	Insights     []insightMessage `json:"insights"`
	Options      Options          `json:"-"`
}

// SummaryDiff covers high-level execution differences.
type SummaryDiff struct {
	BaseExecutionMs   float64 `json:"base_execution_ms"`
	TargetExecutionMs float64 `json:"target_execution_ms"`
	DeltaExecutionMs  float64 `json:"delta_execution_ms"`
	PercentExecution  float64 `json:"percent_execution"`
	BasePlanningMs    float64 `json:"base_planning_ms"`
	TargetPlanningMs  float64 `json:"target_planning_ms"`
	DeltaPlanningMs   float64 `json:"delta_planning_ms"`
	PercentPlanning   float64 `json:"percent_planning"`
}

// Entry captures the delta for a set of nodes with the same signature.
type Entry struct {
	Signature        string  `json:"signature"`
	BaseSelfMs       float64 `json:"base_self_ms"`
	TargetSelfMs     float64 `json:"target_self_ms"`
	DeltaSelfMs      float64 `json:"delta_self_ms"`
	PercentChange    float64 `json:"percent_change"`
	BaseRows         float64 `json:"base_rows"`
	TargetRows       float64 `json:"target_rows"`
	BaseRowFactor    float64 `json:"base_row_factor"`
	TargetRowFactor  float64 `json:"target_row_factor"`
	BaseBuffers      float64 `json:"base_buffers"`
	TargetBuffers    float64 `json:"target_buffers"`
	DeltaBuffers     float64 `json:"delta_buffers"`
	BaseTempBlocks   float64 `json:"base_temp_blocks"`
	TargetTempBlocks float64 `json:"target_temp_blocks"`
	DeltaTempBlocks  float64 `json:"delta_temp_blocks"`
}

type insightMessage struct {
	Severity string `json:"severity"`
	Icon     string `json:"icon"`
	Message  string `json:"message"`
}

// Compare builds a diff report for two plan analyses.
func Compare(base, target *analyzer.PlanAnalysis, opts Options) (*Report, error) {
	if base == nil || base.Root == nil {
		return nil, fmt.Errorf("diff: base analysis missing")
	}
	if target == nil || target.Root == nil {
		return nil, fmt.Errorf("diff: target analysis missing")
	}

	opts = applyDefaults(opts)

	baseAgg := aggregate(base.Root)
	targetAgg := aggregate(target.Root)

	signatures := unionKeys(baseAgg, targetAgg)
	var regressions, improvements []Entry

	for _, sig := range signatures {
		baseMetrics := baseAgg[sig]
		targetMetrics := targetAgg[sig]

		entry := buildEntry(sig, baseMetrics, targetMetrics)

		if passesRegression(entry, opts) {
			regressions = append(regressions, entry)
		} else if passesImprovement(entry, opts) {
			improvements = append(improvements, entry)
		}
	}

	sort.Slice(regressions, func(i, j int) bool {
		return regressions[i].DeltaSelfMs > regressions[j].DeltaSelfMs
	})
	sort.Slice(improvements, func(i, j int) bool {
		return improvements[i].DeltaSelfMs < improvements[j].DeltaSelfMs
	})

	if opts.MaxItems > 0 {
		if len(regressions) > opts.MaxItems {
			regressions = regressions[:opts.MaxItems]
		}
		if len(improvements) > opts.MaxItems {
			improvements = improvements[:opts.MaxItems]
		}
	}

	execDelta := target.TotalTimeMs - base.TotalTimeMs
	execPct := percentChange(base.TotalTimeMs, target.TotalTimeMs)
	planDelta := target.PlanningTimeMs - base.PlanningTimeMs
	planPct := percentChange(base.PlanningTimeMs, target.PlanningTimeMs)

	report := &Report{
		Summary: SummaryDiff{
			BaseExecutionMs:   base.TotalTimeMs,
			TargetExecutionMs: target.TotalTimeMs,
			DeltaExecutionMs:  execDelta,
			PercentExecution:  execPct,
			BasePlanningMs:    base.PlanningTimeMs,
			TargetPlanningMs:  target.PlanningTimeMs,
			DeltaPlanningMs:   planDelta,
			PercentPlanning:   planPct,
		},
		Regressions:  regressions,
		Improvements: improvements,
		Options:      opts,
	}
	report.Insights = synthesizeInsights(report)
	return report, nil
}

// Markdown renders the report as a Markdown document.
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# xplain diff\n\n")
	b.WriteString("## Summary\n")
	_, _ = fmt.Fprintf(&b, "- Execution: %.3f ms → %.3f ms (%+.3f ms, %+.1f%%)\n",
		r.Summary.BaseExecutionMs, r.Summary.TargetExecutionMs,
		r.Summary.DeltaExecutionMs, r.Summary.PercentExecution)
	_, _ = fmt.Fprintf(&b, "- Planning: %.3f ms → %.3f ms (%+.3f ms, %+.1f%%)\n\n",
		r.Summary.BasePlanningMs, r.Summary.TargetPlanningMs,
		r.Summary.DeltaPlanningMs, r.Summary.PercentPlanning)

	b.WriteString("### Insights\n")
	if len(r.Insights) == 0 {
		b.WriteString("- No notable plan changes detected\n")
	} else {
		for _, insight := range r.Insights {
			b.WriteString(fmt.Sprintf("- %s %s\n", insight.Icon, insight.Message))
		}
	}
	b.WriteString("\n")

	b.WriteString("### Regressions\n")
	if len(r.Regressions) == 0 {
		b.WriteString("- None above threshold\n")
	} else {
		b.WriteString("| Operator | Base self (ms) | Target self (ms) | Δ self (ms) | Δ % | Rows (actual / est) |\n")
		b.WriteString("|---|---:|---:|---:|---:|---|\n")
		for _, entry := range r.Regressions {
			_, _ = fmt.Fprintf(&b, "| %s | %.2f | %.2f | %+.2f | %+.1f%% | %s |\n",
				entry.Signature,
				entry.BaseSelfMs,
				entry.TargetSelfMs,
				entry.DeltaSelfMs,
				entry.PercentChange,
				rowsSummary(entry))
		}
	}
	b.WriteString("\n### Improvements\n")
	if len(r.Improvements) == 0 {
		b.WriteString("- None above threshold\n")
	} else {
		b.WriteString("| Operator | Base self (ms) | Target self (ms) | Δ self (ms) | Δ % | Rows (actual / est) |\n")
		b.WriteString("|---|---:|---:|---:|---:|---|\n")
		for _, entry := range r.Improvements {
			_, _ = fmt.Fprintf(&b, "| %s | %.2f | %.2f | %+.2f | %+.1f%% | %s |\n",
				entry.Signature,
				entry.BaseSelfMs,
				entry.TargetSelfMs,
				entry.DeltaSelfMs,
				entry.PercentChange,
				rowsSummary(entry))
		}
	}
	return b.String()
}

// JSON marshals the diff report into an indented JSON document.
func (r *Report) JSON() ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil report")
	}
	type alias Report
	return json.MarshalIndent((*alias)(r), "", "  ")
}

func rowsSummary(entry Entry) string {
	base := formatRows(entry.BaseRows, entry.BaseRowFactor)
	target := formatRows(entry.TargetRows, entry.TargetRowFactor)
	return fmt.Sprintf("%s → %s", base, target)
}

func formatRows(rows, factor float64) string {
	if rows == 0 && (factor == 0 || math.IsNaN(factor)) {
		return "0"
	}
	if math.IsInf(factor, 1) {
		return fmt.Sprintf("%.0f (∞)", rows)
	}
	return fmt.Sprintf("%.0f (x%.2f)", rows, factor)
}

func synthesizeInsights(r *Report) []insightMessage {
	if r == nil {
		return nil
	}
	var insights []insightMessage
	maxItems := 3
	cfg := config.Active()
	diffCfg := cfg.Diff
	insightCfg := cfg.Insights

	for i, entry := range r.Regressions {
		if i >= maxItems {
			break
		}
		text := fmt.Sprintf("%s self +%.2f ms (+%.1f%%)", entry.Signature, entry.DeltaSelfMs, entry.PercentChange)
		if entry.DeltaTempBlocks > 0 {
			text += fmt.Sprintf(", temp +%s", humanizeBlocks(entry.DeltaTempBlocks))
		} else if entry.DeltaBuffers > 0 {
			text += fmt.Sprintf(", buffers +%s", humanizeBlocks(entry.DeltaBuffers))
		}
		icon := "🔥"
		level := "critical"
		if entry.DeltaSelfMs < diffCfg.CriticalDeltaMs && entry.DeltaSelfMs >= diffCfg.WarningDeltaMs {
			icon = "⚠️"
			level = "warning"
		} else if entry.DeltaSelfMs < diffCfg.WarningDeltaMs {
			icon = "⚠️"
			level = "warning"
		}
		insights = append(insights, insightMessage{Severity: level, Icon: icon, Message: text})
	}

	for i, entry := range r.Improvements {
		if i >= maxItems {
			break
		}
		text := fmt.Sprintf("%s self %.2f ms (%.1f%%)", entry.Signature, entry.DeltaSelfMs, entry.PercentChange)
		if entry.DeltaTempBlocks < 0 {
			text += fmt.Sprintf(", temp %s", humanizeBlocks(entry.DeltaTempBlocks))
		} else if entry.DeltaBuffers < 0 {
			text += fmt.Sprintf(", buffers %s", humanizeBlocks(entry.DeltaBuffers))
		}
		insights = append(insights, insightMessage{Severity: "improvement", Icon: "✅", Message: text})
	}

	for _, entry := range r.Regressions {
		if entry.BaseTempBlocks == 0 && entry.TargetTempBlocks >= insightCfg.SpillNewBlocks {
			text := fmt.Sprintf("%s began spilling to disk: %.0f temp buffers (~%s)", entry.Signature, entry.TargetTempBlocks, humanizeBlocks(entry.TargetTempBlocks))
			insights = append(insights, insightMessage{Severity: "warning", Icon: "⚠️", Message: text})
		}
	}

	return insights
}

func humanizeBlocks(blocks float64) string {
	if blocks == 0 {
		return "0 B"
	}
	const blockSize = 8192
	sign := ""
	if blocks < 0 {
		blocks = -blocks
		sign = "-"
	}
	bytes := blocks * blockSize
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	idx := 0
	for bytes >= 1024 && idx < len(units)-1 {
		bytes /= 1024
		idx++
	}
	return fmt.Sprintf("%s%.2f %s", sign, bytes, units[idx])
}

type aggregated struct {
	SelfMs        float64
	ActualRows    float64
	EstimatedRows float64
	Buffers       float64
	TempBlocks    float64
}

func aggregate(root *analyzer.NodeStats) map[string]aggregated {
	result := map[string]aggregated{}
	var walk func(*analyzer.NodeStats)
	walk = func(n *analyzer.NodeStats) {
		sig := signature(n)
		entry := result[sig]
		entry.SelfMs += n.ExclusiveTimeMs
		entry.ActualRows += n.ActualTotalRows
		entry.EstimatedRows += n.EstimatedRows
		entry.Buffers += float64(n.Buffers.Total())
		entry.TempBlocks += float64(n.Buffers.TempRead + n.Buffers.TempWritten)
		result[sig] = entry
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return result
}

func signature(node *analyzer.NodeStats) string {
	parts := []string{node.Node.NodeType}
	if node.Node.RelationName != "" {
		parts = append(parts, node.Node.RelationName)
	}
	if node.Node.IndexName != "" {
		parts = append(parts, node.Node.IndexName)
	}
	if node.Node.JoinType != "" {
		parts = append(parts, node.Node.JoinType)
	}
	return strings.Join(parts, " · ")
}

func unionKeys(base, target map[string]aggregated) []string {
	seen := map[string]struct{}{}
	for k := range base {
		seen[k] = struct{}{}
	}
	for k := range target {
		seen[k] = struct{}{}
	}
	all := make([]string, 0, len(seen))
	for k := range seen {
		all = append(all, k)
	}
	sort.Strings(all)
	return all
}

func buildEntry(sig string, base, target aggregated) Entry {
	baseFactor := ratio(base.ActualRows, base.EstimatedRows)
	targetFactor := ratio(target.ActualRows, target.EstimatedRows)
	return Entry{
		Signature:        sig,
		BaseSelfMs:       base.SelfMs,
		TargetSelfMs:     target.SelfMs,
		DeltaSelfMs:      target.SelfMs - base.SelfMs,
		PercentChange:    percentChange(base.SelfMs, target.SelfMs),
		BaseRows:         base.ActualRows,
		TargetRows:       target.ActualRows,
		BaseRowFactor:    baseFactor,
		TargetRowFactor:  targetFactor,
		BaseBuffers:      base.Buffers,
		TargetBuffers:    target.Buffers,
		DeltaBuffers:     target.Buffers - base.Buffers,
		BaseTempBlocks:   base.TempBlocks,
		TargetTempBlocks: target.TempBlocks,
		DeltaTempBlocks:  target.TempBlocks - base.TempBlocks,
	}
}

func passesRegression(entry Entry, opts Options) bool {
	return entry.DeltaSelfMs >= opts.MinSelfTimeDeltaMs && entry.PercentChange >= opts.MinPercentChange
}

func passesImprovement(entry Entry, opts Options) bool {
	return entry.DeltaSelfMs <= -opts.MinSelfTimeDeltaMs && entry.PercentChange <= -opts.MinPercentChange
}

func ratio(actual, estimated float64) float64 {
	const eps = 1e-9
	if estimated <= eps {
		if actual <= eps {
			return 1
		}
		return math.Inf(1)
	}
	return actual / estimated
}

func percentChange(base, target float64) float64 {
	const eps = 1e-9
	if math.Abs(base) <= eps {
		if math.Abs(target) <= eps {
			return 0
		}
		if target > 0 {
			return 100
		}
		return -100
	}
	return (target - base) / base * 100
}

func applyDefaults(opts Options) Options {
	cfg := config.Active().Diff
	if opts.MinSelfTimeDeltaMs <= 0 {
		opts.MinSelfTimeDeltaMs = cfg.MinSelfDeltaMs
	}
	if opts.MinPercentChange <= 0 {
		opts.MinPercentChange = cfg.MinPercentChange
	}
	if opts.MaxItems <= 0 {
		opts.MaxItems = cfg.MaxItems
	}
	return opts
}
