package session

import (
	"strings"
	"testing"
)

func spoken(source string, width int) []string { return strings.Split(strings.TrimSpace(source), "\n") }

func withAChildThatRead(t *testing.T, parentCalled string) Model {
	t.Helper()
	model := New(fixed(), spoken)
	model.SetSize(80, 24)
	model.Start()
	if parentCalled != "" {
		model.Append(Entry{Kind: Tool, ID: "p1", Head: "read", Body: parentCalled})
		model.Finish("p1", Result{Status: "1 line, 20 bytes"})
	}
	model.Append(Entry{Kind: Tool, ID: "s1", Head: "spawn", Body: "read note.txt and say what it holds"})
	model.Stream("reading the note")
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "read", Body: "note.txt"})
	model.Decide(Decision{Tool: "read", Verdict: Ask})
	return model
}

func TestAChildsToolRowIsDrawnTheSameWayWhateverTheParentCalledBefore(t *testing.T) {
	alone := withAChildThatRead(t, "")
	after := withAChildThatRead(t, "other.txt")
	if alone.FoldedOutOfChat("c1") {
		t.Fatal("a judged call is folded out of the chat even when nothing came before it")
	}
	if after.FoldedOutOfChat("c1") {
		t.Errorf("the child's judged call is folded out of the chat because the parent called read earlier:\n%s", after.View())
	}
	if !after.FoldedOutOfChat("p1") {
		t.Errorf("the parent's finished call took the decision meant for the child:\n%s", after.View())
	}
	if !strings.Contains(after.View(), "read note.txt") {
		t.Errorf("the child's row is not drawn in full:\n%s", after.View())
	}
}

func TestAChildsLastMessageAndTheParentsFirstAreTwoMessages(t *testing.T) {
	model := New(fixed(), spoken)
	model.SetSize(80, 24)
	model.Start()
	model.Append(Entry{Kind: Tool, ID: "s1", Head: "spawn", Body: "read note.txt and say what it holds"})
	model.Stream("the note says a note")
	model.Finish("s1", Result{Status: "3 lines, 199 bytes"})
	model.Stream("the child read it and the note says a note")
	answer, found := model.LastAnswer()
	if !found {
		t.Fatal("no message reached the transcript at all")
	}
	if answer != "the child read it and the note says a note" {
		t.Errorf("the child's last message and the parent's first are one message: %q", answer)
	}
	if said := strings.Count(model.View(), assistantMark); said != 2 {
		t.Errorf("the transcript draws %d messages, want the child's and the parent's:\n%s", said, model.View())
	}
}
