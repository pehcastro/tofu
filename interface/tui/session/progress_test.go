package session

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const generousRows = 1000

func started() Model {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Start()
	return model
}

func transcript(model Model) string {
	return ansi.Strip(strings.Join(model.linesFrom(anchor{}, generousRows), "\n"))
}

func TestTheRunningBlockIsOneProgressLineWithNoSummaryAcrossTwelveCalls(t *testing.T) {
	model := started()
	for step := range 12 {
		id := strconv.Itoa(step)
		model.Append(Entry{Kind: Tool, ID: id, Head: "read", Body: "file " + id})
		drawn := transcript(model)
		if lines := strings.Count(drawn, "\n") + 1; lines != 1 {
			t.Fatalf("call %d: the running block carries %d lines, want only its progress line\n%s", step, lines, drawn)
		}
		if strings.Contains(drawn, ") tools") {
			t.Fatalf("call %d: the running block summarises a turn that has not ended: %q", step, drawn)
		}
		model.Finish(id, Result{Status: "ok"})
	}
}

func TestTheRunningBlockCarriesTheIDOfTheCallThatIsRunning(t *testing.T) {
	model := started()
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "read", Body: "internal/turn/loop.go"})
	if drawn := transcript(model); !strings.Contains(drawn, "#c1") {
		t.Fatalf("the running line carries no id that reaches work: %q", drawn)
	}
}

func TestTheFoldSummaryReturnsWhenTheTurnEnds(t *testing.T) {
	model := started()
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "read", Body: "internal/turn/loop.go"})
	model.Finish("c1", Result{Status: "ok"})
	model.Stop()
	if drawn := transcript(model); !strings.Contains(drawn, "(1) tools") {
		t.Fatalf("a finished turn lost its fold line\n%s", drawn)
	}
}

func TestATurnThatIsRunningDoesNotHideThePreviousTurnsFoldLine(t *testing.T) {
	model := started()
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "read", Body: "internal/turn/loop.go"})
	model.Finish("c1", Result{Status: "ok"})
	model.Stop()
	model.Append(Entry{Kind: User, Body: "and the second question"})
	model.Start()
	model.Append(Entry{Kind: Tool, ID: "c2", Head: "read", Body: "internal/turn/step.go"})
	drawn := transcript(model)
	if !strings.Contains(drawn, "(1) tools") {
		t.Fatalf("the first turn's fold line vanished when the second turn started\n%s", drawn)
	}
	if strings.Contains(drawn, "(2) tools") {
		t.Fatalf("the running turn drew a fold line\n%s", drawn)
	}
}
