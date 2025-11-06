package insight

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mickamy/xplain/internal/analyzer"
	"github.com/mickamy/xplain/internal/config"
)

// Severity expresses the urgency of an insight message.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Message represents an actionable observation about a plan.
type Message struct {
	Severity Severity
	Text     string
	Anchor   string
}

// BuildMessages derives human-readable insight messages for a plan.
func BuildMessages(analysis *analyzer.PlanAnalysis) []Message {
	if analysis == nil {
		return nil
	}
	var out []Message

	if msg := hotspotMessage(analysis); msg != nil {
		out = append(out, *msg)
	}

	for _, msg := range driftMessages(analysis) {
		out = append(out, msg)
	}

	if msg := bufferMessage(analysis); msg != nil {
		out = append(out, *msg)
	}

	if msg := parallelLimitMessage(analysis); msg != nil {
		out = append(out, *msg)
	}

	for _, msg := range spillMessages(analysis) {
		out = append(out, msg)
	}

	for _, msg := range nestedLoopMessages(analysis) {
		out = append(out, msg)
	}

	return out
}

func hotspotMessage(analysis *analyzer.PlanAnalysis) *Message {
	if len(analysis.HotNodes) == 0 {
		return nil
	}
	cfg := config.Active().Insights
	hot := analysis.HotNodes[0]
	text := fmt.Sprintf("Hot spot: %s self %.2f ms (%.1f%%)", CompactLabel(hot), hot.ExclusiveTimeMs, hot.PercentExclusive*100)
	if buf := hot.Buffers.Total(); buf > 0 {
		text += fmt.Sprintf(", buffers %d (~%s)", buf, HumanizeBuffers(buf))
	}
	if strings.Contains(hot.Node.NodeType, "Seq Scan") && int64(hot.Buffers.Total()) > cfg.SeqScanBufferHint {
		text += " — consider adding an index or tightening the filter"
	}
	severity := severityForHotspot(hot)
	return &Message{Severity: severity, Text: text, Anchor: AnchorID(hot)}
}

