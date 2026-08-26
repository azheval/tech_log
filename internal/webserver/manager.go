// Package webserver provides the bounded, local-only backend for a future UI.
package webserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"techlog-stat/internal/config"
	"techlog-stat/internal/report/overview"
)

const (
	defaultMaxRuns   = 20
	defaultMaxActive = 1
)

// Progress is a best-effort, bounded status signal from a Builder.
type Progress struct {
	Phase   string `json:"phase"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

// Builder is deliberately an adapter boundary around overview.BuildContext.
type Builder func(context.Context, overview.Options, func(Progress)) (overview.OverviewResult, error)

// DefaultBuilder maps overview's serial callback onto the web progress shape.
func DefaultBuilder(ctx context.Context, options overview.Options, progress func(Progress)) (overview.OverviewResult, error) {
	options.Progress = func(update overview.Progress) {
		progress(Progress{Phase: update.Status, Current: update.FilesCompleted + update.FilesFailed, Total: update.FilesMatched})
	}
	return overview.BuildContext(ctx, options)
}

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunCanceled  RunStatus = "canceled"
	RunFailed    RunStatus = "failed"
)

// RunRequest intentionally accepts only overview settings; it never accepts
// output or artifact paths.
type RunRequest struct {
	InputRoot          string          `json:"input_root"`
	Glob               string          `json:"glob"`
	BucketInterval     string          `json:"bucket_interval"`
	TopN               int             `json:"top_n"`
	Workers            int             `json:"workers"`
	MinDurationMicros  int64           `json:"min_duration_micros"`
	Filters            []FilterRequest `json:"filters,omitempty"`
	MaxOpenTraces      int             `json:"max_open_traces,omitempty"`
	MaxTraces          int             `json:"max_traces,omitempty"`
	LockSampleLimit    int             `json:"lock_sample_limit,omitempty"`
	SCALLSampleLimit   int             `json:"scall_sample_limit,omitempty"`
	WebSampleLimit     int             `json:"web_sample_limit,omitempty"`
	SessionSampleLimit int             `json:"session_sample_limit,omitempty"`
}

type FilterRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (r RunRequest) options() (overview.Options, error) {
	if r.InputRoot == "" || r.Glob == "" || r.BucketInterval == "" {
		return overview.Options{}, fmt.Errorf("input_root, glob, and bucket_interval are required")
	}
	bucket, err := time.ParseDuration(r.BucketInterval)
	if err != nil || bucket <= 0 {
		return overview.Options{}, fmt.Errorf("bucket_interval must be a positive duration")
	}
	if r.TopN < 0 || r.Workers < 0 || r.MinDurationMicros < 0 {
		return overview.Options{}, fmt.Errorf("top_n, workers, and min_duration_micros must not be negative")
	}
	filters := make([]config.Filter, 0, len(r.Filters))
	for _, filter := range r.Filters {
		if strings.TrimSpace(filter.Key) == "" {
			return overview.Options{}, fmt.Errorf("filter key must not be empty")
		}
		filters = append(filters, config.Filter{Key: filter.Key, Value: filter.Value})
	}
	return overview.Options{
		InputRoot: r.InputRoot, Glob: r.Glob, BucketInterval: bucket, TopN: r.TopN, Workers: r.Workers,
		Filters: filters, MinDurationMicros: r.MinDurationMicros, MaxOpenTraces: r.MaxOpenTraces,
		MaxTraces: r.MaxTraces, LockSampleLimit: r.LockSampleLimit, SCALLSampleLimit: r.SCALLSampleLimit,
		WebSampleLimit: r.WebSampleLimit, SessionSampleLimit: r.SessionSampleLimit,
	}, nil
}

// Run is an immutable API snapshot. Result is available only after success.
type Run struct {
	ID          string                   `json:"id"`
	Status      RunStatus                `json:"status"`
	Request     RunRequest               `json:"request"`
	Progress    Progress                 `json:"progress"`
	CreatedAt   time.Time                `json:"created_at"`
	StartedAt   *time.Time               `json:"started_at,omitempty"`
	FinishedAt  *time.Time               `json:"finished_at,omitempty"`
	Error       string                   `json:"error,omitempty"`
	CancelReady bool                     `json:"cancel_ready"`
	Result      *overview.OverviewResult `json:"-"`
}

type ManagerOptions struct {
	MaxRuns          int
	MaxActive        int
	AllowedInputRoot string
	// DefaultRequest is display-only data returned from GET /config. It does
	// not grant access or bypass Create validation.
	DefaultRequest RunRequest
	Builder        Builder
	Now            func() time.Time
}

// Manager owns bounded in-memory results and all run cancellation functions.
type Manager struct {
	mu               sync.RWMutex
	runs             map[string]*Run
	order            []string
	next             uint64
	maxRuns          int
	maxActive        int
	active           int
	allowedInputRoot string
	defaultRequest   RunRequest
	builder          Builder
	now              func() time.Time
	cancels          map[string]context.CancelFunc
	workers          sync.WaitGroup
	closing          bool
}

func NewManager(options ManagerOptions) (*Manager, error) {
	if options.MaxRuns <= 0 {
		options.MaxRuns = defaultMaxRuns
	}
	if options.MaxActive <= 0 {
		options.MaxActive = defaultMaxActive
	}
	if options.Builder == nil {
		options.Builder = DefaultBuilder
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	root := ""
	if options.AllowedInputRoot != "" {
		var err error
		root, err = resolveInputDirectory(options.AllowedInputRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve allowed input root: %w", err)
		}
	}
	if root != "" {
		root = filepath.Clean(root)
	}
	return &Manager{runs: make(map[string]*Run), maxRuns: options.MaxRuns, maxActive: options.MaxActive, allowedInputRoot: root, defaultRequest: normalizeDisplayDefaults(options.DefaultRequest), builder: options.Builder, now: options.Now, cancels: make(map[string]context.CancelFunc)}, nil
}

func (m *Manager) Settings() Settings {
	return Settings{MaxRuns: m.maxRuns, MaxActive: m.maxActive, AllowedInputRoot: m.allowedInputRoot, DefaultRequest: m.defaultRequest}
}

type Settings struct {
	MaxRuns          int        `json:"max_runs"`
	MaxActive        int        `json:"max_active"`
	AllowedInputRoot string     `json:"allowed_input_root,omitempty"`
	DefaultRequest   RunRequest `json:"default_request"`
}

func normalizeDisplayDefaults(request RunRequest) RunRequest {
	request.InputRoot = strings.TrimSpace(request.InputRoot)
	request.Glob = strings.TrimSpace(request.Glob)
	request.BucketInterval = strings.TrimSpace(request.BucketInterval)
	if request.BucketInterval != "" {
		if duration, err := time.ParseDuration(request.BucketInterval); err != nil || duration <= 0 {
			request.BucketInterval = ""
		}
	}
	if request.TopN < 0 {
		request.TopN = 0
	}
	if request.Workers < 0 {
		request.Workers = 0
	}
	if request.MinDurationMicros < 0 {
		request.MinDurationMicros = 0
	}
	filters := make([]FilterRequest, 0, len(request.Filters))
	for _, filter := range request.Filters {
		if strings.TrimSpace(filter.Key) != "" {
			filters = append(filters, filter)
		}
	}
	request.Filters = filters
	return request
}

func (m *Manager) Create(request RunRequest) (Run, error) {
	options, err := request.options()
	if err != nil {
		return Run{}, err
	}
	if err := m.checkInputRoot(options.InputRoot); err != nil {
		return Run{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return Run{}, fmt.Errorf("run manager is shutting down")
	}
	if m.active >= m.maxActive {
		return Run{}, fmt.Errorf("maximum number of active runs reached")
	}
	m.evictForNewRunLocked()
	if len(m.runs) >= m.maxRuns {
		return Run{}, fmt.Errorf("run retention is full; wait for a run to finish or delete one")
	}
	m.next++
	id := fmt.Sprintf("r-%d", m.next)
	now := m.now().UTC()
	started := now
	run := &Run{ID: id, Status: RunRunning, Request: request, Progress: Progress{Phase: "starting"}, CreatedAt: now, StartedAt: &started, CancelReady: true}
	m.runs[id] = run
	m.order = append(m.order, id)
	m.active++
	ctx, cancel := context.WithCancel(context.Background())
	m.cancels[id] = cancel
	m.workers.Add(1)
	go m.execute(ctx, id, options)
	return snapshot(run), nil
}

func (m *Manager) execute(ctx context.Context, id string, options overview.Options) {
	defer m.workers.Done()
	result, err := m.builder(ctx, options, func(progress Progress) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if run := m.runs[id]; run != nil && (run.Status == RunRunning || run.Status == RunQueued) {
			run.Progress = progress
		}
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[id]
	if run == nil {
		return
	}
	now := m.now().UTC()
	run.FinishedAt, run.CancelReady = &now, false
	delete(m.cancels, id)
	m.active--
	if ctx.Err() != nil {
		run.Status, run.Error = RunCanceled, "canceled"
	} else if err != nil {
		run.Status, run.Error = RunFailed, err.Error()
	} else {
		progress := run.Progress
		progress.Phase = "complete"
		if progress.Total <= 0 {
			progress.Total = result.Meta.FilesMatched
		}
		progress.Current = progress.Total
		run.Status, run.Result, run.Progress = RunSucceeded, &result, progress
	}
	m.evictLocked()
}

// CancelAll requests cancellation for every active BuildContext. It does not
// wait, so an interactive caller can return immediately after the request.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, cancel := range m.cancels {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// Shutdown stops new runs, cancels active builds, and waits for their worker
// goroutines to exit. It never holds the manager mutex while waiting.
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	m.closing = true
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, cancel := range m.cancels {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() { m.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Get(id string) (Run, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	run, ok := m.runs[id]
	return snapshot(run), ok
}

func (m *Manager) List() []Run {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]Run, 0, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		if run := m.runs[m.order[i]]; run != nil {
			items = append(items, snapshot(run))
		}
	}
	return items
}

func (m *Manager) Cancel(id string) (Run, error) {
	m.mu.Lock()
	run := m.runs[id]
	if run == nil {
		m.mu.Unlock()
		return Run{}, fmt.Errorf("run not found")
	}
	if run.Status != RunRunning && run.Status != RunQueued {
		result := snapshot(run)
		m.mu.Unlock()
		return result, nil
	}
	cancel, result := m.cancels[id], snapshot(run)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return result, nil
}

// Delete only removes terminal runs, retaining no results or artifacts on disk.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[id]
	if run == nil {
		return fmt.Errorf("run not found")
	}
	if run.Status == RunRunning || run.Status == RunQueued {
		return fmt.Errorf("running runs cannot be deleted")
	}
	m.removeLocked(id)
	return nil
}

func (m *Manager) Result(id string) (overview.OverviewResult, Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	run := m.runs[id]
	if run == nil {
		return overview.OverviewResult{}, Run{}, fmt.Errorf("run not found")
	}
	if run.Status != RunSucceeded || run.Result == nil {
		return overview.OverviewResult{}, snapshot(run), fmt.Errorf("run is not completed successfully")
	}
	return *run.Result, snapshot(run), nil
}

func (m *Manager) checkInputRoot(input string) error {
	abs, err := resolveInputDirectory(input)
	if err != nil {
		return fmt.Errorf("resolve input_root: %w", err)
	}
	if m.allowedInputRoot == "" {
		return nil
	}
	rel, err := filepath.Rel(m.allowedInputRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("input_root must be within the configured allowed input root")
	}
	return nil
}

func resolveInputDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func (m *Manager) evictLocked() {
	for len(m.runs) > m.maxRuns {
		removed := false
		for _, id := range m.order {
			if run := m.runs[id]; run != nil && run.Status != RunRunning && run.Status != RunQueued {
				m.removeLocked(id)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func (m *Manager) evictForNewRunLocked() {
	for len(m.runs) >= m.maxRuns {
		removed := false
		for _, id := range m.order {
			run := m.runs[id]
			if run != nil && run.Status != RunRunning && run.Status != RunQueued {
				m.removeLocked(id)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func (m *Manager) removeLocked(id string) {
	delete(m.runs, id)
	for i, value := range m.order {
		if value == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}
func snapshot(run *Run) Run {
	if run == nil {
		return Run{}
	}
	copy := *run
	copy.Result = nil
	return copy
}
