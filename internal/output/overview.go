package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strconv"
	"strings"

	"techlog-stat/internal/analysis/errorcontext"
	"techlog-stat/internal/analysis/filedbstats"
	"techlog-stat/internal/analysis/licensestats"
	"techlog-stat/internal/analysis/lockstats"
	"techlog-stat/internal/analysis/scallstats"
	"techlog-stat/internal/analysis/sqlstats"
	"techlog-stat/internal/analysis/trace"
	"techlog-stat/internal/analysis/webstats"
	"techlog-stat/internal/report/overview"
)

const (
	maxOverviewSQLRows  = 100
	maxOverviewTraces   = 100
	maxTraceSpans       = 100
	maxOverviewLockRows = 100
	maxRawPreviewRunes  = 4_000
)

// RenderOverviewHTML creates a portable dashboard. The chart uses the
// browser canvas API and has no external CDN or runtime dependency.
func RenderOverviewHTML(report overview.OverviewResult) ([]byte, error) {
	buckets, err := json.Marshal(report.Buckets)
	if err != nil {
		return nil, fmt.Errorf("encode overview buckets: %w", err)
	}
	sqlRows, sqlOmitted := boundedSQLRows(report.SQLRows, maxOverviewSQLRows)
	traces, tracesOmitted := boundedTraces(report.Traces, maxOverviewTraces, maxTraceSpans)
	scallCalls, scallOmitted := boundedRows(report.SCALL.ByCall, maxOverviewSQLRows)
	webRequests, webOmitted := boundedRows(report.Web.Requests, maxOverviewSQLRows)
	licenseRows, licenseOmitted := boundedRows(report.Licenses.Licenses, maxOverviewSQLRows)
	errorGroups, errorOmitted := boundedRows(report.ErrorContext.Groups, maxOverviewSQLRows)
	fileDBRows, fileDBOmitted := boundedRows(report.FileDB.ByFunc, maxOverviewSQLRows)
	data := overviewPageData{Report: report, BucketJSON: template.JS(string(buckets)), SQLRows: sqlRows, SQLRowsOmitted: sqlOmitted, Traces: traces, TracesOmitted: tracesOmitted, Locks: boundedLocks(report.Locks, maxOverviewLockRows), SCALLCalls: scallCalls, SCALLOmitted: scallOmitted, WebRequests: webRequests, WebOmitted: webOmitted, LicenseRows: licenseRows, LicenseOmitted: licenseOmitted, ErrorGroups: errorGroups, ErrorOmitted: errorOmitted, FileDBRows: fileDBRows, FileDBOmitted: fileDBOmitted}
	var out bytes.Buffer
	if err := overviewHTML.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render overview HTML: %w", err)
	}
	return out.Bytes(), nil
}

type overviewPageData struct {
	Report         overview.OverviewResult
	BucketJSON     template.JS
	SQLRows        []sqlstats.Row
	SQLRowsOmitted int
	Traces         []overviewTraceView
	TracesOmitted  int
	Locks          overviewLocksView
	SCALLCalls     []scallstats.Call
	SCALLOmitted   int
	WebRequests    []webstats.RequestRow
	WebOmitted     int
	LicenseRows    []licensestats.LicenseRow
	LicenseOmitted int
	ErrorGroups    []errorcontext.Group
	ErrorOmitted   int
	FileDBRows     []filedbstats.Aggregate
	FileDBOmitted  int
}

type overviewLocksView struct {
	Quality                                                                                                       lockstats.Quality
	ByEvent, ByContext, ByTable, ByRegion                                                                         []lockstats.Aggregate
	Conflicts                                                                                                     []lockstats.Conflict
	Relations                                                                                                     []lockstats.Relation
	Samples                                                                                                       []lockstats.Sample
	EventOmitted, ContextOmitted, TableOmitted, RegionOmitted, ConflictsOmitted, RelationsOmitted, SamplesOmitted int
}

type overviewTraceView struct {
	Trace        trace.Trace
	Spans        []trace.Span
	SpansOmitted int
}

func boundedSQLRows(rows []sqlstats.Row, limit int) ([]sqlstats.Row, int) {
	if len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}

func boundedRows[T any](rows []T, limit int) ([]T, int) {
	if len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}