func severityForHotspot(node *analyzer.NodeStats) Severity {
	if node == nil {
		return SeverityInfo
	}
	cfg := config.Active().Insights
	switch {
	case node.PercentExclusive >= cfg.HotspotCriticalPercent:
		return SeverityCritical
	case node.PercentExclusive >= cfg.HotspotWarningPercent:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

func driftMessages(analysis *analyzer.PlanAnalysis) []Message {
	if len(analysis.DivergentNodes) == 0 {
		return nil
	}
	cfg := config.Active().Insights
	max := 2
	var msgs []Message
	for i, node := range analysis.DivergentNodes {
		if i >= max {
			break
		}
		ratio := node.RowEstimateFactor
		text := fmt.Sprintf("Estimate drift: %s expected %.0f got %.0f", CompactLabel(node), node.EstimatedRows, node.ActualTotalRows)
		if !math.IsNaN(ratio) && !math.IsInf(ratio, 0) {
			text += fmt.Sprintf(" (x%.2f)", ratio)
		} else if math.IsInf(ratio, 1) {
			text += " (∞)"
		}
		text += " — update statistics (ANALYZE) or review estimates"
		severity := SeverityWarning
		if ratio >= cfg.RowEstimateCriticalHigh || ratio <= cfg.RowEstimateCriticalLow {
			severity = SeverityCritical
		}
		msgs = append(msgs, Message{Severity: severity, Text: text, Anchor: AnchorID(node)})
	}
	return msgs
}

func bufferMessage(analysis *analyzer.PlanAnalysis) *Message {
	candidate := selectBufferCandidate(analysis)
	if candidate == nil {
		return nil
	}
	cfg := config.Active().Insights
	buf := candidate.Buffers.Total()
	text := fmt.Sprintf("Buffer churn: %s touched %d buffers (~%s)", CompactLabel(candidate), buf, HumanizeBuffers(buf))
	severity := SeverityInfo
	switch {
	case buf >= cfg.BufferCriticalBlocks:
		severity = SeverityCritical
	case buf >= cfg.BufferWarningBlocks:
		severity = SeverityWarning
	}
	return &Message{Severity: severity, Text: text, Anchor: AnchorID(candidate)}
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

func parallelLimitMessage(analysis *analyzer.PlanAnalysis) *Message {
	if analysis == nil || analysis.Root == nil {
		return nil
	}
	cfg := config.Active().Insights
	var candidate *analyzer.NodeStats
	walkNodes(analysis.Root, func(node *analyzer.NodeStats) {
		if candidate != nil {
			return
		}
		if node.Node == nil || node.Parent == nil {
			return
		}
		if !(node.Node.NodeType == "Gather" || node.Node.NodeType == "Gather Merge") {
			return
		}
		if node.Parent.Node == nil || node.Parent.Node.NodeType != "Limit" {
			return
		}
		if node.EstimatedRows <= 0 {
			return
		}
		if node.ActualTotalRows/node.EstimatedRows >= cfg.ParallelLimitKeepRatio {
			return
		}
		candidate = node
	})
	if candidate == nil {
		return nil
	}
	text := fmt.Sprintf("Parallel gather reads %.0f rows but LIMIT keeps %.0f — consider adding an index or reducing parallelism", candidate.EstimatedRows, candidate.ActualTotalRows)
	return &Message{Severity: SeverityWarning, Text: text, Anchor: AnchorID(candidate)}
}

func spillMessages(analysis *analyzer.PlanAnalysis) []Message {
	if analysis == nil || analysis.Root == nil {
		return nil
	}
	cfg := config.Active().Insights
	var candidates []*analyzer.NodeStats
	walkNodes(analysis.Root, func(node *analyzer.NodeStats) {
		if node == nil || node.Node == nil {
			return
		}
		tempBlocks := node.Buffers.TempRead + node.Buffers.TempWritten
		if float64(tempBlocks) < cfg.SpillNewBlocks {
			return
		}
		switch node.Node.NodeType {
		case "Sort", "Incremental Sort", "Hash", "Hash Join":
			candidates = append(candidates, node)
		}
	})
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		ti := candidates[i].Buffers.TempRead + candidates[i].Buffers.TempWritten
		tj := candidates[j].Buffers.TempRead + candidates[j].Buffers.TempWritten
		return ti > tj
	})
	limit := 2
	if len(candidates) < limit {
		limit = len(candidates)
	}
	var msgs []Message
	for _, node := range candidates[:limit] {
		tempBlocks := node.Buffers.TempRead + node.Buffers.TempWritten
		label := CompactLabel(node)
		text := fmt.Sprintf("%s spilled to disk: %s used %d temp buffers (~%s)", node.Node.NodeType, label, tempBlocks, HumanizeBuffers(tempBlocks))
		switch node.Node.NodeType {
		case "Sort", "Incremental Sort":
			text += " — consider increasing work_mem or adding a supporting index"
		default:
			text += " — consider increasing work_mem or rewriting the join"
		}
		severity := SeverityWarning
		if tempBlocks >= 20000 {
			severity = SeverityCritical
		} else if tempBlocks < 2000 {
			severity = SeverityInfo
		}
		msgs = append(msgs, Message{Severity: severity, Text: text, Anchor: AnchorID(node)})
	}
	return msgs
}

func nestedLoopMessages(analysis *analyzer.PlanAnalysis) []Message {
	if analysis == nil || analysis.Root == nil {
		return nil
	}
	cfg := config.Active().Insights
	var msgs []Message
	walkNodes(analysis.Root, func(node *analyzer.NodeStats) {
		if node == nil || node.Node == nil || node.Node.NodeType != "Nested Loop" {
			return
		}
		for _, child := range node.Children {
			if child == nil || child.Node == nil {
				continue
			}
			if child.ActualLoops <= cfg.NestedLoopWarnLoops {
				continue
			}
			if !strings.Contains(child.Node.NodeType, "Scan") {
				continue
			}
			text := fmt.Sprintf("Nested Loop: %s invoked %s %.0f times — consider adding an index or rewriting the join order",
				CompactLabel(node), CompactLabel(child), child.ActualLoops)
			severity := SeverityWarning
			if child.ActualLoops >= cfg.NestedLoopCriticalLoops {
				severity = SeverityCritical
			} else if child.ActualLoops < cfg.NestedLoopWarnLoops*2 {
				severity = SeverityInfo
			}
			msgs = append(msgs, Message{Severity: severity, Text: text, Anchor: AnchorID(node)})
			break
		}
	})
	if len(msgs) > 2 {
		return msgs[:2]
	}
	return msgs
}

func walkNodes(node *analyzer.NodeStats, fn func(*analyzer.NodeStats)) {
	if node == nil {
		return
	}
	fn(node)
	for _, child := range node.Children {
		walkNodes(child, fn)
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
func AnchorID(node *analyzer.NodeStats) string {
	if node == nil {
		return ""
	}
	label := NodeLabel(node)
	label = strings.ToLower(label)
	label = strings.ReplaceAll(label, " ", "-")
	label = strings.ReplaceAll(label, "/", "-")
	label = strings.ReplaceAll(label, "\\", "-")
	label = strings.ReplaceAll(label, "(", "")
	label = strings.ReplaceAll(label, ")", "")
	label = strings.ReplaceAll(label, ",", "")
	label = strings.ReplaceAll(label, "--", "-")
	return label
}
