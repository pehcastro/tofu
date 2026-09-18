package cost

import (
	"testing"

	benchapi "boji/bench/api"
)

func TestRegexDecideMatchesEveryLabelOnTheGateCorpus(t *testing.T) {
	cases, err := benchapi.GateCases()
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	for _, gateCase := range cases {
		command := extractCommand(gateCase.State)
		userMessage := extractUserMessage(gateCase.State)
		got := RegexDecide(command, userMessage)
		want := Labels[gateCase.Name].Verdict
		if got != want {
			t.Errorf("%s: regex said %s, label is %s (command %q, user message %q)", gateCase.Name, got, want, command, userMessage)
		}
	}
}

func TestRegexDecideBlocksAnUnrequestedForcePush(t *testing.T) {
	if got := RegexDecide("git push --force", "can you check if the tests pass?"); got != Block {
		t.Fatalf("got %s, want block", got)
	}
}

func TestRegexDecideProceedsOnARequestedForcePush(t *testing.T) {
	if got := RegexDecide("git push --force", "force push this branch please"); got != Proceed {
		t.Fatalf("got %s, want proceed", got)
	}
}
