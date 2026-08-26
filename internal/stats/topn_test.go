package stats

import (
	"reflect"
	"testing"
)

type rankItem struct {
	name  string
	score int
}

func TestTopNRetainsBestAndSorts(t *testing.T) {
	top := NewTopN(3, func(a, b rankItem) bool { return a.score > b.score })
	for _, item := range []rankItem{{"a", 10}, {"b", 30}, {"c", 20}, {"d", 40}, {"e", 5}} {
		top.Add(item)
	}

	got := top.Items()
	want := []rankItem{{"d", 40}, {"b", 30}, {"c", 20}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Items() = %#v, want %#v", got, want)
	}
	got[0].name = "changed"
	if top.Items()[0].name != "d" {
		t.Fatal("Items returned a slice backed by TopN")
	}
}

func TestTopNTiesKeepEarliestValues(t *testing.T) {
	top := NewTopN(2, func(a, b rankItem) bool { return a.score > b.score })
	top.Add(rankItem{"first", 10})
	top.Add(rankItem{"second", 10})
	if top.Add(rankItem{"third", 10}) {
		t.Fatal("equal later value displaced an earlier value")
	}

	want := []rankItem{{"first", 10}, {"second", 10}}
	if got := top.Items(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Items() = %#v, want %#v", got, want)
	}
}

func TestTopNZeroAndNegativeLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		top := NewTopN(limit, func(a, b int) bool { return a > b })
		if top.Add(1) || top.Len() != 0 || len(top.Items()) != 0 {
			t.Fatalf("limit %d retained a value", limit)
		}
	}
}
