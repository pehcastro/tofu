package session

import (
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/paste"
	"tofu/internal/host"
)

func TestAnImageAfterALineAsWideAsTheComposerPlacesTheCaretAndReturns(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	full := strings.Repeat("mock", 80)[:80-composerSideCells]
	cases := []struct{ name, before, after string }{
		{"the chip on the last line", full + "\n", ""},
		{"the chip mid draft", full + "\n" + full + "\nsee ", " and fix it\nthen " + full},
		{"wide runes before the chip", strings.Repeat("界", (80-composerSideCells)/2) + "\n界", "界"},
	}
	for _, row := range cases {
		model.composer.SetValue(row.after)
		model.composer.MoveToBegin()
		model.composer.InsertString(row.before + pastingToken(1))
		model.pending = []pendingPaste{{index: 1, token: pastingToken(1)}}
		done := make(chan struct{})
		go func() {
			model.Attached(paste.Outcome{Index: 1, State: paste.Ready, Name: "mock.png", Bytes: 2048})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: placing the caret after the chip did not return in 2s", row.name)
		}
		head := row.before + host.ImageToken(1)
		if got := model.composer.Value(); got != head+row.after {
			t.Fatalf("%s: the draft reads %q, want %q", row.name, got, head+row.after)
		}
		if line, want := model.composer.Line(), strings.Count(head, "\n"); line != want {
			t.Fatalf("%s: the caret is on line %d, want %d", row.name, line, want)
		}
		if column, want := model.composer.Column(), len([]rune(head[strings.LastIndexByte(head, '\n')+1:])); column != want {
			t.Fatalf("%s: the caret is at column %d, want %d", row.name, column, want)
		}
	}
}
