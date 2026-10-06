package session

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/fixture"
	"tofu/internal/golden"
)

func TestTheAskBlockCarriesTheNumbersTheParagraphCannotFit(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Start()
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "bash", Body: "git push --force origin main"})
	model.Decide(Decision{
		Tool:    "bash",
		Verdict: Ask,
		Answers: []Answer{
			{Question: "approval", Value: 0.83, Max: 1},
			{Question: "risk", Value: 2, Max: 3},
		},
		Reason: Reason{
			Question:  "risk",
			Limit:     "risk_ask_at",
			Levels:    fixture.RiskLevels(),
			Threshold: 1.5,
			Value:     2,
		},
	})
	model.Await()

	frame := model.View()
	plain := ansi.Strip(frame)
	ask := ""
	for _, line := range strings.Split(plain, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), askMarker) {
			ask = line
		}
	}
	if !strings.Contains(ask, "risk 2.00 over 1.50") {
		t.Fatalf("the ask block does not say what tripped the gate: %q", ask)
	}
	if strings.Contains(ask, "…") {
		t.Fatalf("the ask block truncates at eighty columns: %q", ask)
	}
	if _, body, _ := model.Expansion("c1", 76); !strings.Contains(ansi.Strip(strings.Join(body, "\n")), "risk is hard to undo or reaches outside the workspace") {
		t.Fatalf("the expanded call lost the worded sentence\n%s", ansi.Strip(strings.Join(body, "\n")))
	}
	golden.Assert(t, "ask-block-80x24.golden", frame)
}

func TestAnAskWithNoThresholdFallsBackToWhatTheGateSaid(t *testing.T) {
	failed := Decision{Verdict: Ask, Failure: "the judge timed out"}
	if got := tripped(failed); got != "the gate could not answer, so the call is an ask: the judge timed out" {
		t.Fatalf("a failed gate reads %q in the ask block", got)
	}
	dead := Decision{
		Verdict: Ask,
		Reason:  Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 1.5, DeadBand: true},
	}
	if got := tripped(dead); got != "risk 1.50 in the dead band at 1.50" {
		t.Fatalf("the dead band reads %q in the ask block", got)
	}
}

func TestTheSessionFormatsTheNumbersItWasHanded(t *testing.T) {
	view := New(fixed(), counted(new(int)))
	view.SetSize(100, 20)
	view.Append(Entry{Kind: Tool, ID: "w1", Head: "write", Body: "README.md"})
	view.Decide(Decision{
		Tool:    "write",
		Verdict: Ask,
		Answers: []Answer{
			{Question: "approval", Value: 0.75, Max: 1},
			{Question: "from_untrusted", Value: 0.02, Max: 1},
			{Question: "risk", Value: 2, Max: 3},
			{Question: "user_requested", Value: 0.11, Max: 1},
		},
		Reason: Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 2},
	})
	head, body, found := view.Expansion("w1", 100)
	if !found {
		t.Fatalf("the call has no expansion\n%s", view.View())
	}
	content := head + "\n" + strings.Join(body, "\n")
	for _, want := range []string{"ask", "risk", "2.00", "approval", "0.75", "▓", "risk 2.00 is over risk_ask_at 1.50"} {
		if !strings.Contains(content, want) {
			t.Errorf("the session view does not render %q\n%s", want, content)
		}
	}
}
