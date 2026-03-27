package reportutil

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func ParseFileHour(path string) (time.Time, error) {
	base := filepath.Base(path)
	if len(base) < 8 {
		return time.Time{}, fmt.Errorf("invalid log filename %q", base)
	}
	stamp := base[:8]
	yy, err := strconv.Atoi(stamp[0:2])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid log filename %q: %w", base, err)
	}
	mm, err := strconv.Atoi(stamp[2:4])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid log filename %q: %w", base, err)
	}
	dd, err := strconv.Atoi(stamp[4:6])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid log filename %q: %w", base, err)
	}
	hh, err := strconv.Atoi(stamp[6:8])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid log filename %q: %w", base, err)
	}
	return time.Date(2000+yy, time.Month(mm), dd, hh, 0, 0, 0, time.Local), nil
}

func BuildTimestamp(hour time.Time, minute, second, micros int) time.Time {
	return time.Date(hour.Year(), hour.Month(), hour.Day(), hour.Hour(), minute, second, micros*1000, hour.Location())
}

func DayKey(ts time.Time) string {
	return ts.Format("2006-01-02")
}

func HourLabel(ts time.Time) string {
	return ts.Format("2006-01-02T15:00:00")
}

func ShortenContext(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return ""
	}
	if idx := strings.Index(context, " ; "); idx >= 0 {
		return strings.TrimSpace(context[:idx])
	}
	if idx := strings.Index(context, "; "); idx >= 0 {
		return strings.TrimSpace(context[:idx])
	}
	const maxLen = 160
	runes := []rune(context)
	if len(runes) <= maxLen {
		return context
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}

func ShortenDescription(descr string) string {
	descr = strings.TrimSpace(descr)
	if descr == "" {
		return ""
	}
	const maxLen = 160
	runes := []rune(descr)
	if len(runes) <= maxLen {
		return descr
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}
