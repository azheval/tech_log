package output

import (
	"bytes"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/model"
)

// RenderContextHTML renders a self-contained dashboard for an aggregate context report.
func RenderContextHTML(report model.ContextReport) ([]byte, error) {
	rows := make([][]string, 0, len(report.Rows))
	for _, row := range report.Rows {
		rows = append(rows, []string{strconv.Itoa(row.Rank), row.Context, row.ShortContext, number(row.TotalDurationMS), percent(row.TimePct), strconv.FormatInt(row.Count, 10), percent(row.CountPct), number(row.AverageMS)})
	}
	return renderHTML(pageData{
		Title: reportTitle(report.Meta.Report), Meta: report.Meta, Totals: &report.Totals,
		TableTitle: "Contexts with the longest duration",
		Headers:    []string{"#", "Context", "Short Context", "Total, ms", "Time Share", "Events", "Event Share", "Average, ms"},
		Rows:       rows, Errors: report.Errors, Matches: report.Matches,
	})
}

// RenderErrorHTML renders a self-contained dashboard for an aggregate error report.
func RenderErrorHTML(report model.ErrorReport) ([]byte, error) {
	rows := make([][]string, 0, len(report.Rows))
	for _, row := range report.Rows {
		rows = append(rows, []string{strconv.Itoa(row.Rank), row.Event, row.Description, row.ShortDescription, number(row.TotalDurationMS), percent(row.TimePct), strconv.FormatInt(row.Count, 10), percent(row.CountPct), number(row.AverageMS)})
	}
	return renderHTML(pageData{
		Title: reportTitle(report.Meta.Report), Meta: report.Meta, Totals: &report.Totals,
		TableTitle: "Errors", Headers: []string{"#", "Event", "Description", "Short Description", "Total, ms", "Time Share", "Count", "Event Share", "Average, ms"},
		Rows: rows, Errors: report.Errors, Matches: report.Matches,
	})
}

// RenderRawContextHTML renders a self-contained table of raw context events.
func RenderRawContextHTML(report model.RawContextReport) ([]byte, error) {
	rows := make([][]string, 0)
	for _, day := range report.Days {
		for _, hour := range day.Hours {
			for _, event := range hour.Events {
				rows = append(rows, []string{event.Timestamp.Format("2006-01-02 15:04:05.000000"), event.Event, event.File, strconv.FormatInt(event.DurationMicros, 10), number(event.DurationMS), event.Context, event.ShortContext})
			}
		}
	}
	return renderHTML(pageData{
		Title: reportTitle(report.Meta.Report) + " — source events", Meta: report.Meta,
		TableTitle: "Longest events by hour", Headers: []string{"Time", "Event", "File", "Duration, μs", "Duration, ms", "Context", "Short Context"},
		Rows: rows, Errors: report.Errors, Matches: report.Matches,
	})
}

// RenderRawErrorHTML renders a self-contained table of raw error events.
func RenderRawErrorHTML(report model.RawErrorReport) ([]byte, error) {
	rows := make([][]string, 0)
	for _, day := range report.Days {
		for _, hour := range day.Hours {
			for _, event := range hour.Events {
				rows = append(rows, []string{event.Timestamp.Format("2006-01-02 15:04:05.000000"), event.Event, event.File, strconv.FormatInt(event.DurationMicros, 10), number(event.DurationMS), event.Description, event.ShortDescription})
			}
		}
	}
	return renderHTML(pageData{
		Title: reportTitle(report.Meta.Report) + " — source events", Meta: report.Meta,
		TableTitle: "Errors by hour", Headers: []string{"Time", "Event", "File", "Duration, μs", "Duration, ms", "Description", "Short Description"},
		Rows: rows, Errors: report.Errors, Matches: report.Matches,
	})
}

type pageData struct {
	Title      string
	Meta       model.RunMeta
	Totals     *model.Totals
	TableTitle string
	Headers    []string
	Rows       [][]string
	Errors     []string
	Matches    []string
}

func renderHTML(data pageData) ([]byte, error) {
	var out bytes.Buffer
	if err := htmlPage.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render HTML report: %w", err)
	}
	return out.Bytes(), nil
}

func number(value float64) string  { return formatHumanFloat(value) }
func percent(value float64) string { return formatPercent(value) }

func formatHTMLTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Format("2006-01-02 15:04:05 MST")
}

func joinFormats(formats []string) string {
	if len(formats) == 0 {
		return "—"
	}
	return strings.Join(formats, ", ")
}