func boundedTraces(items []trace.Trace, traceLimit, spanLimit int) ([]overviewTraceView, int) {
	omittedTraces := 0
	if len(items) > traceLimit {
		omittedTraces = len(items) - traceLimit
		items = items[:traceLimit]
	}
	views := make([]overviewTraceView, 0, len(items))
	for _, item := range items {
		spans := item.Spans
		omittedSpans := 0
		if len(spans) > spanLimit {
			omittedSpans = len(spans) - spanLimit
			spans = spans[:spanLimit]
		}
		views = append(views, overviewTraceView{Trace: item, Spans: spans, SpansOmitted: omittedSpans})
	}
	return views, omittedTraces
}

func boundedLocks(locks lockstats.Result, limit int) overviewLocksView {
	events, eventOmitted := boundedLockAggregates(locks.ByEvent, limit)
	contexts, contextOmitted := boundedLockAggregates(locks.ByContext, limit)
	tables, tableOmitted := boundedLockAggregates(locks.ByTable, limit)
	regions, regionOmitted := boundedLockAggregates(locks.ByRegion, limit)
	conflicts, conflictsOmitted := boundedLockConflicts(locks.TopConflicts, limit)
	relations, relationsOmitted := boundedLockRelations(locks.Relations, limit)
	samples, samplesOmitted := boundedLockSamples(locks.Samples, limit)
	return overviewLocksView{Quality: locks.Quality, ByEvent: events, ByContext: contexts, ByTable: tables, ByRegion: regions, Conflicts: conflicts, Relations: relations, Samples: samples, EventOmitted: eventOmitted, ContextOmitted: contextOmitted, TableOmitted: tableOmitted, RegionOmitted: regionOmitted, ConflictsOmitted: conflictsOmitted, RelationsOmitted: relationsOmitted, SamplesOmitted: samplesOmitted}
}

func boundedLockAggregates(rows []lockstats.Aggregate, limit int) ([]lockstats.Aggregate, int) {
	if len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}
func boundedLockConflicts(rows []lockstats.Conflict, limit int) ([]lockstats.Conflict, int) {
	if len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}
func boundedLockRelations(rows []lockstats.Relation, limit int) ([]lockstats.Relation, int) {
	if len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}
func boundedLockSamples(rows []lockstats.Sample, limit int) ([]lockstats.Sample, int) {
	if len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}

func overviewNumber(value float64) string { return formatHumanFloat(value) }
func overviewTime(value interface{ Format(string) string }) string {
	return value.Format("2006-01-02 15:04")
}
func overviewRank(index int) string { return strconv.Itoa(index + 1) }
func overviewPreview(value string) string {
	runes := []rune(value)
	if len(runes) <= maxRawPreviewRunes {
		return value
	}
	return string(runes[:maxRawPreviewRunes]) + " …"
}

