package webserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/analysis/lockstats"
	"techlog-stat/internal/report/overview"
)

func TestManagerLifecycleAndRetention(t *testing.T) {
	builder := func(_ context.Context, _ overview.Options, progress func(Progress)) (overview.OverviewResult, error) {
		progress(Progress{Phase: "parsing", Current: 1, Total: 3})
		return overview.OverviewResult{}, nil
	}
	manager, err := NewManager(ManagerOptions{Builder: builder, MaxRuns: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, first.ID, RunSucceeded)
	completed, _ := manager.Get(first.ID)
	if completed.Progress.Phase != "complete" || completed.Progress.Current != 3 || completed.Progress.Total != 3 {
		t.Fatalf("completed progress = %+v", completed.Progress)
	}
	second, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, second.ID, RunSucceeded)
	if _, ok := manager.Get(first.ID); ok {
		t.Fatal("old terminal run was not evicted")
	}
	if err := manager.Delete(second.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Get(second.ID); ok {
		t.Fatal("deleted run remained visible")
	}
}

func TestManagerCancel(t *testing.T) {
	started := make(chan struct{})
	builder := func(ctx context.Context, _ overview.Options, _ func(Progress)) (overview.OverviewResult, error) {
		close(started)
		<-ctx.Done()
		return overview.OverviewResult{}, ctx.Err()
	}
	manager, err := NewManager(ManagerOptions{Builder: builder})
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := manager.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, run.ID, RunCanceled)
}

func TestManagerShutdownCancelsAndWaits(t *testing.T) {
	started := make(chan struct{})
	builder := func(ctx context.Context, _ overview.Options, _ func(Progress)) (overview.OverviewResult, error) {
		close(started)
		<-ctx.Done()
		return overview.OverviewResult{}, ctx.Err()
	}
	manager, err := NewManager(ManagerOptions{Builder: builder})
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, run.ID, RunCanceled)
	if _, err := manager.Create(validRequest()); err == nil {
		t.Fatal("manager accepted a run after shutdown")
	}
}

func TestServeStopsOnContextAndRejectsNonLoopback(t *testing.T) {
	if err := Serve(context.Background(), "0.0.0.0:0", http.NotFoundHandler()); err == nil {
		t.Fatal("non-loopback address accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "127.0.0.1:0", http.NotFoundHandler()) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop after cancellation")
	}
}

