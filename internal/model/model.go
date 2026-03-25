package model

import "time"

type RunMeta struct {
	ToolVersion    string        `json:"tool_version"`
	Report         string        `json:"report"`
	StartedAt      time.Time     `json:"started_at"`
	FinishedAt     time.Time     `json:"finished_at"`
	Duration       time.Duration `json:"duration_ns"`
	InputRoot      string        `json:"input_root"`
	Glob           string        `json:"glob"`
	OutputDir      string        `json:"output_dir"`
	Workers        int           `json:"workers"`
	TopN           int           `json:"top_n"`
	Formats        []string      `json:"formats"`
	FilesMatched   int           `json:"files_matched"`
	FilesProcessed int           `json:"files_processed"`
	FilesFailed    int           `json:"files_failed"`
	BytesRead      int64         `json:"bytes_read"`
}

type Totals struct {
	EventCount      int64   `json:"event_count"`
	DurationMS      float64 `json:"duration_ms"`
	AverageDuration float64 `json:"average_duration_ms"`
}

type ContextRow struct {
	Rank            int     `json:"rank"`
	Context         string  `json:"context"`
	ShortContext    string  `json:"short_context"`
	TotalDurationMS float64 `json:"total_duration_ms"`
	TimePct         float64 `json:"time_pct"`
	Count           int64   `json:"count"`
	CountPct        float64 `json:"count_pct"`
	AverageMS       float64 `json:"avg_duration_ms"`
}

type ContextReport struct {
	Meta    RunMeta      `json:"meta"`
	Totals  Totals       `json:"totals"`
	Rows    []ContextRow `json:"rows"`
	Errors  []string     `json:"errors,omitempty"`
	Matches []string     `json:"matches,omitempty"`
}

type ErrorRow struct {
	Rank             int     `json:"rank"`
	Event            string  `json:"event"`
	Description      string  `json:"description"`
	ShortDescription string  `json:"short_description"`
	TotalDurationMS  float64 `json:"total_duration_ms"`
	TimePct          float64 `json:"time_pct"`
	Count            int64   `json:"count"`
	CountPct         float64 `json:"count_pct"`
	AverageMS        float64 `json:"avg_duration_ms"`
}

type ErrorReport struct {
	Meta    RunMeta    `json:"meta"`
	Totals  Totals     `json:"totals"`
	Rows    []ErrorRow `json:"rows"`
	Errors  []string   `json:"errors,omitempty"`
	Matches []string   `json:"matches,omitempty"`
}
