package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"strconv"
	"strings"

	"techlog-stat/internal/report/compare"
)

// RenderCompareText renders a readable standalone comparison summary.
func RenderCompareText(report compare.Result) []byte {
	var buffer bytes.Buffer
	fmt.Fprintln(&buffer, "Technological Log Overview Comparison")
	writeCompareTextChange(&buffer, "Totals", report.Totals)
	for _, section := range compareSections(report) {
		fmt.Fprintf(&buffer, "\n%s:\n", section.Name)
		for _, change := range section.Changes {
			writeCompareTextChange(&buffer, change.Key, change)
		}
	}
	return withUTF8BOM(buffer.Bytes())
}

func writeCompareTextChange(buffer *bytes.Buffer, label string, change compare.Change) {
	fmt.Fprintf(buffer, "%s: %s total=%s us count=%s p95=%s", label, change.Classification, formatDelta(change.DurationDelta), formatDelta(change.CountDelta), formatDelta(change.P95Delta))
	if len(change.Reasons) > 0 {
		fmt.Fprintf(buffer, " reasons=%s", joinReasons(change.Reasons))
	}
	buffer.WriteByte('\n')
}

// RenderCompareCSV writes all comparison sections into one spreadsheet-friendly CSV.
func RenderCompareCSV(report compare.Result) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"section", "key", "classification", "baseline_total_micros", "current_total_micros", "total_delta_micros", "total_delta_percent", "baseline_count", "current_count", "count_delta", "count_delta_percent", "baseline_p95_micros", "current_p95_micros", "p95_delta_micros", "p95_delta_percent", "reasons"}); err != nil {
		return nil, err
	}
	for _, section := range compareSections(report) {
		for _, change := range section.Changes {
			record := []string{section.Name, change.Key, string(change.Classification),
				floatText(change.DurationDelta.Baseline), floatText(change.DurationDelta.Current), floatText(change.DurationDelta.AbsoluteDelta), percentText(change.DurationDelta.PercentDelta),
				floatText(change.CountDelta.Baseline), floatText(change.CountDelta.Current), floatText(change.CountDelta.AbsoluteDelta), percentText(change.CountDelta.PercentDelta),
				floatText(change.P95Delta.Baseline), floatText(change.P95Delta.Current), floatText(change.P95Delta.AbsoluteDelta), percentText(change.P95Delta.PercentDelta), joinReasons(change.Reasons)}
			if err := writer.Write(record); err != nil {
				return nil, err
			}
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return withUTF8BOM(buffer.Bytes()), nil
}

// RenderCompareJSON serializes the full comparison result.
func RenderCompareJSON(report compare.Result) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// RenderCompareHTML renders a self-contained comparison dashboard with no
// external JavaScript, stylesheets, fonts, or network requests.
func RenderCompareHTML(report compare.Result) ([]byte, error) {
	var buffer bytes.Buffer
	if err := compareHTMLTemplate.Execute(&buffer, compareHTMLData{Sections: compareSections(report)}); err != nil {
		return nil, fmt.Errorf("render compare HTML: %w", err)
	}
	return buffer.Bytes(), nil
}

type compareSection struct {
	Name    string
	Changes []compare.Change
}
type compareHTMLData struct{ Sections []compareSection }

func compareSections(report compare.Result) []compareSection {
	return []compareSection{
		{Name: "Totals", Changes: []compare.Change{report.Totals}},
		{Name: "Event types", Changes: report.EventTypes}, {Name: "Users", Changes: report.Users},
		{Name: "Databases", Changes: report.Databases}, {Name: "Processes", Changes: report.Processes}, {Name: "SQL fingerprints", Changes: report.SQLFingerprints},
		{Name: "SCALL calls", Changes: report.SCALLByCall}, {Name: "Web requests", Changes: report.WebRequests},
		{Name: "Session events", Changes: report.SessionByEvent}, {Name: "PROC by process", Changes: report.PROCByProcess},
		{Name: "SCOM by operation", Changes: report.SCOMByOperation}, {Name: "Licenses", Changes: report.Licenses},
		{Name: "Error context groups", Changes: report.ErrorGroups}, {Name: "File DB by Func", Changes: report.FileDBByFunc},
	}
}

func formatDelta(delta compare.MetricDelta) string {
	text := floatText(delta.AbsoluteDelta)
	if delta.PercentDelta != nil {
		text += " (" + percentText(delta.PercentDelta) + ")"
	}
	return text
}

func floatText(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func percentText(value *float64) string {
	if value == nil {
		return "—"
	}
	return floatText(*value) + "%"
}
func joinReasons(reasons []compare.Reason) string {
	values := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		values = append(values, reason.Metric+":"+string(reason.Classification))
	}
	return strings.Join(values, "; ")
}

var compareHTMLTemplate = htmltemplate.Must(htmltemplate.New("compare").Funcs(htmltemplate.FuncMap{
	"delta": formatDelta, "reasons": joinReasons,
}).Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Techlog overview comparison</title><style>
body{margin:0;background:#f5f7fb;color:#172033;font:14px system-ui,-apple-system,"Segoe UI",sans-serif}main{max-width:1440px;margin:auto;padding:28px}h1{margin:0 0 22px}h2{font-size:18px;margin:0 0 12px}.card{background:#fff;border:1px solid #e1e7f0;border-radius:10px;padding:18px;margin:16px 0;overflow:auto}table{border-collapse:collapse;width:100%;min-width:860px}th,td{padding:9px 10px;border-bottom:1px solid #edf0f4;text-align:left;vertical-align:top}th{background:#f7f9fc;color:#52627b}.new,.regressed{color:#a52d2d;font-weight:600}.improved{color:#177443;font-weight:600}.removed{color:#8a5b10;font-weight:600}.unchanged{color:#65728a}.muted{color:#65728a}
</style></head><body><main><h1>Technological Log Overview Comparison</h1>{{range .Sections}}<section class="card"><h2>{{.Name}}</h2>{{if .Changes}}<table><thead><tr><th>Key</th><th>Classification</th><th>Total Δ, µs</th><th>Count Δ</th><th>P95 Δ, µs</th><th>Reasons</th></tr></thead><tbody>{{range .Changes}}<tr><td>{{.Key}}</td><td class="{{.Classification}}">{{.Classification}}</td><td>{{delta .DurationDelta}}</td><td>{{delta .CountDelta}}</td><td>{{delta .P95Delta}}</td><td>{{reasons .Reasons}}</td></tr>{{end}}</tbody></table>{{else}}<span class="muted">No rows.</span>{{end}}</section>{{end}}</main></body></html>`))
