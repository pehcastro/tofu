package search

import (
	"reflect"
	"testing"
)

type staticLoader []Document

func (s staticLoader) Load() ([]Document, error) { return s, nil }

func TestAllIntersects(t *testing.T) {
	ix := NewIndex(staticLoader{{ID: "1", Text: "Red box of tea"}, {ID: "2", Text: "red tea"}, {ID: "3", Text: "box"}})
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := ix.All("red", "tea"); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("got %v", got)
	}
}
