package config

import (
	"fmt"
	"time"
)

const (
	ReportSDBLContext      = "sdbl-context"
	ReportCALLContext      = "call-context"
	ReportDBMSSQLContext   = "dbmssql-context"
	ReportPostgresContext  = "postgres-context"
	ReportFileDBContext    = "file-context"
	ReportLockContext      = "lock-context"
	ReportTimeoutContext   = "timeout-context"
	ReportDeadlockContext  = "deadlock-context"
	ReportErrorDescr       = "error-descr"
	ReportDBPOSTGRSContext = "dbpostgrs-context"
	ReportDBV8DBEngContext = "dbv8dbeng-context"
	ReportLocksContext     = "locks-context"
	ReportEXCPDescr        = "excp-descr"

	ModeAggregate = "aggregate"
	ModeRaw       = "raw"
)

type Config struct {
	Report            string
	Mode              string
	InputRoot         string
	Glob              string
	OutputDir         string
	Formats           []string
	Filters           []Filter
	MinDurationMicros int64
	TimeRange         TimeRange
	TopN              int
	Workers           int
}

func NormalizeReport(report string) string {
	switch report {
	case ReportDBPOSTGRSContext:
		return ReportPostgresContext
	case ReportDBV8DBEngContext:
		return ReportFileDBContext
	case ReportLocksContext:
		return ReportLockContext
	case ReportEXCPDescr:
		return ReportErrorDescr
	default:
		return report
	}
}

func NormalizeMode(mode string) string {
	switch mode {
	case "", ModeAggregate:
		return ModeAggregate
	case ModeRaw:
		return ModeRaw
	default:
		return mode
	}
}

func (c Config) Validate() error {
	switch c.Report {
	case ReportSDBLContext, ReportCALLContext, ReportDBMSSQLContext, ReportPostgresContext, ReportFileDBContext, ReportLockContext, ReportTimeoutContext, ReportDeadlockContext, ReportErrorDescr:
	default:
		return fmt.Errorf("unsupported report: %s", c.Report)
	}
	switch c.Mode {
	case ModeAggregate, ModeRaw:
	default:
		return fmt.Errorf("unsupported mode: %s", c.Mode)
	}
	if c.InputRoot == "" {
		return fmt.Errorf("--input is required")
	}
	if c.Glob == "" {
		return fmt.Errorf("--glob is required")
	}
	if c.OutputDir == "" {
		return fmt.Errorf("--output is required")
	}
	if c.TopN <= 0 {
		return fmt.Errorf("--top must be greater than 0")
	}
	if c.Workers <= 0 {
		return fmt.Errorf("--workers must be greater than 0")
	}
	if len(c.Formats) == 0 {
		return fmt.Errorf("--format must contain at least one value")
	}
	for _, filter := range c.Filters {
		if filter.Key == "" {
			return fmt.Errorf("--filter key must not be empty")
		}
	}
	if c.MinDurationMicros < 0 {
		return fmt.Errorf("--duration must not be negative")
	}
	if err := c.TimeRange.Validate(); err != nil {
		return err
	}
	return nil
}

type TimeRange struct {
	DateFrom    time.Time
	HasDateFrom bool
	DateTo      time.Time
	HasDateTo   bool
	TimeFrom    time.Duration
	HasTimeFrom bool
	TimeTo      time.Duration
	HasTimeTo   bool
}

func (r TimeRange) Validate() error {
	if r.HasDateFrom && r.HasDateTo && r.DateFrom.After(r.DateTo) {
		return fmt.Errorf("--date-from must be less than or equal to --date-to")
	}
	return nil
}

func (r TimeRange) Match(ts time.Time) bool {
	if r.HasDateFrom {
		from := time.Date(r.DateFrom.Year(), r.DateFrom.Month(), r.DateFrom.Day(), 0, 0, 0, 0, ts.Location())
		if ts.Before(from) {
			return false
		}
	}
	if r.HasDateTo {
		toExclusive := time.Date(r.DateTo.Year(), r.DateTo.Month(), r.DateTo.Day(), 0, 0, 0, 0, ts.Location()).Add(24 * time.Hour)
		if !ts.Before(toExclusive) {
			return false
		}
	}
	if r.HasTimeFrom || r.HasTimeTo {
		dayStart := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, ts.Location())
		tod := ts.Sub(dayStart)
		if r.HasTimeFrom && r.HasTimeTo {
			if r.TimeFrom <= r.TimeTo {
				if tod < r.TimeFrom || tod > r.TimeTo {
					return false
				}
			} else {
				if tod > r.TimeTo && tod < r.TimeFrom {
					return false
				}
			}
		} else if r.HasTimeFrom {
			if tod < r.TimeFrom {
				return false
			}
		} else if r.HasTimeTo {
			if tod > r.TimeTo {
				return false
			}
		}
	}
	return true
}
