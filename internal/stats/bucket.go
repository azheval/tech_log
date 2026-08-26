package stats

import (
	"fmt"
	"time"
)

// Bucket is a half-open time interval [Start, End).
type Bucket struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether instant falls in the bucket.
func (b Bucket) Contains(instant time.Time) bool {
	return !instant.Before(b.Start) && instant.Before(b.End)
}

// Bucketer assigns instants to fixed-duration buckets. Bucket boundaries are
// aligned to the Unix epoch and displayed in the instant's original location.
type Bucketer struct {
	interval time.Duration
}

// NewBucketer validates interval and creates a bucketer.
func NewBucketer(interval time.Duration) (Bucketer, error) {
	if interval <= 0 {
		return Bucketer{}, fmt.Errorf("bucket interval must be positive: %s", interval)
	}
	return Bucketer{interval: interval}, nil
}

// Interval returns the configured bucket duration.
func (b Bucketer) Interval() time.Duration { return b.interval }

// Start returns the beginning of the bucket containing instant.
func (b Bucketer) Start(instant time.Time) time.Time {
	if b.interval <= 0 {
		panic("stats.Bucketer: zero interval")
	}
	nanos := instant.UnixNano()
	step := int64(b.interval)
	start := nanos - nanos%step
	if nanos < 0 && nanos%step != 0 {
		start -= step
	}
	return time.Unix(0, start).In(instant.Location())
}

// Bucket returns the half-open bucket containing instant.
func (b Bucketer) Bucket(instant time.Time) Bucket {
	start := b.Start(instant)
	return Bucket{Start: start, End: start.Add(b.interval)}
}

// Range returns buckets that intersect [from, to), in chronological order.
// It returns nil when to is not after from.
func (b Bucketer) Range(from, to time.Time) []Bucket {
	if !to.After(from) {
		return nil
	}
	result := make([]Bucket, 0)
	for start := b.Start(from); start.Before(to); start = start.Add(b.interval) {
		result = append(result, Bucket{Start: start, End: start.Add(b.interval)})
	}
	return result
}
