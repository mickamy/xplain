package html_test

import (
	"bytes"
	"testing"

	"github.com/mickamy/xplain/internal/render/html"
	"github.com/mickamy/xplain/test"
)

func TestRenderSampleHTML(t *testing.T) {
	analysis := test.LoadSampleAnalysis(t, "pgbench_hot.json")

	var buf bytes.Buffer
	if err := html.Render(&buf, analysis, html.Options{Title: "test", IncludeStyles: true}); err != nil {
		t.Fatalf("render html: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected html output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("Insights")) {
		t.Fatalf("expected insights section in html output")
	}
}
