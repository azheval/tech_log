package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"techlog-stat/internal/report/compare"
)

func TestCompareRenderers(t *testing.T) {
	percent := 20.0
	report := compare.Result{Totals: compare.Change{Key: "total", Classification: compare.Regressed,
		DurationDelta: compare.MetricDelta{Baseline: 100, Current: 120, AbsoluteDelta: 20, PercentDelta: &percent},
		CountDelta:    compare.MetricDelta{Baseline: 1, Current: 2, AbsoluteDelta: 1, PercentDelta: &percent},
		P95Delta:      compare.MetricDelta{Baseline: 50, Current: 70, AbsoluteDelta: 20, PercentDelta: &percent},
		Reasons:       []compare.Reason{{Metric: "p95", Classification: compare.Regressed}}},
		EventTypes:      []compare.Change{{Key: "SDBL", Classification: compare.Improved, DurationDelta: compare.MetricDelta{AbsoluteDelta: -10}}},
		SCALLByCall:     []compare.Change{{Key: "1:I1:N1:M", Classification: compare.Regressed}},
		WebRequests:     []compare.Change{{Key: "GET /items", Classification: compare.Regressed}},
		SessionByEvent:  []compare.Change{{Key: "SESN", Classification: compare.Improved}},
		PROCByProcess:   []compare.Change{{Key: "rphost", Classification: compare.Regressed}},
		SCOMByOperation: []compare.Change{{Key: "new ServerProcessData", Classification: compare.Regressed}},
		Licenses:        []compare.Change{{Key: "getLicense", Classification: compare.Regressed}},
		ErrorGroups:     []compare.Change{{Key: "error-signature", Classification: compare.Unchanged, Reasons: []compare.Reason{{Metric: "count", Classification: compare.Regressed}}}},
		FileDBByFunc:    []compare.Change{{Key: "Read", Classification: compare.Regressed}},
	}
	text := RenderCompareText(report)
	if !bytes.HasPrefix(text, []byte{0xEF, 0xBB, 0xBF}) || !strings.Contains(string(text), "Totals: regressed") || !strings.Contains(string(text), "File DB by Func") {
		t.Fatalf("text = %q", text)
	}
	csvData, err := RenderCompareCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(csvData, []byte{0xEF, 0xBB, 0xBF})))
	rows, err := reader.ReadAll()
	if err != nil || len(rows) != 11 || rows[1][0] != "Totals" || rows[2][1] != "SDBL" || !containsCSVSection(rows, "SCALL calls") || !containsCSVSection(rows, "File DB by Func") {
		t.Fatalf("csv rows=%v err=%v", rows, err)
	}
	jsonData, err := RenderCompareJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded compare.Result
	if err := json.Unmarshal(jsonData, &decoded); err != nil || decoded.Totals.Classification != compare.Regressed || len(decoded.FileDBByFunc) != 1 || len(decoded.ErrorGroups) != 1 {
		t.Fatalf("json=%s err=%v", jsonData, err)
	}
	htmlData, err := RenderCompareHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	textHTML := string(htmlData)
	if !strings.Contains(textHTML, "<!doctype html>") || !strings.Contains(textHTML, "SDBL") || !strings.Contains(textHTML, "SCALL calls") || !strings.Contains(textHTML, "File DB by Func") || strings.Contains(textHTML, "http://") || strings.Contains(textHTML, "https://") {
		t.Fatalf("html is not autonomous: %s", textHTML)
	}
}

func containsCSVSection(rows [][]string, want string) bool {
	for _, row := range rows {
		if len(row) > 0 && row[0] == want {
			return true
		}
	}
	return false
}
