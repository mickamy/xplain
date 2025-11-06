package analyzer

import (
	"fmt"
	"math"
	"sort"

	"github.com/mickamy/xplain/internal/model"
)

// PlanAnalysis contains derived metrics for a parsed plan.
type PlanAnalysis struct {
	Root            *NodeStats
	PlanningTimeMs  float64
	ExecutionTimeMs float64
	TotalTimeMs     float64
	NodeCount       int
	HotNodes        []*NodeStats
	DivergentNodes  []*NodeStats
}

// NodeStats augments a plan node with computed statistics.
type NodeStats struct {
	Node              *model.PlanNode
	Depth             int
	InclusiveTimeMs   float64
	ExclusiveTimeMs   float64
	PercentExclusive  float64
	PercentInclusive  float64
	ActualTotalRows   float64
	EstimatedRows     float64
	RowEstimateFactor float64
	Buffers           BufferTotals
	Warnings          []string
	Children          []*NodeStats
}

// BufferTotals mirrors the buffer counters for easier reporting.
type BufferTotals struct {
	SharedHit     int64
	SharedRead    int64
	SharedDirtied int64
	SharedWritten int64
	LocalHit      int64
	LocalRead     int64
	LocalDirtied  int64
	LocalWritten  int64
	TempRead      int64
	TempWritten   int64
}

// Total returns the sum of all buffer counters.
func (b BufferTotals) Total() int64 {
	return b.SharedHit + b.SharedRead + b.SharedDirtied + b.SharedWritten +
		b.LocalHit + b.LocalRead + b.LocalDirtied + b.LocalWritten + b.TempRead + b.TempWritten
}

// Analyze derives metrics for the provided plan.
func Analyze(explain *model.Explain) (*PlanAnalysis, error) {
	if explain == nil || explain.Plan == nil {
		return nil, fmt.Errorf("analyze: missing plan")
	}

	root := buildStats(explain.Plan, 0)
	totalTime := root.InclusiveTimeMs

	annotateRatios(root, totalTime)

	allNodes := flatten(root)

	hot := selectHotNodes(allNodes)
	divergent := selectDivergentNodes(allNodes)

	return &PlanAnalysis{
		Root:            root,
		PlanningTimeMs:  explain.PlanningTime,
		ExecutionTimeMs: explain.ExecutionTime,
		TotalTimeMs:     totalTime,
		NodeCount:       len(allNodes),
		HotNodes:        hot,
		DivergentNodes:  divergent,
	}, nil
}

func buildStats(node *model.PlanNode, depth int) *NodeStats {
	loops := node.ActualLoops
	if loops <= 0 {
		loops = 1
	}

	inclusive := node.ActualTotalTime * loops

	stats := &NodeStats{
		Node:            node,
		Depth:           depth,
		InclusiveTimeMs: inclusive,
		ActualTotalRows: node.ActualRows * loops,
		EstimatedRows:   node.PlanRows * loops,
		Buffers: BufferTotals{
			SharedHit:     node.Buffers.SharedHit,
			SharedRead:    node.Buffers.SharedRead,
			SharedDirtied: node.Buffers.SharedDirtied,
			SharedWritten: node.Buffers.SharedWritten,
			LocalHit:      node.Buffers.LocalHit,
			LocalRead:     node.Buffers.LocalRead,
			LocalDirtied:  node.Buffers.LocalDirtied,
			LocalWritten:  node.Buffers.LocalWritten,
			TempRead:      node.Buffers.TempRead,
			TempWritten:   node.Buffers.TempWritten,
		},
	}

	var childTime float64
	for _, childNode := range node.Children {
		child := buildStats(childNode, depth+1)
		stats.Children = append(stats.Children, child)
		childTime += child.InclusiveTimeMs
	}

	stats.ExclusiveTimeMs = inclusive - childTime
	if stats.ExclusiveTimeMs < 0 {
		if math.Abs(stats.ExclusiveTimeMs) < 1e-6 {
			stats.ExclusiveTimeMs = 0
		} else {
			stats.ExclusiveTimeMs = 0
		}
	}

	stats.RowEstimateFactor = computeEstimateFactor(stats.EstimatedRows, stats.ActualTotalRows)
	stats.Warnings = append(stats.Warnings, deriveWarnings(stats)...)

	return stats
}

func annotateRatios(node *NodeStats, total float64) {
	if total > 0 {
		node.PercentExclusive = node.ExclusiveTimeMs / total
		node.PercentInclusive = node.InclusiveTimeMs / total
	}
	for _, child := range node.Children {
		annotateRatios(child, total)
	}
}

func flatten(root *NodeStats) []*NodeStats {
	var out []*NodeStats
	var walk func(*NodeStats)
	walk = func(n *NodeStats) {
		out = append(out, n)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return out
}

func selectHotNodes(nodes []*NodeStats) []*NodeStats {
	if len(nodes) == 0 {
		return nil
	}

	candidates := make([]*NodeStats, 0, len(nodes))
	for _, n := range nodes {
		if n.PercentExclusive > 0 {
			candidates = append(candidates, n)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].PercentExclusive > candidates[j].PercentExclusive
	})

	limit := 5
	if len(candidates) < limit {
		limit = len(candidates)
	}
	cutoff := 0.10

	var out []*NodeStats
	for _, candidate := range candidates[:limit] {
		if candidate.PercentExclusive < cutoff {
			break
		}
		out = append(out, candidate)
	}

	if len(out) == 0 && len(candidates) > 0 {
		out = candidates[:limit]
	}

	return out
}

func selectDivergentNodes(nodes []*NodeStats) []*NodeStats {
	var out []*NodeStats
	for _, n := range nodes {
		if math.IsInf(n.RowEstimateFactor, 1) || math.IsInf(n.RowEstimateFactor, -1) {
			out = append(out, n)
			continue
		}
		if n.RowEstimateFactor >= 2.0 || n.RowEstimateFactor <= 0.5 {
			if n.EstimatedRows > 0 || n.ActualTotalRows > 0 {
				out = append(out, n)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return math.Abs(out[i].RowEstimateFactor-1) > math.Abs(out[j].RowEstimateFactor-1)
	})
	limit := 5
	if len(out) < limit {
		limit = len(out)
	}
	return out[:limit]
}

func computeEstimateFactor(estimated, actual float64) float64 {
	const epsilon = 1e-9
	if estimated <= epsilon {
		if actual <= epsilon {
			return 1
		}
		return math.Inf(1)
	}
	return actual / estimated
}

func deriveWarnings(stats *NodeStats) []string {
	var warnings []string
	if stats.PercentExclusive >= 0.20 {
		warnings = append(warnings, fmt.Sprintf("self time %.1f%% of plan", stats.PercentExclusive*100))
	}
	if stats.RowEstimateFactor >= 2.0 {
		warnings = append(warnings, fmt.Sprintf("rows %.1fx higher than estimate", stats.RowEstimateFactor))
	} else if stats.RowEstimateFactor <= 0.5 {
		warnings = append(warnings, fmt.Sprintf("rows %.1fx lower than estimate", stats.RowEstimateFactor))
	}
	if stats.Buffers.Total() > 0 && stats.PercentExclusive >= 0.05 {
		warnings = append(warnings, "heavy buffer usage")
	}
	return warnings
}
