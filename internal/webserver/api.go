package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"techlog-stat/internal/analysis/lockstats"
	"techlog-stat/internal/output"
	comparereport "techlog-stat/internal/report/compare"
	"techlog-stat/internal/report/overview"
	"techlog-stat/internal/techlog"
)

const defaultMaxBodyBytes int64 = 1 << 20

type HandlerOptions struct {
	MaxBodyBytes int64
	// AllowedHosts is the exact loopback Host allow-list (with optional port)
	// accepted by the DNS-rebinding/CSRF guard. Empty uses common loopback names.
	AllowedHosts []string
}

// NewHandler returns the complete versioned-in-practice /api handler. No
// endpoint reads an artifact from disk: downloads are rendered from a stored
// OverviewResult and an allow-list of names.
func NewHandler(manager *Manager, options HandlerOptions) http.Handler {
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = defaultMaxBodyBytes
	}
	if len(options.AllowedHosts) == 0 {
		options.AllowedHosts = []string{"localhost", "127.0.0.1", "[::1]"}
	}
	hosts := make(map[string]struct{}, len(options.AllowedHosts))
	for _, host := range options.AllowedHosts {
		hosts[strings.ToLower(host)] = struct{}{}
	}
	return &api{manager: manager, maxBodyBytes: options.MaxBodyBytes, allowedHosts: hosts}
}

