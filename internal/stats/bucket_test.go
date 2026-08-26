package stats

import (
	"reflect"
	"testing"
	"time"
)

func TestNewBucketerRejectsInvalidInterval(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Minute} {
		if _, err := NewBucketer(interval); err == nil {
			t.Fatalf("NewBucketer(%s) error = nil", interval)
		}
	}
}

func TestBucketerBucketPreservesLocation(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	bucketer, err := NewBucketer(15 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 8, 23, 10, 7, 42, 0, location)

	got := bucketer.Bucket(instant)
	wantStart := time.Date(2026, 8, 23, 10, 0, 0, 0, location)
	wantEnd := time.Date(2026, 8, 23, 10, 15, 0, 0, location)
	if !got.Start.Equal(wantStart) || !got.End.Equal(wantEnd) {
		t.Fatalf("Bucket() = %#v, want [%s, %s)", got, wantStart, wantEnd)
	}
	if got.Start.Location() != location {
		t.Fatalf("location = %v, want %v", got.Start.Location(), location)
	}
	if !got.Contains(instant) || got.Contains(wantEnd) || got.Contains(wantStart.Add(-time.Nanosecond)) {
		t.Fatal("Bucket.Contains has incorrect half-open boundaries")
	}
}

func TestBucketerRange(t *testing.T) {
	bucketer, err := NewBucketer(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC)
	to := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	got := bucketer.Range(from, to)
	want := []Bucket{
		{Start: time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC)},
		{Start: time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range() = %#v, want %#v", got, want)
	}
	if got := bucketer.Range(to, from); got != nil {
		t.Fatalf("descending Range = %#v, want nil", got)
	}
}
