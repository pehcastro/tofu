package cost

import (
	"os"
	"strings"
	"testing"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
)

func shippedGate(t *testing.T) (gate.Rule, gate.Resolution) {
	t.Helper()
	pol, resolution, err := gateRule(repoRoot)
	if err != nil {
		t.Fatalf("loading the shipped tool gate: %v", err)
	}
	return pol, resolution
}

func gateGrid() []gateAnswers {
	var grid []gateAnswers
	for risk := 0.0; risk <= 3.0; risk += 0.25 {
		for approval := 0.0; approval <= 1.0; approval += 0.1 {
			for requested := 0.0; requested <= 1.0; requested += 0.1 {
				for untrusted := 0.0; untrusted <= 1.0; untrusted += 0.25 {
					grid = append(grid, gateAnswers{Risk: risk, Approval: approval, UserRequested: requested, FromUntrusted: untrusted})
				}
			}
		}
	}
	return grid
}

func TestTheArmAndTheGateReachTheSameVerdictFromTheSameAnswers(t *testing.T) {
	pol, _ := shippedGate(t)
	for _, answer := range gateGrid() {
		answers := answerMap(pol, answer)
		armVerdict, _, err := decide(answers, pol)
		if err != nil {
			t.Fatalf("%+v: %v", answer, err)
		}
		gateVerdict, _, err := gate.Decide(answers, pol)
		if err != nil {
			t.Fatalf("%+v: %v", answer, err)
		}
		want := Block
		if gateVerdict == gate.VerdictAllow {
			want = Proceed
		}
		if armVerdict != want {
			t.Fatalf("%+v: the arm says %s and the gate says %s", answer, armVerdict, gateVerdict)
		}
	}
}

func TestTheArmRefusesAnAnswerSetTheGateWouldRefuse(t *testing.T) {
	pol, _ := shippedGate(t)
	answers := answerMap(pol, gateAnswers{Risk: 3})
	delete(answers, pol.RiskQuestion)
	if _, _, err := decide(answers, pol); err == nil {
		t.Fatal("the arm decided without a risk answer, so it is not going through gate.Decide")
	}
	answers = answerMap(pol, gateAnswers{Risk: 3})
	answers[pol.RiskQuestion] = jev.Answer{Kind: jev.QuestionNoul, Noul: 1}
	if _, _, err := decide(answers, pol); err == nil {
		t.Fatal("the arm read a noul where the rule needs a score")
	}
}

func TestTheGateCarriesNoThresholdInItsOwnSource(t *testing.T) {
	source, err := os.ReadFile("gate.go")
	if err != nil {
		t.Fatalf("reading gate.go: %v", err)
	}
	for _, literal := range []string{"0.5", "0.75", "0.8", "1.5", "2.5"} {
		if strings.Contains(string(source), literal) {
			t.Fatalf("gate.go carries %q, so a threshold is in code again", literal)
		}
	}
}

func TestNoRecordedRowCarriesTheRiskAnswerTheGateDecidesOn(t *testing.T) {
	result := recordedRescore(t)
	if result.ModelCalls != 0 {
		t.Fatalf("the re-score counted %d model calls", result.ModelCalls)
	}
	total := 0
	for _, arm := range result.Arms {
		total += arm.Rescored
		t.Logf("%s: %d rows, %d re-scorable, %d with answers but no risk, %d verdict alone, %d refusals",
			arm.Arm, arm.Rows, arm.Rescored, arm.PartialAnswers, arm.VerdictOnly, arm.Refusals)
	}
	if total != 0 {
		t.Fatalf("%d rows re-scored, and the 2026-09-19 run persisted no risk answer at all", total)
	}
}

func TestARowThatCarriesAllFourAnswersIsRescoredAndCanChangeItsVerdict(t *testing.T) {
	pol, resolution := shippedGate(t)
	risk, untrusted := 3.0, 0.0
	rows := []AnswerRow{{
		Case: "probe", Arm: "jev", Label: Block, Verdict: Proceed,
		Risk: &risk, FromUntrusted: &untrusted, Approval: 0.10, UserRequested: 0.0,
		AnswersKnown: true,
	}}
	result, err := Rescore(rows, pol, resolution)
	if err != nil {
		t.Fatalf("re-scoring: %v", err)
	}
	arm := result.Arms[0]
	if arm.Rescored != 1 {
		t.Fatalf("a row with all four answers was not re-scored: %+v", arm)
	}
	if arm.Changed != 1 || arm.CorrectRecorded != 0 || arm.CorrectRescored != 1 {
		t.Fatalf("a risk of 3.0 should turn a recorded proceed into a block: %+v", arm)
	}
}
