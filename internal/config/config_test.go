package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mickamy/xplain/test"
)

func TestApplyDefaultAndFile(t *testing.T) {
	Use(Default())
	t.Cleanup(func() { Use(Default()) })

	if Active().Insights.HotspotCriticalPercent == 0 {
		t.Fatalf("expected default hotspot threshold to be non-zero")
	}

	root := test.RootPath(t)
	path := filepath.Join(root, "samples", "config.example.json")
	if err := Apply(path); err != nil {
		t.Fatalf("apply config: %v", err)
	}

	cfg := Active()
	if cfg.Insights.HotspotCriticalPercent != 0.5 {
		t.Fatalf("expected hotspot threshold from sample config, got %v", cfg.Insights.HotspotCriticalPercent)
	}
	if cfg.Diff.MaxItems != 12 {
		t.Fatalf("expected diff max items from sample config, got %v", cfg.Diff.MaxItems)
	}

	if err := Apply(""); err != nil {
		t.Fatalf("reset config: %v", err)
	}
	if Active().Diff.MaxItems == 0 {
		t.Fatalf("expected defaults restored")
	}
}

func TestApplyMissingFile(t *testing.T) {
	if err := Apply(filepath.Join(os.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatalf("expected error for missing config file")
	}
}
