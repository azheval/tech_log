package techlog

import "time"

// Event is a normalized technological-log event. Fields contains every
// key/value property found after the event header; Raw preserves the complete
// source event, including continuation lines.
type Event struct {
	Timestamp      time.Time
	DurationMicros int64
	Name           string
	Level          int
	Fields         map[string]string
	Raw            string
	Source         string
}

// ParseStats describes input quality in addition to successful parsing.
type ParseStats struct {
	BytesRead        int64
	LinesRead        int64
	Events           int64
	MalformedHeaders int64
	OrphanLines      int64
}
