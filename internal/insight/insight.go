package insight

import (
	"fmt"
	"math"
	"strings"

	"github.com/mickamy/xplain/internal/analyzer"
)

// BuildMessages derives human-readable insight strings for a plan.
func BuildMessages(analysis *analyzer.PlanAnalysis) []string {
	if analysis == nil {
		return nil
	}
	var out []string

	if len(analysis.HotNodes) > 0 {
		hot := analysis.HotNodes[0]
		msg := fmt.Sprintf("Hot spot: %s self %.2f ms (%.1f%%)", CompactLabel(hot), hot.ExclusiveTimeMs, hot.PercentExclusive*100)
		if buf := hot.Buffers.Total(); buf > 0 {
			msg += fmt.Sprintf(", buffers %d (~%s)", buf, HumanizeBuffers(buf))
		}
		out = append(out, msg)
	}

	maxDivergent := 2
	for i, node := range analysis.DivergentNodes {
		if i >= maxDivergent {
			break
		}
		msg := fmt.Sprintf("Estimate drift: %s expected %.0f got %.0f", CompactLabel(node), node.EstimatedRows, node.ActualTotalRows)
		if ratio := node.RowEstimateFactor; !math.IsNaN(ratio) && !math.IsInf(ratio, 0) {
			msg += fmt.Sprintf(" (x%.2f)", ratio)
		} else if math.IsInf(ratio, 1) {
			msg += " (∞)"
		}
		out = append(out, msg)
	}

	if candidate := selectBufferCandidate(analysis); candidate != nil {
		buf := candidate.Buffers.Total()
		out = append(out, fmt.Sprintf("Buffer churn: %s touched %d buffers (~%s)", CompactLabel(candidate), buf, HumanizeBuffers(buf)))
	}

	return out
}

func selectBufferCandidate(analysis *analyzer.PlanAnalysis) *analyzer.NodeStats {
	if analysis == nil || len(analysis.BufferHeavy) == 0 {
		return nil
	}

	if len(analysis.HotNodes) > 0 {
		hot := analysis.HotNodes[0]
		if hot.Buffers.Total() > 0 && !isWrapperNode(hot.Node.NodeType) {
			return hot
		}
	}

	for _, node := range analysis.BufferHeavy {
		if node.Buffers.Total() == 0 {
			continue
		}
		if isWrapperNode(node.Node.NodeType) {
			continue
		}
		return node
	}
	return analysis.BufferHeavy[0]
}

func isWrapperNode(nodeType string) bool {
	switch nodeType {
	case "Limit", "Sort", "Gather", "Gather Merge", "Incremental Sort", "Unique", "Materialize":
		return true
	default:
		return false
	}
}

// NodeLabel builds a descriptive label for a plan node.
func NodeLabel(node *analyzer.NodeStats) string {
	if node == nil {
		return ""
	}
	label := node.Node.NodeType
	if node.Node.RelationName != "" {
		label = fmt.Sprintf("%s %s", label, node.Node.RelationName)
		if node.Node.Alias != "" && node.Node.Alias != node.Node.RelationName {
			label = fmt.Sprintf("%s (%s)", label, node.Node.Alias)
		}
	} else if node.Node.Alias != "" {
		label = fmt.Sprintf("%s (%s)", label, node.Node.Alias)
	}
	return label
}

// CompactLabel shortens long labels for inline summaries.
func CompactLabel(node *analyzer.NodeStats) string {
	label := NodeLabel(node)
	if len(label) > 60 {
		return label[:57] + "..."
	}
	return label
}

// HumanizeBuffers converts a buffer count into a readable size using 8KiB blocks.
func HumanizeBuffers(blocks int64) string {
	if blocks <= 0 {
		return "0"
	}
	const blockSize = 8192
	bytes := float64(blocks * blockSize)
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.2f GiB", bytes/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.2f MiB", bytes/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.2f KiB", bytes/(1<<10))
	default:
		return fmt.Sprintf("%.0f B", bytes)
	}
}

// SummarizeTotalBuffers builds a human readable total buffer summary.
func SummarizeTotalBuffers(total int64) string {
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("%d blocks (~%s)", total, HumanizeBuffers(total))
}

// NormalizeWhitespace collapses whitespace for use in HTML or text.
func NormalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
