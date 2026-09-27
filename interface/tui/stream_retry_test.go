package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestARetriedStreamShowsItsAnswerOnce(t *testing.T) {
	retried := proseApp(t, 24)
	retried.Update(Event{Kind: EventTextDelta, Text: "hel"})
	retried.Update(Event{Kind: EventStreamReset})
	retried.Update(Event{Kind: EventTextDelta, Text: "hello"})

	once := proseApp(t, 24)
	once.Update(Event{Kind: EventTextDelta, Text: "hello"})

	if got, want := retried.View().Content, once.View().Content; got != want {
		t.Fatalf("a retried stream still shows its first attempt\n--- retried ---\n%s\n--- once ---\n%s", ansi.Strip(got), ansi.Strip(want))
	}
}
