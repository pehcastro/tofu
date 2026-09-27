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
	if got := failed.tripped(); got != "the gate could not answer, so the call is an ask: the judge timed out" {
		t.Fatalf("a failed gate reads %q in the ask block", got)
	}
	dead := Decision{
		Verdict: Ask,
		Reason:  Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 1.5, DeadBand: true},
	}
	if got := dead.tripped(); got != "risk 1.50 in the dead band at 1.50" {
		t.Fatalf("the dead band reads %q in the ask block", got)
	}
}