type api struct {
	manager      *Manager
	maxBodyBytes int64
	allowedHosts map[string]struct{}
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.securityHeaders(w)
	if !a.allowedRequestOrigin(r) {
		a.error(w, http.StatusForbidden, "origin_forbidden", "Host and Origin must be loopback same-origin")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		a.error(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		return
	}
	prefix := "/api/v1"
	if strings.HasPrefix(r.URL.Path, "/api/v1/") || r.URL.Path == "/api/v1" { /* canonical */
	} else if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		prefix = "/api" // temporary compatibility alias
	} else {
		a.error(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	switch {
	case r.Method == http.MethodGet && path == "/health":
		a.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.Method == http.MethodGet && path == "/config":
		a.writeJSON(w, http.StatusOK, a.manager.Settings())
	case path == "/runs" && r.Method == http.MethodGet:
		a.writeJSON(w, http.StatusOK, map[string]any{"runs": a.manager.List()})
	case path == "/runs" && r.Method == http.MethodPost:
		a.createRun(w, r)
	case path == "/report/compare" && r.Method == http.MethodPost:
		a.compare(w, r)
	default:
		a.runRoute(w, r, path)
	}
}

func (a *api) runRoute(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "runs" || !validID(parts[1]) {
		a.error(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	id := parts[1]
	if len(parts) == 2 && r.Method == http.MethodGet {
		a.status(w, id)
		return
	}
	if len(parts) == 3 && r.Method == http.MethodPost && parts[2] == "cancel" {
		a.cancel(w, id)
		return
	}
	if len(parts) == 3 && r.Method == http.MethodPost && parts[2] == "delete" {
		a.delete(w, id)
		return
	}
	if len(parts) == 4 && r.Method == http.MethodGet && parts[2] == "report" && parts[3] == "overview" {
		a.overview(w, r, id)
		return
	}
	if len(parts) == 4 && r.Method == http.MethodGet && parts[2] == "report" && parts[3] == "raw-event" {
		a.rawEvent(w, r, id)
		return
	}
	if len(parts) == 4 && r.Method == http.MethodGet && parts[2] == "report" && parts[3] == "trace" {
		a.trace(w, r, id)
		return
	}
	if len(parts) == 4 && r.Method == http.MethodGet && parts[2] == "report" && parts[3] == "source-events" {
		a.sourceEvents(w, r, id)
		return
	}
	if len(parts) == 4 && r.Method == http.MethodGet && parts[2] == "downloads" {
		a.download(w, id, parts[3])
		return
	}
	a.error(w, http.StatusNotFound, "not_found", "endpoint not found")
}

func (a *api) createRun(w http.ResponseWriter, r *http.Request) {
	var request RunRequest
	if err := a.decode(r, w, &request); err != nil {
		return
	}
	run, err := a.manager.Create(request)
	if err != nil {
		a.error(w, http.StatusBadRequest, "invalid_run", err.Error())
		return
	}
	a.writeJSON(w, http.StatusAccepted, run)
}

func (a *api) status(w http.ResponseWriter, id string) {
	run, ok := a.manager.Get(id)
	if !ok {
		a.error(w, http.StatusNotFound, "run_not_found", "run not found")
		return
	}
	a.writeJSON(w, http.StatusOK, run)
}

func (a *api) cancel(w http.ResponseWriter, id string) {
	run, err := a.manager.Cancel(id)
	if err != nil {
		a.error(w, http.StatusNotFound, "run_not_found", err.Error())
		return
	}
	a.writeJSON(w, http.StatusAccepted, run)
}

func (a *api) delete(w http.ResponseWriter, id string) {
	if err := a.manager.Delete(id); err != nil {
		if err.Error() == "run not found" {
			a.error(w, http.StatusNotFound, "run_not_found", err.Error())
		} else {
			a.error(w, http.StatusConflict, "run_active", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) overview(w http.ResponseWriter, r *http.Request, id string) {
	report, run, err := a.manager.Result(id)
	if err != nil {
		a.resultError(w, run, err)
		return
	}
	section := r.URL.Query().Get("section")
	if section == "" {
		section = "event_types"
	}
	limit, offset, err := page(r)
	if err != nil {
		a.error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	filter := strings.ToLower(r.URL.Query().Get("filter"))
	items, total, err := overviewSection(report, section, filter, limit, offset)
	if err != nil {
		a.error(w, http.StatusBadRequest, "invalid_section", err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"run": run, "section": section, "filter": filter, "limit": limit, "offset": offset, "total": total, "items": items})
}

func (a *api) rawEvent(w http.ResponseWriter, r *http.Request, id string) {
	report, run, err := a.manager.Result(id)
	if err != nil {
		a.resultError(w, run, err)
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("top_event"))
	if err != nil || index < 0 || index >= len(report.TopEvents) {
		a.error(w, http.StatusBadRequest, "invalid_top_event", "top_event must identify a retained top event")
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"run": run, "top_event": index, "event": report.TopEvents[index]})
}

func (a *api) trace(w http.ResponseWriter, r *http.Request, id string) {
	report, run, err := a.manager.Result(id)
	if err != nil {
		a.resultError(w, run, err)
		return
	}
	traceID := r.URL.Query().Get("id")
	if traceID == "" {
		a.error(w, http.StatusBadRequest, "invalid_trace", "id is required")
		return
	}
	for _, trace := range report.Traces {
		if trace.ID == traceID {
			a.writeJSON(w, http.StatusOK, map[string]any{"run": run, "trace": trace})
			return
		}
	}
	a.error(w, http.StatusNotFound, "trace_not_found", "trace not found in this run")
}

type sourceEvent struct {
	Timestamp      time.Time         `json:"timestamp"`
	Event          string            `json:"event"`
	Level          int               `json:"level"`
	DurationMicros int64             `json:"duration_micros"`
	Source         string            `json:"source"`
	Usr            string            `json:"usr,omitempty"`
	DataBase       string            `json:"database,omitempty"`
	Process        string            `json:"process,omitempty"`
	Context        string            `json:"context,omitempty"`
	Fields         map[string]string `json:"fields"`
	Raw            string            `json:"raw"`
}

const maxSourceEventRawBytes = 16 << 10

// sourceEvents re-reads only the immutable set of files selected by this run;
// it never accepts a source path or glob from the HTTP client.
func (a *api) sourceEvents(w http.ResponseWriter, r *http.Request, id string) {
	report, run, err := a.manager.Result(id)
	if err != nil {
		a.resultError(w, run, err)
		return
	}
	limit, offset, err := sourcePage(r)
	if err != nil {
		a.error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	query := sourceQuery{Event: r.URL.Query().Get("event"), User: r.URL.Query().Get("user"), Database: r.URL.Query().Get("database"), Process: r.URL.Query().Get("process"), Context: r.URL.Query().Get("context"), Contains: r.URL.Query().Get("contains")}
	items, total, err := findSourceEvents(r.Context(), report.Matches, query, limit, offset)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		a.error(w, http.StatusInternalServerError, "source_read_failed", err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"run": run, "filters": query, "limit": limit, "offset": offset, "total": total, "items": items})
}

type sourceQuery struct {
	Event    string `json:"event,omitempty"`
	User     string `json:"user,omitempty"`
	Database string `json:"database,omitempty"`
	Process  string `json:"process,omitempty"`
	Context  string `json:"context,omitempty"`
	Contains string `json:"contains,omitempty"`
}

func findSourceEvents(ctx context.Context, matches []string, query sourceQuery, limit, offset int) ([]sourceEvent, int, error) {
	items, total := make([]sourceEvent, 0, limit), 0
	for _, path := range matches {
		if err := ctx.Err(); err != nil {
			return nil, total, err
		}
		_, err := techlog.ParseFileContext(ctx, path, func(event techlog.Event) error {
			if !query.matches(event) {
				return nil
			}
			if total >= offset && len(items) < limit {
				items = append(items, sourceEventFrom(event))
			}
			total++
			return nil
		})
		if err != nil {
			return nil, total, err
		}
	}
	return items, total, nil
}

func (q sourceQuery) matches(event techlog.Event) bool {
	if q.Event != "" && event.Name != q.Event {
		return false
	}
	if q.User != "" && !sourceDimensionMatches(event.Fields["Usr"], q.User) {
		return false
	}
	if q.Database != "" && !sourceDimensionMatches(event.Fields["DataBase"], q.Database) {
		return false
	}
	if q.Process != "" && !sourceDimensionMatches(event.Fields["process"], q.Process) {
		return false
	}
	contextValue := strings.Join(strings.Fields(event.Fields["Context"]), " ")
	if q.Context != "" && !sourceDimensionMatches(contextValue, q.Context) {
		return false
	}
	return q.Contains == "" || strings.Contains(event.Raw, q.Contains)
}

func sourceDimensionMatches(actual, wanted string) bool {
	if wanted == "(unknown)" {
		return strings.TrimSpace(actual) == ""
	}
	return actual == wanted
}

func sourceEventFrom(event techlog.Event) sourceEvent {
	raw := event.Raw
	if len(raw) > maxSourceEventRawBytes {
		raw = raw[:maxSourceEventRawBytes] + "\n… [truncated]"
	}
	fields := make(map[string]string, len(event.Fields))
	for key, value := range event.Fields {
		fields[key] = value
	}
	return sourceEvent{Timestamp: event.Timestamp, Event: event.Name, Level: event.Level, DurationMicros: event.DurationMicros, Source: event.Source, Usr: fields["Usr"], DataBase: fields["DataBase"], Process: fields["process"], Context: strings.Join(strings.Fields(fields["Context"]), " "), Fields: fields, Raw: raw}
}

type compareRequest struct {
	BaselineRunID      string  `json:"baseline_run_id"`
	CurrentRunID       string  `json:"current_run_id"`
	ThresholdPercent   float64 `json:"threshold_percent"`
	ThresholdAbsMicros float64 `json:"threshold_abs_micros"`
}

func (a *api) compare(w http.ResponseWriter, r *http.Request) {
	var request compareRequest
	if err := a.decode(r, w, &request); err != nil {
		return
	}
	if !validID(request.BaselineRunID) || !validID(request.CurrentRunID) {
		a.error(w, http.StatusBadRequest, "invalid_run", "baseline_run_id and current_run_id are required")
		return
	}
	base, _, baseErr := a.manager.Result(request.BaselineRunID)
	current, _, currentErr := a.manager.Result(request.CurrentRunID)
	if baseErr != nil || currentErr != nil {
		a.error(w, http.StatusConflict, "run_not_completed", "both runs must have completed successfully")
		return
	}
	options := comparereport.Options{RegressionPercent: request.ThresholdPercent, ImprovementPercent: request.ThresholdPercent, MinAbsoluteDeltaMicros: request.ThresholdAbsMicros}
	if err := comparereport.ValidateOptions(options); err != nil {
		a.error(w, http.StatusBadRequest, "invalid_threshold", err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, comparereport.Compare(base, current, options))
}

func (a *api) download(w http.ResponseWriter, id, name string) {
	report, run, err := a.manager.Result(id)
	if err != nil {
		a.resultError(w, run, err)
		return
	}
	data, contentType, err := renderDownload(name, report)
	if errors.Is(err, errUnknownDownload) {
		a.error(w, http.StatusNotFound, "download_not_found", "download not found")
		return
	}
	if err != nil {
		a.error(w, http.StatusInternalServerError, "render_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

var errUnknownDownload = errors.New("unknown download")

func renderDownload(name string, report overview.OverviewResult) ([]byte, string, error) {
	if fn, ok := map[string]func(overview.OverviewResult) ([]byte, error){
		"event_types.csv": output.RenderOverviewEventTypesCSV, "sql.csv": output.RenderOverviewSQLCSV,
		"traces.csv": output.RenderOverviewTracesCSV, "locks.csv": output.RenderOverviewLocksCSV,
		"scall.csv": output.RenderOverviewSCALLCSV, "web.csv": output.RenderOverviewWebCSV,
		"sessions.csv": output.RenderOverviewSessionsCSV, "processes.csv": output.RenderOverviewProcessesCSV,
		"licenses.csv": output.RenderOverviewLicensesCSV, "filedb.csv": output.RenderOverviewFileDBCSV,
		"error_contexts.csv": output.RenderOverviewErrorContextsCSV,
	}[name]; ok {
		data, err := fn(report)
		return data, "text/csv; charset=utf-8", err
	}
	switch name {
	case "run.json":
		data, err := output.RenderOverviewJSON(report)
		return data, "application/json; charset=utf-8", err
	case "report.html":
		data, err := output.RenderOverviewHTML(report)
		return data, "text/html; charset=utf-8", err
	default:
		return nil, "", errUnknownDownload
	}
}

func overviewSection(report overview.OverviewResult, section, filter string, limit, offset int) (any, int, error) {
	match := func(value string) bool { return filter == "" || strings.Contains(strings.ToLower(value), filter) }
	switch section {
	case "event_types":
		items := make([]overview.EventTypeStat, 0)
		for _, item := range report.EventTypes {
			if match(item.Event) {
				items = append(items, item)
			}
		}
		return slicePage(items, limit, offset), len(items), nil
	case "time_buckets":
		items := make([]overview.TimeBucketStat, 0)
		for _, item := range report.Buckets {
			if match(item.Start.Format(time.RFC3339)) || match(item.End.Format(time.RFC3339)) {
				items = append(items, item)
			}
		}
		return slicePage(items, limit, offset), len(items), nil
	case "top_events":
		items := make([]overview.RawEvent, 0)
		for _, item := range report.TopEvents {
			if match(item.Event) || match(item.Source) {
				items = append(items, item)
			}
		}
		return slicePage(items, limit, offset), len(items), nil
	case "traces":
		items := make([]any, 0)
		for _, item := range report.Traces {
			if match(item.ID) || match(item.Process) {
				items = append(items, item)
			}
		}
		return pageAny(items, limit, offset), len(items), nil
	case "sql_rows":
		items := make([]any, 0)
		for _, item := range report.SQLRows {
			if match(item.Fingerprint) || match(item.NormalizedQuery) {
				items = append(items, item)
			}
		}
		return pageAny(items, limit, offset), len(items), nil
	case "users":
		return dimensionsSection(report.Users, match, limit, offset)
	case "databases":
		return dimensionsSection(report.Databases, match, limit, offset)
	case "processes":
		return dimensionsSection(report.Processes, match, limit, offset)
	case "contexts":
		return dimensionsSection(report.Contexts, match, limit, offset)
	case "locks":
		items := lockRows(report.Locks, match)
		if limit > 500 {
			limit = 500
		}
		return slicePage(items, limit, offset), len(items), nil
	default:
		return nil, 0, fmt.Errorf("unsupported section")
	}
}

// lockRow is a deliberately raw-sample-free, snake_case table for the locks
// view. The aggregations are bounded at response time even when a log contains
// a very high number of distinct keys.
type lockRow struct {
	Kind        string   `json:"kind"`
	Key         string   `json:"key,omitempty"`
	EventType   string   `json:"event_type,omitempty"`
	Context     string   `json:"context,omitempty"`
	Tables      []string `json:"tables,omitempty"`
	Regions     []string `json:"regions,omitempty"`
	Waiter      string   `json:"waiter,omitempty"`
	Blocker     string   `json:"blocker,omitempty"`
	Source      string   `json:"source,omitempty"`
	Count       int64    `json:"count,omitempty"`
	TotalMicros int64    `json:"total_micros,omitempty"`
	MeanMicros  float64  `json:"mean_micros,omitempty"`
	P95Micros   int64    `json:"p95_micros,omitempty"`
}

func lockRows(result lockstats.Result, match func(string) bool) []lockRow {
	rows := make([]lockRow, 0, len(result.ByEvent)+len(result.ByContext)+len(result.ByTable)+len(result.ByRegion)+len(result.TopConflicts)+len(result.Relations))
	addAggregates := func(kind string, values []lockstats.Aggregate) {
		for _, value := range values {
			if match(value.Key) {
				rows = append(rows, lockRow{Kind: kind, Key: value.Key, Count: value.Stats.Count, TotalMicros: value.Stats.TotalMicros, MeanMicros: value.Stats.MeanMicros, P95Micros: value.Stats.P95Micros})
			}
		}
	}
	addAggregates("event", result.ByEvent)
	addAggregates("context", result.ByContext)
	addAggregates("table", result.ByTable)
	addAggregates("region", result.ByRegion)
	for _, value := range result.TopConflicts {
		key := value.EventType + " " + value.Context + " " + strings.Join(value.Tables, " ") + " " + strings.Join(value.Regions, " ")
		if match(key) {
			rows = append(rows, lockRow{Kind: "conflict", EventType: value.EventType, Context: value.Context, Tables: value.Tables, Regions: value.Regions, Count: value.Stats.Count, TotalMicros: value.Stats.TotalMicros, MeanMicros: value.Stats.MeanMicros, P95Micros: value.Stats.P95Micros})
		}
	}
	for _, value := range result.Relations {
		key := value.EventType + " " + value.Waiter + " " + value.Blocker + " " + value.Context + " " + value.Source
		if match(key) {
			rows = append(rows, lockRow{Kind: "relation", EventType: value.EventType, Waiter: value.Waiter, Blocker: value.Blocker, Context: value.Context, Source: value.Source})
		}
	}
	return rows
}

func dimensionsSection(source []overview.DimensionStat, match func(string) bool, limit, offset int) (any, int, error) {
	items := make([]overview.DimensionStat, 0)
	for _, item := range source {
		if match(item.Value) {
			items = append(items, item)
		}
	}
	return slicePage(items, limit, offset), len(items), nil
}
func slicePage[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
func pageAny(items []any, limit, offset int) []any {
	if offset >= len(items) {
		return []any{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func page(r *http.Request) (int, int, error) {
	limit, offset := 100, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("limit must be an integer")
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("offset must be an integer")
		}
	}
	if limit < 1 || limit > 1000 || offset < 0 {
		return 0, 0, fmt.Errorf("limit must be 1..1000 and offset must not be negative")
	}
	return limit, offset, nil
}
func sourcePage(r *http.Request) (int, int, error) {
	limit, offset := 100, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("limit must be an integer")
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("offset must be an integer")
		}
	}
	if limit < 1 || limit > 500 || offset < 0 {
		return 0, 0, fmt.Errorf("limit must be 1..500 and offset must not be negative")
	}
	return limit, offset, nil
}

func (a *api) decode(r *http.Request, w http.ResponseWriter, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, a.maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		a.error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON within the size limit")
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		a.error(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value")
		return err
	}
	return nil
}

func (a *api) resultError(w http.ResponseWriter, run Run, err error) {
	if run.ID == "" {
		a.error(w, http.StatusNotFound, "run_not_found", "run not found")
	} else {
		a.error(w, http.StatusConflict, "run_not_completed", err.Error())
	}
}
func (a *api) securityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
}
func (a *api) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (a *api) error(w http.ResponseWriter, status int, code, message string) {
	a.writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func validID(id string) bool {
	if len(id) < 3 || !strings.HasPrefix(id, "r-") {
		return false
	}
	for _, char := range id[2:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func (a *api) allowedRequestOrigin(r *http.Request) bool {
	if !a.allowedHost(r.Host) {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || !a.allowedHost(parsed.Host) {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func (a *api) allowedHost(host string) bool {
	host = strings.ToLower(host)
	if _, ok := a.allowedHosts[host]; ok {
		return true
	}
	bare := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		bare = strings.Trim(parsed, "[]")
	} else {
		bare = strings.Trim(bare, "[]")
	}
	_, ok := a.allowedHosts[bare]
	return ok
}
