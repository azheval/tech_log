// Package stats contains small, dependency-free primitives for streaming
// aggregations used by technological-log analyses.
package stats

import (
	"encoding/json"
	"math"
	"sort"
)

// Summary is an immutable view of NumericStats.
//
// For an empty series, Min, Max, Mean and all percentiles are NaN. This makes
// an absent value distinguishable from a measured value of zero.
type Summary struct {
	Count uint64
	Sum   float64
	Min   float64
	Max   float64
	Mean  float64
	P50   float64
	P90   float64
	P95   float64
	P99   float64
}

// MarshalJSON preserves the established field names while representing
// unavailable or non-finite measurements as null. NumericStats deliberately
// uses NaN for an empty series, but encoding/json rejects NaN and infinities.
func (s Summary) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Count uint64
		Sum   any
		Min   any
		Max   any
		Mean  any
		P50   any
		P90   any
		P95   any
		P99   any
	}{
		Count: s.Count,
		Sum:   finiteJSONNumber(s.Sum),
		Min:   finiteJSONNumber(s.Min),
		Max:   finiteJSONNumber(s.Max),
		Mean:  finiteJSONNumber(s.Mean),
		P50:   finiteJSONNumber(s.P50),
		P90:   finiteJSONNumber(s.P90),
		P95:   finiteJSONNumber(s.P95),
		P99:   finiteJSONNumber(s.P99),
	})
}

func finiteJSONNumber(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return value
}

// NumericStats incrementally accumulates numerical values. It keeps the
// individual values only to calculate exact percentiles when Finalize is
// called. Add is O(1); Finalize is O(n log n) after new values have arrived.
//
// A zero NumericStats is ready for use. NaN values are rejected; infinities
// are accepted and follow normal IEEE-754 arithmetic.
type NumericStats struct {
	count  uint64
	sum    float64
	min    float64
	max    float64
	values []float64

	dirty        bool
	finalizedSet bool
	finalized    Summary
}

// Add records value. It returns false when value is NaN; all other values are
// recorded and return true.
func (s *NumericStats) Add(value float64) bool {
	if math.IsNaN(value) {
		return false
	}

	if s.count == 0 {
		s.min = value
		s.max = value
	} else {
		if value < s.min {
			s.min = value
		}
		if value > s.max {
			s.max = value
		}
	}

	s.count++
	s.sum += value
	s.values = append(s.values, value)
	s.dirty = true
	return true
}

// Count returns the number of accepted values without requiring Finalize.
func (s *NumericStats) Count() uint64 { return s.count }

// Finalize returns aggregate values and exact percentiles. Percentiles use
// linear interpolation between adjacent values in sorted order: for percentile
// p, the rank is p/100*(n-1). Calling Finalize repeatedly without Add does not
// re-sort the values.
func (s *NumericStats) Finalize() Summary {
	if s.finalizedSet && !s.dirty {
		return s.finalized
	}

	if s.count == 0 {
		nan := math.NaN()
		s.finalized = Summary{Min: nan, Max: nan, Mean: nan, P50: nan, P90: nan, P95: nan, P99: nan}
		s.dirty = false
		s.finalizedSet = true
		return s.finalized
	}

	values := append([]float64(nil), s.values...)
	sort.Float64s(values)
	s.finalized = Summary{
		Count: s.count,
		Sum:   s.sum,
		Min:   s.min,
		Max:   s.max,
		Mean:  s.sum / float64(s.count),
		P50:   percentile(values, 0.50),
		P90:   percentile(values, 0.90),
		P95:   percentile(values, 0.95),
		P99:   percentile(values, 0.99),
	}
	s.dirty = false
	s.finalizedSet = true
	return s.finalized
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	part := rank - float64(lo)
	return sorted[lo] + part*(sorted[hi]-sorted[lo])
}
