package tui_test

import (
	"bytes"
	"testing"

	"github.com/mickamy/xplain/internal/render/tui"
	"github.com/mickamy/xplain/test"
)

func TestRenderSampleTUI(t *testing.T) {
	analysis := test.LoadSampleAnalysis(t, "pgbench_branches.json")

	var buf bytes.Buffer
	err := tui.Render(&buf, analysis, tui.Options{EnableColor: false, MaxDepth: 2})
	if err != nil {
		t.Fatalf("render tui: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Fatalf("expected tui output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("Execution time")) {
		t.Fatalf("expected execution header in tui output")
	}
}
