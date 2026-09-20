package jev

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

const recordedRoundingSlack = 0.03

type recordedAnswer struct {
	Source        string             `json:"source"`
	Model         string             `json:"model"`
	Kind          string             `json:"kind"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

func TestTheFormulasReproduceTheRecordedConfidence(t *testing.T) {
	body, err := os.ReadFile("testdata/recorded-confidence.json")
	if err != nil {
		t.Fatalf("reading the recorded answers: %v", err)
	}
	var recorded []recordedAnswer
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatalf("decoding the recorded answers: %v", err)
	}

	seen := map[string]int{}
	for _, answer := range recorded {
		var local float64
		switch answer.Kind {
		case "choice":
			local = choiceConfidence(answer.Probabilities)
		case "score":
			local = scoreConfidence(answer.Probabilities)
		default:
			t.Fatalf("%s: recorded as a %q, which has no distribution", answer.Source, answer.Kind)
		}
		seen[answer.Kind]++
		t.Logf("%-6s %-58s %-26s %3d wide server %.4f local %.4f gap %+.4f",
			answer.Kind, answer.Source, answer.Model, len(answer.Probabilities), answer.Confidence, local, local-answer.Confidence)
		if gap := math.Abs(local - answer.Confidence); gap > recordedRoundingSlack {
			t.Fatalf("%s: server %.4f and local %.4f differ by %.4f", answer.Source, answer.Confidence, local, gap)
		}
	}
	if seen["choice"] < 3 || seen["score"] < 3 {
		t.Fatalf("expected at least three recorded answers of each kind, found %v", seen)
	}
}

func TestTheFormulasPinTheEndsOfTheirRange(t *testing.T) {
	if got := choiceConfidence(map[string]float64{"a": 0.25, "b": 0.25, "c": 0.25, "d": 0.25}); got != 0 {
		t.Fatalf("a uniform choice should be 0, got %v", got)
	}
	if got := choiceConfidence(map[string]float64{"a": 1, "b": 0}); got != 1 {
		t.Fatalf("a one-hot choice should be 1, got %v", got)
	}
	if got := choiceConfidence(map[string]float64{"a": 0, "b": 0}); got != 0 {
		t.Fatalf("an all-zero choice should read as uniform, got %v", got)
	}
	if got := scoreConfidence(map[string]float64{"0": 0.25, "1": 0.25, "2": 0.25, "3": 0.25}); got != 0 {
		t.Fatalf("a uniform score should be 0, got %v", got)
	}
	if got := scoreConfidence(map[string]float64{"0": 0, "1": 1, "2": 0, "3": 0}); got != 1 {
		t.Fatalf("a one-hot score should be 1, got %v", got)
	}
	if got := scoreConfidence(map[string]float64{"0": 0.5, "low": 0.5}); got != 0 {
		t.Fatalf("a score level that is not an index should be 0, got %v", got)
	}
}

func TestTheServerConfidenceIsKeptBesideTheLocalOne(t *testing.T) {
	response, err := Decode([]byte(gateReply))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	act := response.Answers["act"]
	if act.Confidence != 0.75 {
		t.Fatalf("expected the server confidence untouched, got %v", act.Confidence)
	}
	if math.Abs(act.LocalConfidence-0.7) > 1e-9 {
		t.Fatalf("expected the local confidence 0.7 for 0.8 over three options, got %v", act.LocalConfidence)
	}
	risk := response.Answers["risk"]
	if risk.Confidence != 1 || risk.LocalConfidence != 1 {
		t.Fatalf("expected both confidences at 1 on a one-hot score, got %v and %v", risk.Confidence, risk.LocalConfidence)
	}
}
