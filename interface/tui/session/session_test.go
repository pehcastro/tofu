package session

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const frames = 20

func counted(calls *int) Prose {
	return func(source string, width int) []string {
		*calls++
		return []string{"prose at " + strconv.Itoa(width) + ": " + strings.TrimSpace(source)}
	}
}

func fixed() func() time.Time {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func TestProseIsRenderedOncePerWidth(t *testing.T) {
	calls := 0
	model := New(fixed(), counted(&calls))
	model.SetSize(80, 24)
	model.Append(Entry{Kind: Assistant, Body: "**Boji** reads `toolgate.go`"})
	if calls != 1 {
		t.Fatalf("appending one message called the renderer %d times, want 1", calls)
	}
	for range frames {
		model.View()
	}
	if calls != 1 {
		t.Errorf("%d frames called the renderer %d times, want 1", frames, calls)
	}
	model.SetSize(80, 24)
	model.View()
	if calls != 1 {
		t.Errorf("a resize to the same width called the renderer %d times, want 1", calls)
	}
	model.SetSize(100, 24)
	model.View()
	if calls != 2 {
		t.Errorf("a resize to a new width called the renderer %d times, want 2", calls)
	}
	if !strings.Contains(model.View(), "prose at 98") {
		t.Errorf("the frame does not carry the message rendered at the new width:\n%s", model.View())
	}
}

func TestStreamingProseStaysPlainUntilItStops(t *testing.T) {
	calls := 0
	model := New(fixed(), counted(&calls))
	model.SetSize(80, 24)
	model.Stream("**Boji** ")
	model.Stream("reads `toolgate.go`")
	streaming := model.View()
	if calls != 0 {
		t.Fatalf("a streaming message called the renderer %d times, want 0", calls)
	}
	if !strings.Contains(streaming, "**Boji** reads `toolgate.go`") {
		t.Errorf("a streaming message is not shown as plain text:\n%s", streaming)
	}
	model.Stop()
	complete := model.View()
	if calls != 1 {
		t.Errorf("a message that stopped called the renderer %d times, want 1", calls)
	}
	if streaming == complete {
		t.Error("the frame is unchanged once the message is complete")
	}
	if !strings.Contains(complete, "prose at 78") {
		t.Errorf("the complete message is not rendered as markdown:\n%s", complete)
	}
}
