package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/jev"
)

const recordedResponse = "../../../internal/judge/jev/testdata/adapter-cassette-response.json"

func TestTheLegendOfARecordedDecisionReachesTheThresholdLine(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(recordedResponse))
	if err != nil {
		t.Fatal(err)
	}
	response, err := jev.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	rating := response.Answers["rating"]
	decision := Decision{
		Tool:    "bash",
		Verdict: Ask,
		Answers: []Answer{{
			Question: "rating",
			Value:    rating.Score,
			Max:      float64(len(rating.Probabilities) - 1),
		}},
		Reason: Reason{
			Question:  "rating",
			Limit:     "rating_ask_at",
			Levels:    rating.Legend,
			Threshold: 1.5,
			Value:     rating.Score,
		},
	}
	const want = "rating is the reviewer offers unreserved praise and an emphatic recommendation, " +
		"so the call is asked about"
	if got := decision.sentences(); len(got) != 1 || got[0] != want {
		t.Fatalf("the threshold line is %q, want %q", got, want)
	}
}

func TestAScoreWithNoLegendKeepsItsNumberAndInventsNoWord(t *testing.T) {
	decision := Decision{
		Tool:    "bash",
		Verdict: Ask,
		Answers: []Answer{{Question: "risk", Value: 2, Max: 3}},
		Reason:  Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 2},
	}
	got := decision.sentences()
	if len(got) != 1 || got[0] != "risk 2.00 is over risk_ask_at 1.50" {
		t.Fatalf("a score with no legend reads %q", got)
	}
}

func TestALegendThatDoesNotReachTheScoreDrawsNoWord(t *testing.T) {
	short := Reason{Question: "risk", Value: 2, Levels: []string{"none", "grave"}}
	if word := short.word(); word != "" {
		t.Fatalf("a two word legend under a score of 2 drew %q", word)
	}
}

func TestTheWordIsTheLevelTheScoreRoundsTo(t *testing.T) {
	levels := []string{
		"Read-only or fully reversible inside the workspace: reading files.",
		"Changes the workspace in a way that is easy to undo: editing files.",
		"Hard to undo or reaches outside the workspace: git push.",
		"Destructive or irreversible: recursive deletes.",
	}
	for _, test := range []struct {
		value float64
		want  string
	}{
		{0, "read-only or fully reversible inside the workspace"},
		{1.4, "changes the workspace in a way that is easy to undo"},
		{1.9, "hard to undo or reaches outside the workspace"},
		{2, "hard to undo or reaches outside the workspace"},
		{3, "destructive or irreversible"},
	} {
		risk := Reason{Question: "risk", Levels: levels, Value: test.value}
		if got := risk.word(); got != test.want {
			t.Errorf("%.2f reads as %q, want %q", test.value, got, test.want)
		}
	}
}

func TestTheDeadBandAndARefusalEachSayWhatHappened(t *testing.T) {
	levels := []string{"safe", "grave"}
	dead := Decision{
		Verdict: Ask,
		Reason:  Reason{Question: "risk", Levels: levels, Value: 1, Threshold: 1, DeadBand: true},
	}
	if got := dead.sentences()[0]; !strings.Contains(got, "inside the dead band") {
		t.Errorf("the dead band reads %q", got)
	}
	denied := Decision{
		Verdict: Deny,
		Reason:  Reason{Question: "risk", Levels: levels, Value: 1, Threshold: 0.5},
	}
	if got := denied.sentences()[0]; got != "risk is grave, so the call is refused" {
		t.Errorf("a refusal reads %q", got)
	}
}
