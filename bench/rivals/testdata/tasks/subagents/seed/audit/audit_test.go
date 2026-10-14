package audit

import (
	"strings"
	"testing"
	"time"
)

func TestFlushThenExport(t *testing.T) {
	sink := &MemorySink{}
	r := NewRecorder(sink)
	r.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	r.Note("ops", "close", "store-9")
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := Export(&b, sink.Events); err != nil {
		t.Fatal(err)
	}
	want := "at,actor,action,target\n2026-01-02T03:04:05Z,ops,close,store-9\n"
	if b.String() != want {
		t.Fatalf("got %q", b.String())
	}
}
