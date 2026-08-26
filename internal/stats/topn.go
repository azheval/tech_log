package stats

import "sort"

// TopN retains at most a fixed number of best values. better must return true
// when its first argument should be placed before its second argument in the
// result. Ties retain insertion order, making Items deterministic for the same
// input sequence.
//
// The zero value has a limit of zero and therefore retains no values; construct
// usable instances with NewTopN.
type TopN[T any] struct {
	limit  int
	better func(a, b T) bool
	items  []topNItem[T]
	next   uint64
}

type topNItem[T any] struct {
	value T
	order uint64
}

// NewTopN constructs a bounded ranking. A negative limit is treated as zero.
// better must define a consistent ordering.
func NewTopN[T any](limit int, better func(a, b T) bool) *TopN[T] {
	if limit < 0 {
		limit = 0
	}
	return &TopN[T]{limit: limit, better: better}
}

// Add considers value for inclusion in the ranking. It reports whether the
// value is retained. Add panics if NewTopN received a nil comparison function.
func (t *TopN[T]) Add(value T) bool {
	if t.limit == 0 {
		return false
	}
	if t.better == nil {
		panic("stats.TopN: nil better function")
	}

	item := topNItem[T]{value: value, order: t.next}
	t.next++
	if len(t.items) < t.limit {
		t.items = append(t.items, item)
		return true
	}

	worst := 0
	for i := 1; i < len(t.items); i++ {
		if t.less(t.items[worst], t.items[i]) {
			worst = i
		}
	}
	if !t.less(item, t.items[worst]) {
		return false
	}
	t.items[worst] = item
	return true
}

// Len reports how many values are currently retained.
func (t *TopN[T]) Len() int { return len(t.items) }

// Items returns a sorted copy of retained values. The returned slice can be
// modified by the caller without affecting TopN.
func (t *TopN[T]) Items() []T {
	ordered := append([]topNItem[T](nil), t.items...)
	sort.SliceStable(ordered, func(i, j int) bool { return t.less(ordered[i], ordered[j]) })
	result := make([]T, len(ordered))
	for i, item := range ordered {
		result[i] = item.value
	}
	return result
}

func (t *TopN[T]) less(a, b topNItem[T]) bool {
	if t.better(a.value, b.value) {
		return true
	}
	if t.better(b.value, a.value) {
		return false
	}
	return a.order < b.order
}
