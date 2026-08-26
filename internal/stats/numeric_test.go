package stats

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestNumericStatsFinalize(t *testing.T) {
	var stats NumericStats
	for _, value := range []float64{5, 1, 9, 3, 7} {
		if !stats.Add(value) {
			t.Fatalf("Add(%v) rejected", value)
		}
	}

	got := stats.Finalize()
	want := Summary{Count: 5, Sum: 25, Min: 1, Max: 9, Mean: 5, P50: 5, P90: 8.2, P95: 8.6, P99: 8.92}
	assertSummary(t, got, want)
	if gotAgain := stats.Finalize(); gotAgain != got {
		t.Fatalf("Finalize without new values = %#v, want cached %#v", gotAgain, got)
	}
}

func TestNumericStatsUpdatesFinalizedValue(t *testing.T) {
	var stats NumericStats
	stats.Add(10)
	_ = stats.Finalize()
	stats.Add(30)

	got := stats.Finalize()
	want := Summary{Count: 2, Sum: 40, Min: 10, Max: 30, Mean: 20, P50: 20, P90: 28, P95: 29, P99: 29.8}
	assertSummary(t, got, want)
}

func TestNumericStatsEmptyAndNaN(t *testing.T) {
	var stats NumericStats
	if stats.Add(math.NaN()) {
		t.Fatal("Add(NaN) accepted NaN")
	}
	if stats.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", stats.Count())
	}

	got := stats.Finalize()
	if got.Count != 0 || got.Sum != 0 {
		t.Fatalf("empty summary = %#v", got)
	}
	for _, value := range []float64{got.Min, got.Max, got.Mean, got.P50, got.P90, got.P95, got.P99} {
		if !math.IsNaN(value) {
			t.Fatalf("empty statistic = %v, want NaN", value)
		}
	}
}

func TestSummaryJSONUsesNullForUnavailableMeasurements(t *testing.T) {
	data, err := json.Marshal(Summary{Sum: math.Inf(1), Min: math.NaN()})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, fragment := range []string{`"Count":0`, `"Sum":null`, `"Min":null`, `"Mean":0`} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("summary JSON %s does not contain %s", text, fragment)
		}
	}
}

func assertSummary(t *testing.T, got, want Summary) {
	t.Helper()
	if got.Count != want.Count {
		t.Errorf("Count = %d, want %d", got.Count, want.Count)
	}
	for _, pair := range []struct {
		name string
		got  float64
		want float64
	}{
		{"Sum", got.Sum, want.Sum}, {"Min", got.Min, want.Min}, {"Max", got.Max, want.Max},
		{"Mean", got.Mean, want.Mean}, {"P50", got.P50, want.P50}, {"P90", got.P90, want.P90},
		{"P95", got.P95, want.P95}, {"P99", got.P99, want.P99},
	} {
		if math.Abs(pair.got-pair.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", pair.name, pair.got, pair.want)
		}
	}
}