func TestAPIInvalidInputAndTraversal(t *testing.T) {
	manager, err := NewManager(ManagerOptions{AllowedInputRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(manager, HandlerOptions{})
	response := request(handler, http.MethodPost, "/api/v1/runs", `{"input_root":"..","glob":"*.log","bucket_interval":"1m"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	response = request(handler, http.MethodGet, "/api/v1/runs/r-1/downloads/..%2Fsecret", "")
	if response.Code == http.StatusOK {
		t.Fatal("path traversal download was accepted")
	}
	response = request(handler, http.MethodPut, "/api/v1/health", "")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d, want 405", response.Code)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("API security headers missing")
	}
}

func TestManagerRejectsFilesAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(ManagerOptions{AllowedInputRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.InputRoot = file
	if _, err := manager.Create(request); err == nil {
		t.Fatal("regular file accepted as input_root")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	request.InputRoot = link
	if _, err := manager.Create(request); err == nil {
		t.Fatal("symlink escape accepted as input_root")
	}
}

func TestAPIDownloadIsAllowListedAndAttached(t *testing.T) {
	builder := func(_ context.Context, _ overview.Options, _ func(Progress)) (overview.OverviewResult, error) {
		return overview.OverviewResult{}, nil
	}
	manager, err := NewManager(ManagerOptions{Builder: builder})
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, run.ID, RunSucceeded)
	handler := NewHandler(manager, HandlerOptions{})
	response := request(handler, http.MethodGet, "/api/v1/runs/"+run.ID+"/downloads/run.json", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="run.json"` {
		t.Fatalf("content disposition = %q", got)
	}
	response = request(handler, http.MethodGet, "/api/v1/runs/"+run.ID+"/downloads/not-an-artifact.csv", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown download status = %d", response.Code)
	}
}

func TestAPIUsesV1AndRejectsForeignOrigin(t *testing.T) {
	manager, err := NewManager(ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(manager, HandlerOptions{})
	response := request(handler, http.MethodGet, "/api/v1/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("v1 health status = %d", response.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "https://evil.invalid")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("origin status = %d", response.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
}

func TestConfigReturnsNormalizedDisplayDefaults(t *testing.T) {
	manager, err := NewManager(ManagerOptions{DefaultRequest: RunRequest{InputRoot: " logs ", Glob: " *.log ", BucketInterval: "bad", TopN: -1, Workers: -2, MinDurationMicros: -3, Filters: []FilterRequest{{Key: "", Value: "discard"}, {Key: "Usr", Value: "demo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	response := request(NewHandler(manager, HandlerOptions{}), http.MethodGet, "/api/v1/config", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var settings Settings
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.DefaultRequest.InputRoot != "logs" || settings.DefaultRequest.Glob != "*.log" || settings.DefaultRequest.BucketInterval != "" || settings.DefaultRequest.TopN != 0 || settings.DefaultRequest.Workers != 0 || settings.DefaultRequest.MinDurationMicros != 0 || len(settings.DefaultRequest.Filters) != 1 {
		t.Fatalf("unexpected defaults: %+v", settings.DefaultRequest)
	}
}

func TestOverviewLocksSectionReturnsBoundedAggregateRows(t *testing.T) {
	builder := func(_ context.Context, _ overview.Options, _ func(Progress)) (overview.OverviewResult, error) {
		return overview.OverviewResult{Locks: lockstats.Result{
			ByEvent:   []lockstats.Aggregate{{Key: "TLOCK", Stats: lockstats.DurationStats{Count: 2, TotalMicros: 10, P95Micros: 8}}},
			Relations: []lockstats.Relation{{EventType: "TLOCK", Waiter: "w", Blocker: "b", Context: "ctx", Source: "source"}},
		}}, nil
	}
	manager, err := NewManager(ManagerOptions{Builder: builder})
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, run.ID, RunSucceeded)
	response := request(NewHandler(manager, HandlerOptions{}), http.MethodGet, "/api/v1/runs/"+run.ID+"/report/overview?section=locks&limit=500", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Total int `json:"total"`
		Items []struct {
			Kind      string `json:"kind"`
			EventType string `json:"event_type"`
			Raw       string `json:"raw"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 2 || len(payload.Items) != 2 || payload.Items[0].Kind != "event" || payload.Items[0].EventType != "" {
		t.Fatalf("unexpected locks payload: %+v", payload)
	}
	if payload.Items[0].Raw != "" {
		t.Fatal("lock raw sample leaked into overview table")
	}
}

func TestOverviewSectionSupportsConsoleGroupingDimensions(t *testing.T) {
	start := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	report := overview.OverviewResult{
		EventTypes: []overview.EventTypeStat{{Event: "SDBL"}},
		Buckets:    []overview.TimeBucketStat{{Start: start, End: start.Add(5 * time.Minute)}},
		Users:      []overview.DimensionStat{{Value: "alice"}},
		Databases:  []overview.DimensionStat{{Value: "main"}},
		Processes:  []overview.DimensionStat{{Value: "rphost"}},
		Contexts:   []overview.DimensionStat{{Value: "module call"}},
	}
	for _, section := range []string{"event_types", "time_buckets", "users", "databases", "processes", "contexts"} {
		_, total, err := overviewSection(report, section, "", 100, 0)
		if err != nil {
			t.Fatalf("section %s: %v", section, err)
		}
		if total != 1 {
			t.Fatalf("section %s total = %d, want 1", section, total)
		}
	}
	_, total, err := overviewSection(report, "time_buckets", "2026-08-23t10:00", 100, 0)
	if err != nil || total != 1 {
		t.Fatalf("filtered time buckets total = %d, err = %v", total, err)
	}
}

func TestSourceEventsUsesOnlyStoredMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "26082310.log")
	if err := os.WriteFile(path, []byte("00:00.000000-12,SDBL,1,Usr=alice,DataBase=main,process=rphost,Context='Document.Form',Sdbl='select 1'\n00:01.000000-4,PROC,0,process=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	builder := func(_ context.Context, _ overview.Options, _ func(Progress)) (overview.OverviewResult, error) {
		return overview.OverviewResult{Matches: []string{path}}, nil
	}
	manager, err := NewManager(ManagerOptions{Builder: builder})
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Create(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, run.ID, RunSucceeded)
	response := request(NewHandler(manager, HandlerOptions{}), http.MethodGet, "/api/v1/runs/"+run.ID+"/report/source-events?event=SDBL&user=alice&limit=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Total int `json:"total"`
		Items []struct {
			Event string `json:"event"`
			Usr   string `json:"usr"`
			Raw   string `json:"raw"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].Event != "SDBL" || payload.Items[0].Usr != "alice" || payload.Items[0].Raw == "" {
		t.Fatalf("unexpected source event payload: %+v", payload)
	}
	for _, query := range []string{"context=Document.Form", "user=%28unknown%29", "database=%28unknown%29"} {
		response = request(NewHandler(manager, HandlerOptions{}), http.MethodGet, "/api/v1/runs/"+run.ID+"/report/source-events?"+query, "")
		if response.Code != http.StatusOK {
			t.Fatalf("query %s: status = %d, body=%s", query, response.Code, response.Body.String())
		}
		var filtered struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &filtered); err != nil {
			t.Fatal(err)
		}
		if filtered.Total != 1 {
			t.Fatalf("query %s: total = %d, want 1", query, filtered.Total)
		}
	}
}

func validRequest() RunRequest {
	return RunRequest{InputRoot: ".", Glob: "*.log", BucketInterval: "1m", TopN: 10, Workers: 1}
}

func waitStatus(t *testing.T, manager *Manager, id string, wanted RunStatus) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, ok := manager.Get(id)
		if ok && run.Status == wanted {
			return
		}
		time.Sleep(time.Millisecond)
	}
	run, _ := manager.Get(id)
	t.Fatalf("run status = %q, want %q", run.Status, wanted)
}

func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
