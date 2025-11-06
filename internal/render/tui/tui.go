package tui

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/mickamy/xplain/internal/analyzer"
	"github.com/mickamy/xplain/internal/insight"
)

// Options controls how the TUI renderer behaves.
type Options struct {
	EnableColor  bool
	MaxDepth     int
	ShowWarnings bool
	BarWidth     int
}

// Render prints an ASCII tree that highlights hot nodes and row estimation issues.
func Render(w io.Writer, analysis *analyzer.PlanAnalysis, opts Options) error {
	if w == nil {
		return errors.New("tui: writer is nil")
	}
	if analysis == nil || analysis.Root == nil {
		return errors.New("tui: empty analysis")
	}

	if opts.BarWidth <= 0 {
		opts.BarWidth = 20
	}

	_, _ = fmt.Fprintf(w, "Execution time %.3f ms (planning %.3f ms)\n", analysis.TotalTimeMs, analysis.PlanningTimeMs)
	_, _ = fmt.Fprintf(w, "Nodes %d | Hot nodes >=10%% runtime %d | Divergent estimates %d\n\n",
		analysis.NodeCount, len(analysis.HotNodes), len(analysis.DivergentNodes))

	renderInsights(w, analysis, opts)

	_, _ = fmt.Fprintf(w, "%s\n", renderLine(analysis.Root, opts))
	printChildren(w, analysis.Root, "", opts)

	return nil
}

func printChildren(w io.Writer, parent *analyzer.NodeStats, prefix string, opts Options) {
	for i, child := range parent.Children {
		renderBranch(w, child, prefix, i == len(parent.Children)-1, opts)
	}
}

func renderBranch(w io.Writer, node *analyzer.NodeStats, prefix string, isLast bool, opts Options) {
	connector := "|-- "
	childPrefix := prefix + "|   "
	if isLast {
		connector = "`-- "
		childPrefix = prefix + "    "
	}

	line := renderLine(node, opts)
	_, _ = fmt.Fprintf(w, "%s%s%s\n", prefix, connector, line)

	if opts.MaxDepth > 0 && node.Depth >= opts.MaxDepth {
		if len(node.Children) > 0 {
			_, _ = fmt.Fprintf(w, "%s`-- ... (%d more nodes)\n", childPrefix, countDescendants(node))
		}
		return
	}

	printChildren(w, node, childPrefix, opts)
}

func renderLine(node *analyzer.NodeStats, opts Options) string {
	label := insight.NodeLabel(node)

	self := fmt.Sprintf("self %.2f ms (workers)", node.ExclusiveTimeMs)
	share := fmt.Sprintf("%5.1f%%", node.PercentExclusive*100)

	bar := drawBar(node.PercentExclusive, opts.BarWidth)
	barColor := pickColor(node.PercentExclusive)
	if !opts.EnableColor {
		barColor = ""
	}
	if barColor != "" {
		bar = applyColor(bar, barColor)
	}

	rowInfo := ""
	if node.EstimatedRows > 0 || node.ActualTotalRows > 0 {
		rowInfo = fmt.Sprintf("rows %.0f/%.0f", node.ActualTotalRows, node.EstimatedRows)
		if node.RowEstimateFactor > 0 && !math.IsInf(node.RowEstimateFactor, 0) {
			rowInfo += fmt.Sprintf(" (x%.2f)", node.RowEstimateFactor)
		} else if math.IsInf(node.RowEstimateFactor, 1) {
			rowInfo += " (∞)"
		}
	}

	bufferInfo := ""
	if node.Buffers.Total() > 0 {
		bufferInfo = fmt.Sprintf("buf %d (~%s)", node.Buffers.Total(), insight.HumanizeBuffers(node.Buffers.Total()))
	}

	warningText := ""
	if opts.ShowWarnings && len(node.Warnings) > 0 {
		warningText = strings.Join(node.Warnings, "; ")
		if opts.EnableColor {
			warningText = applyColor(warningText, "yellow")
		}
		warningText = " [" + warningText + "]"
	} else if len(node.Warnings) > 0 {
		warningText = " [" + strings.Join(node.Warnings, "; ") + "]"
	}

	parts := []string{label, self, share, bar}
	if rowInfo != "" {
		parts = append(parts, rowInfo)
	}
	if bufferInfo != "" {
		parts = append(parts, bufferInfo)
	}

	return strings.Join(parts, " | ") + warningText
}

func renderInsights(w io.Writer, analysis *analyzer.PlanAnalysis, opts Options) {
	messages := insight.BuildMessages(analysis)
	if len(messages) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "Insights:")
	for _, msg := range messages {
		icon := severityIcon(msg.Severity)
		_, _ = fmt.Fprintf(w, "  - %s %s\n", icon, msg.Text)
	}
	_, _ = fmt.Fprintln(w)
}

func drawBar(ratio float64, width int) string {
	if width <= 0 {
		return ""
	}
	clamped := ratio
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 1 {
		clamped = 1
	}
	fill := int(math.Round(clamped * float64(width)))
	if clamped > 0 && fill == 0 {
		fill = 1
	}
	if fill > width {
		fill = width
	}
	return strings.Repeat("#", fill) + strings.Repeat("-", width-fill)
}

func pickColor(ratio float64) string {
	switch {
	case ratio >= 0.40:
		return "red"
	case ratio >= 0.20:
		return "yellow"
	case ratio >= 0.10:
		return "cyan"
	default:
		return ""
	}
}

func applyColor(text, color string) string {
	code := ""
	switch color {
	case "red":
		code = "\033[31m"
	case "yellow":
		code = "\033[33m"
	case "cyan":
		code = "\033[36m"
	default:
		return text
	}
	return code + text + "\033[0m"
}

func countDescendants(node *analyzer.NodeStats) int {
	total := 0
	var walk func(*analyzer.NodeStats)
	walk = func(n *analyzer.NodeStats) {
		for _, child := range n.Children {
			total++
			walk(child)
		}
	}
	walk(node)
	return total
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
