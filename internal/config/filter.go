package config

import (
	"fmt"
	"strings"
	"time"
)

type Filter struct {
	Key   string
	Value string
}

func ParseFilter(raw string) (Filter, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), "=", 2)
	if len(parts) != 2 {
		return Filter{}, fmt.Errorf("invalid --filter %q, expected key=value", raw)
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return Filter{}, fmt.Errorf("invalid --filter %q, key must not be empty", raw)
	}
	return Filter{Key: key, Value: value}, nil
}

func ParseMinDurationMicros(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if !strings.ContainsAny(raw, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		raw += "s"
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid --duration %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("--duration must not be negative")
	}
	return d.Microseconds(), nil
}

func ParseDate(raw string) (time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid date %q, expected YYYY-MM-DD", raw)
	}
	return t, true, nil
}

func ParseTimeOfDay(raw string) (time.Duration, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	formats := []string{"15:04", "15:04:05", "15:04:05.000000"}
	for _, format := range formats {
		if t, err := time.Parse(format, raw); err == nil {
			d := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
			return d, true, nil
		}
	}
	return 0, false, fmt.Errorf("invalid time %q, expected HH:MM or HH:MM:SS", raw)
}

func MatchAllFilters(event string, filters []Filter) bool {
	for _, filter := range filters {
		value, ok := ExtractFieldValue(event, filter.Key)
		if !ok || value != filter.Value {
			return false
		}
	}
	return true
}

func ExtractFieldValue(event string, key string) (string, bool) {
	needle := key + "="
	for i := 0; i < len(event); i++ {
		idx := strings.Index(event[i:], needle)
		if idx < 0 {
			return "", false
		}
		idx += i
		if idx > 0 && event[idx-1] != ',' && event[idx-1] != ' ' {
			i = idx + len(needle)
			continue
		}
		start := idx + len(needle)
		if start >= len(event) {
			return "", true
		}
		quote := byte(0)
		if event[start] == '\'' || event[start] == '"' {
			quote = event[start]
			start++
		}
		var b strings.Builder
		for j := start; j < len(event); j++ {
			ch := event[j]
			if quote != 0 {
				if ch == quote {
					return strings.TrimSpace(b.String()), true
				}
				b.WriteByte(ch)
				continue
			}
			if ch == ',' {
				return strings.TrimSpace(b.String()), true
			}
			b.WriteByte(ch)
		}
		return strings.TrimSpace(b.String()), true
	}
	return "", false
}
