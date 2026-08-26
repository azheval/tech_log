package compare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"techlog-stat/internal/report/overview"
	"techlog-stat/internal/stats"
)

func TestLoadAndCompare(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	current := filepath.Join(dir, "current.json")
	writeOverview(t, baseline, overview.OverviewResult{Totals: overview.Aggregate{Count: 1, Duration: stats.Summary{Count: 1, Sum: 100}}})
	writeOverview(t, current, overview.OverviewResult{Totals: overview.Aggregate{Count: 1, Duration: stats.Summary{Count: 1, Sum: 120}}})

	result, err := LoadAndCompare(baseline, current, Options{RegressionPercent: 10, ImprovementPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.Classification != Regressed || result.Totals.AbsoluteDelta != 20 {
		t.Fatalf("result = %+v", result.Totals)
	}
}

func TestLoadOverviewReturnsContextualErrors(t *testing.T) {
	if _, err := LoadOverview("missing.json"); err == nil {
		t.Fatal("missing file accepted")
	}
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverview(path); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func writeOverview(t *testing.T, path string, report overview.OverviewResult) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
