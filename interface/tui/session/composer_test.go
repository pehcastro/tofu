package session

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTypingAndDeletingLeavesThePlaceholderWholeOrNothing(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Focus()

	pristine := model.Cursor()
	if pristine == nil || pristine.Shape != tea.CursorBar {
		t.Fatalf("the pristine cursor shape is %v, want a bar that never covers a placeholder character", pristine)
	}
	model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if value := model.Value(); value != "" {
		t.Fatalf("the composer reads %q after typing and deleting, want empty", value)
	}
	after := model.Cursor()
	if after == nil || after.Shape != tea.CursorBar {
		t.Fatalf("the cursor shape after delete is %v, want a bar that never covers a placeholder character", after)
	}
}

func TestThePlaceholderIsTheSameWhateverTheClockReads(t *testing.T) {
	opened := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	seen := map[string]time.Time{}
	for step := range 3 {
		at := opened.Add(time.Duration(step))
		seen[New(func() time.Time { return at }, counted(new(int))).composer.Placeholder] = at
	}
	if len(seen) != 1 {
		t.Fatalf("three clocks a nanosecond apart drew %d placeholders, so the bottom of the screen depends on the clock: %v", len(seen), seen)
	}
}

func TestTheGreetingLeavesOnceTheFirstMessageIsSent(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Append(Entry{Kind: Note, Body: "type a task and press enter. tofu works in scratch"})
	before := model.View()
	assertGolden(t, "greeting-before-send-80x24.golden", before)
	if !strings.Contains(before, "type a task and press enter") {
		t.Fatalf("the empty session lost its greeting:\n%s", before)
	}

	model.Append(Entry{Kind: User, Body: "hey tofu, can you explain this repository to me?"})
	model.Start()
	after := model.View()
	assertGolden(t, "greeting-after-send-80x24.golden", after)
	if strings.Contains(after, "type a task and press enter") {
		t.Fatalf("the greeting stayed after the first message:\n%s", after)
	}
}

func TestTheActivityClockNeverRestartsAcrossPhasesOrToolCalls(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return at }
	model := New(now, counted(new(int)))
	model.SetSize(80, 24)
	model.Start()

	at = at.Add(5 * time.Second)
	requesting := model.phaseSince()
	if requesting != 5*time.Second {
		t.Fatalf("requesting reads %s, want 5s", requesting)
	}

	model.Requesting()
	at = at.Add(6 * time.Second)
	thinking := model.phaseSince()
	if thinking < requesting {
		t.Fatalf("the clock went from %s to %s across a phase change", requesting, thinking)
	}
	model.Returned()

	last := thinking
	for step := range 3 {
		id := strconv.Itoa(step)
		model.Append(Entry{Kind: Tool, ID: id, Head: "read"})
		at = at.Add(2 * time.Second)
		during := model.phaseSince()
		if during < last {
			t.Fatalf("the clock went from %s to %s across tool call %d", last, during, step)
		}
		last = during
		model.Finish(id, Result{Status: "ok"})
	}

	if last != at.Sub(model.entered) {
		t.Fatalf("the activity clock reads %s, want the same start as the header: %s", last, at.Sub(model.entered))
	}
}

func TestUpAndDownNeverMoveTheTranscript(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	for step := range 40 {
		model.Append(Entry{Kind: Note, Body: "line " + strconv.Itoa(step)})
	}
	before := model.View()
	if _, moved := model.Scroll("up"); moved {
		t.Fatalf("up scrolled the transcript instead of belonging to the composer")
	}
	if _, moved := model.Scroll("down"); moved {
		t.Fatalf("down scrolled the transcript instead of belonging to the composer")
	}
	if after := model.View(); after != before {
		t.Fatalf("the transcript moved after up/down with an empty composer")
	}
}
