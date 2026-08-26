package techlog

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ParseFile streams all events from a single hourly technological-log file.
func ParseFile(path string, emit func(Event) error) (ParseStats, error) {
	return ParseFileContext(context.Background(), path, emit)
}

// ParseFileContext streams all events from a single hourly technological-log
// file and stops between records when ctx is cancelled.
func ParseFileContext(ctx context.Context, path string, emit func(Event) error) (ParseStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ParseStats{}, err
	}
	hour, err := ParseFileHour(path)
	if err != nil {
		return ParseStats{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ParseStats{}, err
	}
	defer f.Close()
	return ParseContext(ctx, f, path, hour, emit)
}

// Parse streams events from r. Continuation lines are attached to the previous
// header and retained in Event.Raw.
func Parse(r io.Reader, source string, fileHour time.Time, emit func(Event) error) (ParseStats, error) {
	return ParseContext(context.Background(), r, source, fileHour, emit)
}

// ParseContext streams events from r and checks ctx between physical lines and
// before invoking emit. Parse remains available for callers without a context.
func ParseContext(ctx context.Context, r io.Reader, source string, fileHour time.Time, emit func(Event) error) (ParseStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := bufio.NewReaderSize(r, 4*1024*1024)
	var stats ParseStats
	var current strings.Builder
	hasCurrent := false

	finish := func() error {
		if !hasCurrent {
			return nil
		}
		event, ok := parseEvent(current.String(), source, fileHour)
		current.Reset()
		hasCurrent = false
		if !ok {
			stats.MalformedHeaders++
			return nil
		}
		stats.Events++
		if emit != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			return emit(event)
		}
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		line, readErr := reader.ReadBytes('\n')
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if len(line) > 0 {
			stats.BytesRead += int64(len(line))
			stats.LinesRead++
			line = bytes.TrimRight(line, "\r\n")
			line = bytes.TrimPrefix(line, []byte{0xEF, 0xBB, 0xBF})
			if isEventStart(line) {
				if err := finish(); err != nil {
					return stats, err
				}
				current.Write(line)
				hasCurrent = true
			} else if hasCurrent {
				current.WriteByte('\n')
				current.Write(line)
			} else if len(bytes.TrimSpace(line)) > 0 {
				stats.OrphanLines++
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return stats, readErr
		}
	}
	if err := finish(); err != nil {
		return stats, err
	}
	return stats, nil
}

// ParseFileHour extracts YYMMDDHH from the beginning of a log filename.
func ParseFileHour(path string) (time.Time, error) {
	base := filepath.Base(path)
	if len(base) < 8 {
		return time.Time{}, fmt.Errorf("invalid log filename %q", base)
	}
	stamp := base[:8]
	parts := make([]int, 4)
	for i := range parts {
		value, err := strconv.Atoi(stamp[i*2 : i*2+2])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid log filename %q: %w", base, err)
		}
		parts[i] = value
	}
	result := time.Date(2000+parts[0], time.Month(parts[1]), parts[2], parts[3], 0, 0, 0, time.Local)
	if result.Year() != 2000+parts[0] || int(result.Month()) != parts[1] || result.Day() != parts[2] || result.Hour() != parts[3] {
		return time.Time{}, fmt.Errorf("invalid date or hour in log filename %q", base)
	}
	return result, nil
}

func isEventStart(line []byte) bool {
	return len(line) >= 12 && isDigit(line[0]) && isDigit(line[1]) && line[2] == ':' &&
		isDigit(line[3]) && isDigit(line[4]) && line[5] == '.'
}

func parseEvent(raw, source string, fileHour time.Time) (Event, bool) {
	firstLine := raw
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	comma1 := strings.IndexByte(firstLine, ',')
	if comma1 <= 0 {
		return Event{}, false
	}
	prefix := firstLine[:comma1]
	minus := strings.LastIndexByte(prefix, '-')
	if minus <= 0 || minus+1 >= len(prefix) {
		return Event{}, false
	}
	timestamp, ok := parseTimestamp(fileHour, prefix[:minus])
	if !ok {
		return Event{}, false
	}
	duration, err := strconv.ParseInt(prefix[minus+1:], 10, 64)
	if err != nil || duration < 0 {
		return Event{}, false
	}

	// Structural header fields must be read from the first physical line only.
	// A continuation can legitimately contain commas, and must not turn a valid
	// level without properties into a malformed header.
	rest := firstLine[comma1+1:]
	comma2 := strings.IndexByte(rest, ',')
	if comma2 <= 0 {
		return Event{}, false
	}
	name := strings.TrimSpace(rest[:comma2])
	afterName := rest[comma2+1:]
	comma3 := strings.IndexByte(afterName, ',')
	levelText := afterName
	properties := ""
	if comma3 >= 0 {
		levelText = afterName[:comma3]
		propertiesOffset := comma1 + 1 + comma2 + 1 + comma3 + 1
		properties = raw[propertiesOffset:]
	}
	level, err := strconv.Atoi(strings.TrimSpace(levelText))
	if err != nil || name == "" {
		return Event{}, false
	}

	return Event{Timestamp: timestamp, DurationMicros: duration, Name: name, Level: level,
		Fields: parseFields(properties), Raw: raw, Source: source}, true
}

func parseTimestamp(hour time.Time, value string) (time.Time, bool) {
	if len(value) < len("00:00.000000") || value[2] != ':' || value[5] != '.' {
		return time.Time{}, false
	}
	minute, err1 := strconv.Atoi(value[0:2])
	second, err2 := strconv.Atoi(value[3:5])
	microText := value[6:]
	micros, err3 := strconv.Atoi(microText)
	if err1 != nil || err2 != nil || err3 != nil || minute < 0 || minute > 59 || second < 0 || second > 59 || micros < 0 || len(microText) > 6 {
		return time.Time{}, false
	}
	for len(microText) < 6 {
		micros *= 10
		microText += "0"
	}
	return time.Date(hour.Year(), hour.Month(), hour.Day(), hour.Hour(), minute, second, micros*1000, hour.Location()), true
}

func parseFields(input string) map[string]string {
	fields := make(map[string]string)
	for _, token := range splitProperties(input) {
		idx := strings.IndexByte(token, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(token[:idx])
		value := strings.TrimSpace(token[idx+1:])
		if key == "" {
			continue
		}
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		fields[key] = value
	}
	return fields
}

func splitProperties(input string) []string {
	var result []string
	start := 0
	var quote byte
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '\'', '"':
			if quote == 0 {
				quote = input[i]
			} else if quote == input[i] {
				if i+1 < len(input) && input[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
		case ',':
			if quote == 0 {
				result = append(result, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	result = append(result, strings.TrimSpace(input[start:]))
	return result
}

func isDigit(value byte) bool { return value >= '0' && value <= '9' }