var htmlPage = template.Must(template.New("report").Funcs(template.FuncMap{
	"time":    formatHTMLTime,
	"formats": joinFormats,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="generator" content="techlog-stat">
  <title>{{.Title}}</title>
  <style>
    :root { color-scheme: light; font-family: Inter, Segoe UI, Arial, sans-serif; color:#172033; background:#f5f7fb; }
    * { box-sizing:border-box; } body { margin:0; } main { max-width:1440px; margin:auto; padding:28px; }
    header { display:flex; align-items:end; justify-content:space-between; gap:20px; margin-bottom:22px; } h1 { margin:0; font-size:25px; } h2 { font-size:17px; margin:0 0 14px; }
    .muted { color:#65728a; font-size:13px; } .panel { background:#fff; border:1px solid #e3e8f2; border-radius:10px; padding:18px; margin-bottom:18px; box-shadow:0 1px 2px #18233a08; }
    .metrics { display:grid; grid-template-columns:repeat(3,minmax(150px,1fr)); gap:12px; } .metric { background:#f8faff; border-radius:8px; padding:13px; } .metric b { display:block; color:#1b6ac9; font-size:22px; margin-top:5px; }
    .meta { display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr)); gap:8px 20px; font-size:13px; } .meta dt { color:#65728a; } .meta dd { margin:2px 0 0; overflow-wrap:anywhere; }
    .table-wrap { overflow:auto; border:1px solid #e9edf5; border-radius:8px; } table { width:100%; min-width:860px; border-collapse:collapse; font-size:13px; } th { text-align:left; color:#52627b; background:#f7f9fc; position:sticky; top:0; } th,td { padding:10px 11px; border-bottom:1px solid #edf0f5; vertical-align:top; } td { white-space:pre-wrap; overflow-wrap:anywhere; } tr:last-child td { border-bottom:0; }
    .notice { padding:10px 12px; border-radius:7px; font-size:13px; margin-top:10px; } .warn { background:#fff7e8; color:#805710; } .info { background:#eef6ff; color:#245d9c; } ul { margin:7px 0 0; padding-left:20px; }
    @media (max-width:640px) { main { padding:16px; } header { display:block; } header .muted { margin-top:7px; } .metrics { grid-template-columns:1fr; } }
  </style>
</head>
<body><main>
  <header><div><h1>{{.Title}}</h1><div class="muted">Standalone 1C technology log report</div></div><div class="muted">Created: {{time .Meta.FinishedAt}}</div></header>
  {{if .Totals}}<section class="panel"><div class="metrics"><div class="metric"><span class="muted">Events</span><b>{{.Totals.EventCount}}</b></div><div class="metric"><span class="muted">Total Duration</span><b>{{printf "%.3f" .Totals.DurationMS}} ms</b></div><div class="metric"><span class="muted">Average Duration</span><b>{{printf "%.3f" .Totals.AverageDuration}} ms</b></div></div></section>{{end}}
  <section class="panel"><h2>Processing Parameters</h2><dl class="meta"><div><dt>Input Directory</dt><dd>{{.Meta.InputRoot}}</dd></div><div><dt>File Pattern</dt><dd>{{.Meta.Glob}}</dd></div><div><dt>Matched / Processed / Failed</dt><dd>{{.Meta.FilesMatched}} / {{.Meta.FilesProcessed}} / {{.Meta.FilesFailed}}</dd></div><div><dt>Read</dt><dd>{{.Meta.BytesRead}} bytes</dd></div><div><dt>Workers</dt><dd>{{.Meta.Workers}}</dd></div><div><dt>Top N</dt><dd>{{.Meta.TopN}}</dd></div><div><dt>Elapsed Time</dt><dd>{{.Meta.Duration}}</dd></div><div><dt>Formats</dt><dd>{{formats .Meta.Formats}}</dd></div></dl></section>
  <section class="panel"><h2>{{.TableTitle}}</h2>{{if .Rows}}<div class="table-wrap"><table><thead><tr>{{range .Headers}}<th>{{.}}</th>{{end}}</tr></thead><tbody>{{range .Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody></table></div>{{else}}<p class="muted">No data matches the report criteria.</p>{{end}}</section>
  {{if .Matches}}<section class="panel notice info"><strong>Processed Files</strong><ul>{{range .Matches}}<li>{{.}}</li>{{end}}</ul></section>{{end}}
  {{if .Errors}}<section class="panel notice warn"><strong>Processing Warnings and Errors</strong><ul>{{range .Errors}}<li>{{.}}</li>{{end}}</ul></section>{{end}}
</main></body></html>`))
