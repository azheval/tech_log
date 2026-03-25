package output

import (
	"strings"

	"techlog-stat/internal/config"
)

func RenderErrors(lines []string) []byte {
	if len(lines) == 0 {
		return []byte{}
	}

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func reportTitle(report string) string {
	switch report {
	case config.ReportSDBLContext:
		return "SDBL by Context"
	case config.ReportCALLContext:
		return "CALL by Context"
	case config.ReportDBMSSQLContext:
		return "DBMSSQL by Context"
	case config.ReportPostgresContext:
		return "DBPOSTGRS by Context"
	case config.ReportFileDBContext:
		return "DBV8DBEng by Context"
	case config.ReportLockContext:
		return "Locks by Context"
	case config.ReportTimeoutContext:
		return "TTIMEOUT by Context"
	case config.ReportDeadlockContext:
		return "TDEADLOCK by Context"
	case config.ReportErrorDescr:
		return "Errors by Description"
	default:
		return report
	}
}
