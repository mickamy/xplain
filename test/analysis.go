package test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mickamy/xplain/internal/analyzer"
	"github.com/mickamy/xplain/internal/parser"
)

var (
	rootPath string
	once     sync.Once
)

// RootPath resolves a path relative to the repository rootPath (where go.mod resides).
func RootPath(t *testing.T) string {
	t.Helper()
	once.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		for {
			if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
				rootPath = wd
				break
			}
			next := filepath.Dir(wd)
			if next == wd {
				t.Fatalf("go.mod not found from %s", wd)
			}
			wd = next
		}
	})
	return rootPath
}

// LoadSampleAnalysis loads and analyzes a plan relative to the repository rootPath.
func LoadSampleAnalysis(t *testing.T, rel string) *analyzer.PlanAnalysis {
	t.Helper()
	root := RootPath(t)
	f, err := os.Open(filepath.Join(root, "samples", rel))
	if err != nil {
		t.Fatalf("open plan: %v", err)
	}
	defer func() { _ = f.Close() }()

	plan, err := parser.ParseJSON(f)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	analysis, err := analyzer.Analyze(plan)
	if err != nil {
		t.Fatalf("analyze plan: %v", err)
	}
	return analysis
}