var overviewHTML = template.Must(template.New("overview").Funcs(template.FuncMap{
	"number":  overviewNumber,
	"time":    overviewTime,
	"rank":    overviewRank,
	"preview": overviewPreview,
	"join":    strings.Join,
	"dict":    dict,
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Technology Log Overview</title>
<style>
:root{font-family:Segoe UI,Arial,sans-serif;color:#172033;background:#f5f7fb}*{box-sizing:border-box}body{margin:0}main{max-width:1440px;margin:auto;padding:24px}.panel{background:#fff;border:1px solid #e3e8f2;border-radius:10px;padding:18px;margin:16px 0}.metrics{display:grid;grid-template-columns:repeat(4,minmax(140px,1fr));gap:12px}.metric{background:#f8faff;border-radius:8px;padding:12px}.metric b{display:block;font-size:22px;color:#1b6ac9;margin-top:4px}.muted{color:#65728a;font-size:13px}.chart{width:100%;height:280px}.table{overflow:auto}table{border-collapse:collapse;width:100%;font-size:13px;min-width:620px}th,td{text-align:left;padding:9px;border-bottom:1px solid #edf0f5;vertical-align:top}th{background:#f7f9fc;color:#52627b;cursor:pointer}th:focus{outline:2px solid #1b6ac9;outline-offset:-2px}td{overflow-wrap:anywhere}.grids{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}.trace{border:1px solid #e9edf5;border-radius:7px;padding:10px;margin:10px 0}.trace summary{cursor:pointer;font-weight:600}.waterfall{margin:12px 0 0 10px;border-left:3px solid #9fc5f8}.span{padding:7px 10px;margin:0 0 2px 12px;background:#f8faff;border-radius:5px}.span pre{max-height:220px;overflow:auto;white-space:pre-wrap;margin:7px 0 0;font-size:12px}.nav{display:flex;flex-wrap:wrap;gap:8px;margin:14px 0}.nav a{padding:7px 10px;border:1px solid #cdd8ea;border-radius:6px;color:#1b579e;background:#fff;text-decoration:none}.nav a[aria-current="page"]{background:#1b6ac9;color:#fff;border-color:#1b6ac9}.controls{display:flex;align-items:end;flex-wrap:wrap;gap:14px}.controls label{display:grid;gap:5px;font-size:13px}.controls input[type="search"]{min-width:270px;padding:8px;border:1px solid #b8c5d9;border-radius:6px}.controls .check{display:flex;align-items:center;gap:6px}.js-hidden{display:none!important}.sort-mark{margin-left:4px;font-size:11px}@media(max-width:800px){main{padding:14px}.metrics,.grids{grid-template-columns:1fr}.controls input[type="search"]{min-width:0;width:100%}}
</style><script>
document.addEventListener("DOMContentLoaded",function(){
  var search=document.getElementById("global-search"),unknown=document.getElementById("hide-unknown"),status=document.getElementById("filter-status"),links=[].slice.call(document.querySelectorAll("[data-tab]")),sections=[].slice.call(document.querySelectorAll("main>section:not(.controls)"));
  function sectionTab(section){var text=section.textContent||"";if(section.getAttribute("data-section")==="events")return"events";if(text.indexOf("Top SQL / SDBL")>=0)return"sql";if(/Locks|conflicts|wait relations|Contexts|Tables|Regions/.test(text))return"locks";if(/tracing|Call Traces/.test(text))return"traces";if(/Users|Databases|Processes/.test(text))return"dimensions";return"overview"}
  function readState(){var saved={};try{saved=JSON.parse(localStorage.getItem("techlog-overview-ui")||"{}")||{}}catch(e){}var hash=location.hash.replace(/^#/,"");if(hash&&hash.indexOf("tab=")!==0)saved.tab=hash;else if(hash.indexOf("tab=")===0)saved.tab=decodeURIComponent(hash.slice(4));return saved}
  var state=readState(),active=state.tab||"overview";search.value=state.query||"";unknown.checked=!!state.hideUnknown;
  function persist(){try{localStorage.setItem("techlog-overview-ui",JSON.stringify({tab:active,query:search.value,hideUnknown:unknown.checked}))}catch(e){}}
  function applyFilter(){var query=search.value.toLocaleLowerCase(),shown=0;document.querySelectorAll("table tbody tr").forEach(function(row){var cells=row.cells,first=cells.length?cells[0].textContent.trim():"",match=(!query||row.textContent.toLocaleLowerCase().indexOf(query)>=0)&&(!unknown.checked||first!=="(unknown)");row.classList.toggle("js-hidden",!match);if(match)shown++});status.textContent=query||unknown.checked?"Rows shown: "+shown:"";persist()}
  function activate(tab){active=tab;sections.forEach(function(section){section.classList.toggle("js-hidden",sectionTab(section)!==tab)});links.forEach(function(link){link.setAttribute("aria-current",link.getAttribute("data-tab")===tab?"page":"false")});if(history.replaceState)history.replaceState(null,"","#tab="+encodeURIComponent(tab));persist()}
  links.forEach(function(link){link.addEventListener("click",function(event){event.preventDefault();activate(link.getAttribute("data-tab"))})});search.addEventListener("input",applyFilter);unknown.addEventListener("change",applyFilter);
  document.querySelectorAll("table").forEach(function(table){var headers=table.querySelectorAll("thead th"),body=table.querySelector("tbody");if(!body)return;headers.forEach(function(header,column){header.tabIndex=0;header.setAttribute("aria-label","Sort by "+header.textContent.trim());function sort(){var descending=header.getAttribute("aria-sort")!=="descending",rows=[].slice.call(body.rows);headers.forEach(function(item){item.removeAttribute("aria-sort");var mark=item.querySelector(".sort-mark");if(mark)mark.remove()});rows.sort(function(left,right){var a=(left.cells[column]||{}).textContent||"",b=(right.cells[column]||{}).textContent||"",an=Number(a.replace(/[^0-9,.-]/g,"").replace(",",".")),bn=Number(b.replace(/[^0-9,.-]/g,"").replace(",",".")),result=Number.isFinite(an)&&Number.isFinite(bn)&&a.match(/[0-9]/)&&b.match(/[0-9]/)?an-bn:a.localeCompare(b,"en");return descending?-result:result});rows.forEach(function(row){body.appendChild(row)});header.setAttribute("aria-sort",descending?"descending":"ascending");var mark=document.createElement("span");mark.className="sort-mark";mark.setAttribute("aria-hidden","true");mark.textContent=descending?"▼":"▲";header.appendChild(mark);applyFilter()}header.addEventListener("click",sort);header.addEventListener("keydown",function(event){if(event.key==="Enter"||event.key===" "){event.preventDefault();sort()}})})});
  activate(active);applyFilter();
});
</script></head><body><main>
<h1>1C Technology Log Overview</h1><p class="muted">Standalone report, created: {{.Report.Meta.FinishedAt.Format "2006-01-02 15:04:05 MST"}}</p>
<nav class="nav" aria-label="Report sections"><a href="#overview" data-tab="overview" aria-current="page">Overview</a><a href="#events" data-tab="events">1C Events</a><a href="#sql" data-tab="sql">SQL / SDBL</a><a href="#locks" data-tab="locks">Locks</a><a href="#traces" data-tab="traces">Traces</a><a href="#dimensions" data-tab="dimensions">Dimensions</a></nav>
<section class="panel controls" aria-label="Report filters"><label for="global-search">Search tables<input id="global-search" type="search" autocomplete="off" placeholder="Context, table, SQL, user…"></label><label class="check" for="hide-unknown"><input id="hide-unknown" type="checkbox">Hide (unknown)</label><span id="filter-status" class="muted" aria-live="polite"></span></section>
<section id="overview" data-section="overview" class="panel metrics"><div class="metric"><span class="muted">Events</span><b>{{.Report.Totals.Count}}</b></div><div class="metric"><span class="muted">Total, μs</span><b>{{number .Report.Totals.Duration.Sum}}</b></div><div class="metric"><span class="muted">p95, μs</span><b>{{number .Report.Totals.Duration.P95}}</b></div><div class="metric"><span class="muted">Files: processed / failed</span><b>{{.Report.Meta.FilesProcessed}} / {{.Report.Meta.FilesFailed}}</b></div></section>
<section data-section="overview" class="panel"><h2>Event Duration Over Time</h2><canvas id="timeline" class="chart" aria-label="Timeline chart"></canvas><p id="chart-empty" class="muted" hidden>No data available for the chart.</p></section>
<section data-section="overview" class="panel"><h2>Event Types</h2><div class="table"><table><thead><tr><th>Event</th><th>Count</th><th>Total, μs</th><th>Average, μs</th><th>p95, μs</th><th>Max., μs</th></tr></thead><tbody>{{range .Report.EventTypes}}<tr><td>{{.Event}}</td><td>{{.Stats.Count}}</td><td>{{number .Stats.Duration.Sum}}</td><td>{{number .Stats.Duration.Mean}}</td><td>{{number .Stats.Duration.P95}}</td><td>{{number .Stats.Duration.Max}}</td></tr>{{end}}</tbody></table></div></section>
{{if .SQLRows}}<section class="panel"><h2>Top SQL / SDBL</h2><div class="table"><table><thead><tr><th>Type</th><th>Count</th><th>Total, μs</th><th>p95, μs</th><th>Normalized Query</th><th>Contexts</th></tr></thead><tbody>{{range .SQLRows}}<tr><td>{{.EventType}}</td><td>{{.Count}}</td><td>{{.TotalDurationMicros}}</td><td>{{.P95DurationMicros}}</td><td>{{.NormalizedQuery}}</td><td>{{range .Contexts}}{{.}}<br>{{end}}</td></tr>{{end}}</tbody></table></div>{{if .SQLRowsOmitted}}<p class="muted">Showing the first {{len .SQLRows}} rows; {{.SQLRowsOmitted}} more are available in sql.csv and run.json.</p>{{end}}</section>{{end}}
{{if .Report.SCALL.Quality.CallEvents}}<section data-section="events" class="panel"><h2>SCALL: Server Calls</h2><p class="muted">Events: {{.Report.SCALL.Quality.CallEvents}} · groups: {{len .Report.SCALL.ByCall}} · slow samples: {{len .Report.SCALL.SlowSamples}}</p><div class="table"><table><thead><tr><th>Interface</th><th>IName</th><th>Method</th><th>Count</th><th>Total, μs</th><th>p95, μs</th></tr></thead><tbody>{{range .SCALLCalls}}<tr><td>{{.Interface}}</td><td>{{.IName}}</td><td>{{.Method}}</td><td>{{.Metrics.Duration.Count}}</td><td>{{number .Metrics.Duration.Sum}}</td><td>{{number .Metrics.Duration.P95}}</td></tr>{{end}}</tbody></table></div>{{if .SCALLOmitted}}<p class="muted">{{.SCALLOmitted}} groups hidden. The full set is available in scall.csv and run.json.</p>{{end}}</section>{{end}}
{{if or .Report.Web.Quality.Requests .Report.Web.Quality.Responses .Report.Web.Quality.CacheEvents}}<section data-section="events" class="panel"><h2>VRSREQUEST / VRSRESPONSE / VRSCACHE</h2><p class="muted">Requests: {{.Report.Web.Quality.Requests}} · responses: {{.Report.Web.Quality.Responses}} · matched: {{.Report.Web.Quality.MatchedResponses}} · unmatched responses: {{.Report.Web.Quality.OrphanResponses}} · cache hit/miss: {{.Report.Web.Quality.CacheHits}} / {{.Report.Web.Quality.CacheMisses}}</p><div class="table"><table><thead><tr><th>Method</th><th>URI</th><th>Status</th><th>Result</th><th>Count</th><th>p95, μs</th><th>Request / response bytes</th></tr></thead><tbody>{{range .WebRequests}}<tr><td>{{.Method}}</td><td>{{.URI}}</td><td>{{.Status}}</td><td>{{.Result}}</td><td>{{.Count}}</td><td>{{.Stats.P95Micros}}</td><td>{{.RequestBytes}} / {{.ResponseBytes}}</td></tr>{{end}}</tbody></table></div>{{if .WebOmitted}}<p class="muted">{{.WebOmitted}} groups hidden. The full set is available in web.csv and run.json.</p>{{end}}</section>{{end}}
{{if .Report.Sessions.Quality.LifecycleEvents}}<section data-section="events" class="panel"><h2>SESN / CONN</h2><div class="metrics"><div class="metric"><span class="muted">Completed / Open</span><b>{{.Report.Sessions.Quality.CompletedSessions}} / {{.Report.Sessions.Quality.OpenSessions}}</b></div><div class="metric"><span class="muted">Peak Concurrent</span><b>{{.Report.Sessions.Peak}}</b></div><div class="metric"><span class="muted">Finishes Without Starts</span><b>{{.Report.Sessions.Quality.UnmatchedFinishes}}</b></div></div><p class="muted">Duration is calculated only from matching start and end timestamps.</p></section>{{end}}
{{if or .Report.ProcessStats.Quality.PROCEvents .Report.ProcessStats.Quality.SCOMEvents}}<section data-section="events" class="panel"><h2>PROC / SCOM</h2><p class="muted">PROC: {{.Report.ProcessStats.Quality.PROCEvents}} · SCOM: {{.Report.ProcessStats.Quality.SCOMEvents}} · explicit process relations: {{len .Report.ProcessStats.ExplicitProcessRelations}}. PROC duration is event duration, not process runtime.</p></section>{{end}}
{{if or .Report.Licenses.Quality.LicenseEvents .Report.Licenses.Quality.HASPEvents}}<section data-section="events" class="panel"><h2>LIC / HASP</h2><p class="muted">LIC: {{.Report.Licenses.Quality.LicenseEvents}} · HASP: {{.Report.Licenses.Quality.HASPEvents}} · failures: {{.Report.Licenses.Quality.Failures}} · expired: {{.Report.Licenses.Quality.Expired}} · wrong type: {{.Report.Licenses.Quality.WrongType}}</p><div class="table"><table><thead><tr><th>Func</th><th>Result</th><th>Process</th><th>User</th><th>Count</th><th>Acquire / Release</th><th>Success / Failure</th></tr></thead><tbody>{{range .LicenseRows}}<tr><td>{{.Func}}</td><td>{{.Result}}</td><td>{{.Process}}</td><td>{{.User}}</td><td>{{.Count}}</td><td>{{.Acquire}} / {{.Release}}</td><td>{{.Success}} / {{.Failure}}</td></tr>{{end}}</tbody></table></div>{{if .LicenseOmitted}}<p class="muted">{{.LicenseOmitted}} groups hidden. The full set is available in licenses.csv and run.json.</p>{{end}}<p class="muted">Paths, hardware identifiers, and other sensitive values are redacted.</p></section>{{end}}
{{if .Report.ErrorContext.Quality.ErrorEvents}}<section data-section="events" class="panel"><h2>EXCPCNTX: Error Context</h2><p class="muted">Errors: {{.Report.ErrorContext.Quality.ErrorEvents}} · contexts: {{.Report.ErrorContext.Quality.ContextEvents}} · matched: {{.Report.ErrorContext.Quality.MatchedContexts}} · unmatched: {{.Report.ErrorContext.Quality.OrphanContexts}} · ambiguous: {{.Report.ErrorContext.Quality.AmbiguousContexts}}</p><div class="table"><table><thead><tr><th>Signature</th><th>Exception</th><th>Module</th><th>Process</th><th>User</th><th>Count</th></tr></thead><tbody>{{range .ErrorGroups}}<tr><td>{{.Signature}}</td><td>{{.Exception}}</td><td>{{.Module}}</td><td>{{.Process}}</td><td>{{.User}}</td><td>{{.Count}}</td></tr>{{end}}</tbody></table></div>{{if .ErrorOmitted}}<p class="muted">{{.ErrorOmitted}} groups hidden. The full set is available in error_contexts.csv and run.json.</p>{{end}}</section>{{end}}
{{if .Report.FileDB.Quality.DBV8DBEngEvents}}<section data-section="events" class="panel"><h2>DBV8DBEng: File Database</h2><p class="muted">Events: {{.Report.FileDB.Quality.DBV8DBEngEvents}} · functions: {{len .Report.FileDB.ByFunc}} · tables: {{len .Report.FileDB.ByTable}} · explicit errors: {{.Report.FileDB.Quality.ExplicitErrorEvents}}</p><div class="table"><table><thead><tr><th>Func</th><th>Count</th><th>Total, μs</th><th>p95, μs</th><th>Rows</th><th>RowsAffected</th></tr></thead><tbody>{{range .FileDBRows}}<tr><td>{{.Key}}</td><td>{{.Count}}</td><td>{{.Duration.TotalMicros}}</td><td>{{.Duration.P95Micros}}</td><td>{{.Rows.Rows.Sum}}</td><td>{{.Rows.RowsAffected.Sum}}</td></tr>{{end}}</tbody></table></div>{{if .FileDBOmitted}}<p class="muted">{{.FileDBOmitted}} functions hidden. The full set is available in filedb.csv and run.json.</p>{{end}}<p class="muted">Absolute file paths are redacted; missing Rows and RowsAffected are not treated as zero.</p></section>{{end}}
{{if .Locks.Quality.LockEvents}}<section class="panel"><h2>Locks — Summary</h2><div class="metrics"><div class="metric"><span class="muted">Lock Events</span><b>{{.Locks.Quality.LockEvents}}</b></div><div class="metric"><span class="muted">Contexts / Tables / Regions</span><b>{{len .Locks.ByContext}} / {{len .Locks.ByTable}} / {{len .Locks.ByRegion}}</b></div><div class="metric"><span class="muted">Incomplete Context / Locks / Regions</span><b>{{.Locks.Quality.MissingContext}} / {{.Locks.Quality.MissingLocks}} / {{.Locks.Quality.MissingRegions}}</b></div><div class="metric"><span class="muted">Explicit Relations</span><b>{{.Locks.Quality.EventsWithExplicitRelation}}</b></div></div></section>
<section class="panel"><h2>Lock Event Types</h2>{{template "lockaggregate" (dict "Rows" .Locks.ByEvent "Omitted" .Locks.EventOmitted "Label" "types")}}</section>
<section class="grids"><section class="panel"><h2>Contexts</h2>{{template "lockaggregate" (dict "Rows" .Locks.ByContext "Omitted" .Locks.ContextOmitted "Label" "contexts")}}</section><section class="panel"><h2>Tables</h2>{{template "lockaggregate" (dict "Rows" .Locks.ByTable "Omitted" .Locks.TableOmitted "Label" "tables")}}</section><section class="panel"><h2>Regions</h2>{{template "lockaggregate" (dict "Rows" .Locks.ByRegion "Omitted" .Locks.RegionOmitted "Label" "regions")}}</section></section>
{{if .Locks.Conflicts}}<section class="panel"><h2>Top Conflicts</h2><div class="table"><table><thead><tr><th>Type</th><th>Context</th><th>Tables</th><th>Regions</th><th>Count</th><th>Total, μs</th><th>p95, μs</th></tr></thead><tbody>{{range .Locks.Conflicts}}<tr><td>{{.EventType}}</td><td>{{.Context}}</td><td>{{join .Tables " · "}}</td><td>{{join .Regions " · "}}</td><td>{{.Stats.Count}}</td><td>{{.Stats.TotalMicros}}</td><td>{{.Stats.P95Micros}}</td></tr>{{end}}</tbody></table></div>{{if .Locks.ConflictsOmitted}}<p class="muted">{{.Locks.ConflictsOmitted}} conflicts hidden. The full set is in locks.csv and run.json.</p>{{end}}</section>{{end}}
{{if .Locks.Relations}}<section class="panel"><h2>Explicit Wait Relations</h2><div class="table"><table><thead><tr><th>Type</th><th>Waiter</th><th>Blocker</th><th>Context</th><th>Source</th></tr></thead><tbody>{{range .Locks.Relations}}<tr><td>{{.EventType}}</td><td>{{.Waiter}}</td><td>{{.Blocker}}</td><td>{{.Context}}</td><td>{{.Source}}</td></tr>{{end}}</tbody></table></div>{{if .Locks.RelationsOmitted}}<p class="muted">{{.Locks.RelationsOmitted}} relations hidden.</p>{{end}}</section>{{end}}
{{if .Locks.Samples}}<section class="panel"><h2>Source Lock Events</h2>{{range .Locks.Samples}}<details class="trace lock-sample"><summary>{{.EventType}} · {{.Timestamp.Format "2006-01-02 15:04:05.000000"}} · {{.DurationMicros}} μs · {{.Context}}</summary><p class="muted">Tables: {{join .Tables " · "}} · Regions: {{join .Regions " · "}} · {{.Source}}</p><pre>{{preview .Raw}}</pre></details>{{end}}{{if .Locks.SamplesOmitted}}<p class="muted">{{.Locks.SamplesOmitted}} source events hidden.</p>{{end}}</section>{{end}}{{end}}
<section class="panel"><h2>Trace Quality</h2><p>Events consumed: {{.Report.TraceQuality.EventsConsumed}} · CALL: {{.Report.TraceQuality.Calls}} · Context: {{.Report.TraceQuality.Contexts}} · correlated: {{.Report.TraceQuality.CorrelatedEvents}} · without trace: {{.Report.TraceQuality.OrphanEvents}} · without Context: {{.Report.TraceQuality.MissingContextTraces}} · open / completed: {{.Report.TraceQuality.RetainedOpenTraces}} / {{.Report.TraceQuality.RetainedCompletedTraces}}</p></section>
{{if .Traces}}<section class="panel"><h2>Call Traces</h2>{{range .Traces}}<details class="trace"><summary>{{.Trace.ID}} · {{.Trace.StartedAt.Format "2006-01-02 15:04:05.000000"}} · {{.Trace.Process}} / thread {{.Trace.OSThread}} · {{len .Spans}} spans</summary><div class="waterfall">{{range .Spans}}<div class="span"><b>{{.Event}}</b> · {{.Timestamp.Format "15:04:05.000000"}} · {{.DurationMicros}} μs{{if .Raw}}<details><summary class="muted">Source Event</summary><pre>{{preview .Raw}}</pre></details>{{end}}</div>{{end}}</div>{{if .SpansOmitted}}<p class="muted">{{.SpansOmitted}} spans hidden.</p>{{end}}</details>{{end}}{{if .TracesOmitted}}<p class="muted">Showing the first {{len .Traces}} traces; {{.TracesOmitted}} more are available in traces.csv and run.json.</p>{{end}}</section>{{end}}
<section class="grids">{{template "dimension" (dict "Title" "Users" "Rows" .Report.Users)}}{{template "dimension" (dict "Title" "Databases" "Rows" .Report.Databases)}}{{template "dimension" (dict "Title" "Processes" "Rows" .Report.Processes)}}</section>
<section class="panel"><h2>Parsing Quality</h2><p>Lines: {{.Report.Quality.LinesRead}} · events parsed: {{.Report.Quality.EventsParsed}} · malformed headers: {{.Report.Quality.MalformedHeaders}} · lines without event: {{.Report.Quality.OrphanLines}} · bytes read: {{.Report.Quality.BytesRead}}</p></section>
{{if .Report.Errors}}<section class="panel"><h2>File Errors</h2><ul>{{range .Report.Errors}}<li>{{.}}</li>{{end}}</ul></section>{{end}}
<script>const buckets={{.BucketJSON}};const canvas=document.getElementById("timeline"),empty=document.getElementById("chart-empty");if(!buckets.length){canvas.hidden=true;empty.hidden=false}else{const w=canvas.clientWidth||800,h=canvas.clientHeight||280,dpr=devicePixelRatio||1;canvas.width=w*dpr;canvas.height=h*dpr;const c=canvas.getContext("2d");c.scale(dpr,dpr);const values=buckets.map(x=>x.stats.duration_micros.sum),max=Math.max(...values,1),pad=34;c.strokeStyle="#dce4f0";c.beginPath();c.moveTo(pad,8);c.lineTo(pad,h-pad);c.lineTo(w-8,h-pad);c.stroke();c.strokeStyle="#1b6ac9";c.lineWidth=2;c.beginPath();values.forEach((v,i)=>{const x=pad+i*(w-pad-12)/Math.max(values.length-1,1),y=h-pad-v/max*(h-pad-18);i?c.lineTo(x,y):c.moveTo(x,y)});c.stroke();c.fillStyle="#65728a";c.font="12px Segoe UI,Arial";c.fillText("μs, max "+max.toFixed(0),pad,16);c.fillText(new Date(buckets[0].start).toLocaleString(),pad,h-10);c.fillText(new Date(buckets[buckets.length-1].start).toLocaleString(),Math.max(pad,w-190),h-10)}</script>
</main></body></html>{{define "dimension"}}<section class="panel"><h2>{{.Title}}</h2><div class="table"><table><thead><tr><th>Value</th><th>Count</th><th>Total, μs</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Value}}</td><td>{{.Stats.Count}}</td><td>{{number .Stats.Duration.Sum}}</td></tr>{{end}}</tbody></table></div></section>{{end}}{{define "lockaggregate"}}<div class="table"><table><thead><tr><th>Value</th><th>Count</th><th>Total, μs</th><th>p95, μs</th><th>Max., μs</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Key}}</td><td>{{.Stats.Count}}</td><td>{{.Stats.TotalMicros}}</td><td>{{.Stats.P95Micros}}</td><td>{{.Stats.MaxMicros}}</td></tr>{{end}}</tbody></table></div>{{if .Omitted}}<p class="muted">{{.Omitted}} {{.Label}} hidden.</p>{{end}}{{end}}`))

// dict keeps the template concise while preserving automatic HTML escaping.
func dict(values ...interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		key, _ := values[index].(string)
		result[key] = values[index+1]
	}
	return result
}
