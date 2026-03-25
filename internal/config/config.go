package config

import "fmt"

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
)

type Config struct {
	Report    string
	InputRoot string
	Glob      string
	OutputDir string
	Formats   []string
	TopN      int
	Workers   int
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

func (c Config) Validate() error {
	switch c.Report {
	case ReportSDBLContext, ReportCALLContext, ReportDBMSSQLContext, ReportPostgresContext, ReportFileDBContext, ReportLockContext, ReportTimeoutContext, ReportDeadlockContext, ReportErrorDescr:
	default:
		return fmt.Errorf("unsupported report: %s", c.Report)
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
	return nil
}
