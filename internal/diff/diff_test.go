package diff_test

import (
	"testing"

	"github.com/mickamy/xplain/internal/diff"
	"github.com/mickamy/xplain/test"
)

func TestCompareSamplesAndJSON(t *testing.T) {
	base := test.LoadSampleAnalysis(t, "nloop_base.json")
	target := test.LoadSampleAnalysis(t, "nloop_index.json")

	report, err := diff.Compare(base, target, diff.Options{})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if report == nil || len(report.Improvements) == 0 {
		t.Fatalf("expected improvements in diff report")
	}

	jsonOut, err := report.JSON()
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if len(jsonOut) == 0 {
		t.Fatalf("expected json payload")
	}
}
