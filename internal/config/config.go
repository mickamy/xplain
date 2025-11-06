package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Config holds tunable thresholds for insight scoring and diff reporting.
type Config struct {
	Insights InsightConfig `json:"insights"`
	Diff     DiffConfig    `json:"diff"`
}

// InsightConfig defines thresholds for insight generation.
type InsightConfig struct {
	HotspotCriticalPercent  float64 `json:"hotspot_critical_percent"`
	HotspotWarningPercent   float64 `json:"hotspot_warning_percent"`
	SeqScanBufferHint       int64   `json:"seq_scan_buffer_hint"`
	BufferWarningBlocks     int64   `json:"buffer_warning_blocks"`
	BufferCriticalBlocks    int64   `json:"buffer_critical_blocks"`
	NestedLoopWarnLoops     float64 `json:"nested_loop_warn_loops"`
	NestedLoopCriticalLoops float64 `json:"nested_loop_critical_loops"`
	RowEstimateCriticalHigh float64 `json:"row_estimate_critical_high"`
	RowEstimateCriticalLow  float64 `json:"row_estimate_critical_low"`
	SpillNewBlocks          float64 `json:"spill_new_blocks"`
	ParallelLimitKeepRatio  float64 `json:"parallel_limit_keep_ratio"`
}

// DiffConfig defines thresholds for diff summaries.
type DiffConfig struct {
	MinSelfDeltaMs   float64 `json:"min_self_delta_ms"`
	MinPercentChange float64 `json:"min_percent_change"`
	MaxItems         int     `json:"max_items"`
	CriticalDeltaMs  float64 `json:"critical_delta_ms"`
	WarningDeltaMs   float64 `json:"warning_delta_ms"`
}

var (
	mu     sync.RWMutex
	active = Default()
)

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Insights: InsightConfig{
			HotspotCriticalPercent:  0.40,
			HotspotWarningPercent:   0.20,
			SeqScanBufferHint:       5000,
			BufferWarningBlocks:     5000,
			BufferCriticalBlocks:    50000,
			NestedLoopWarnLoops:     100,
			NestedLoopCriticalLoops: 10000,
			RowEstimateCriticalHigh: 5.0,
			RowEstimateCriticalLow:  0.2,
			SpillNewBlocks:          100,
			ParallelLimitKeepRatio:  0.10,
		},
		Diff: DiffConfig{
			MinSelfDeltaMs:   2.0,
			MinPercentChange: 5.0,
			MaxItems:         8,
			CriticalDeltaMs:  10.0,
			WarningDeltaMs:   5.0,
		},
	}
}

// Active returns the currently applied configuration.
func Active() Config {
	mu.RLock()
	defer mu.RUnlock()
	return active
}

// Use replaces the active configuration.
func Use(cfg Config) {
	mu.Lock()
	active = cfg
	mu.Unlock()
}

// Apply loads configuration from the provided path (JSON). Empty path resets to default.
func Apply(path string) error {
	if path == "" {
		Use(Default())
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	Use(cfg)
	return nil
}
