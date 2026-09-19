package cost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTheOfflineArmsWriteAnAnswersFileWithOneRowPerArmPerCase(t *testing.T) {
	pol, _ := shippedGate(t)
	_, heldOut := halves(t)
	regexArm, floorArm := runRegex(heldOut), runAlwaysProceed(heldOut)
	path := filepath.Join(t.TempDir(), answersFilename(time.Now()))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	arms := []ArmResult{regexArm, floorArm}
	if err := writeAnswers(path, answerRows(arms, pol, "the offline arms")); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	rows := readBack(t, path)
	want := len(regexArm.Cases) + len(floorArm.Cases)
	if len(rows) != want {
		t.Fatalf("%d rows for %d arm-case pairs", len(rows), want)
	}
	for _, row := range rows {
		if row.AnswersKnown || row.Live {
			t.Fatalf("%s/%s claims an answer or a call, and a deterministic arm makes neither", row.Arm, row.Case)
		}
	}
}

func TestTheArtifactCarriesEveryAnswerTheGateReadsAndSurvivesTheRoundTrip(t *testing.T) {
	pol, _ := shippedGate(t)
	answer := gateAnswers{Risk: 2.75, Approval: 0.62, UserRequested: 0.09, FromUntrusted: 0.44}
	answers := answerMap(pol, answer)
	verdict, _, err := decide(answers, pol)
	if err != nil {
		t.Fatal(err)
	}
	arm := ArmResult{Arm: "jev", Cases: []CaseResult{{
		Case: "probe", ModelID: "typesafe/jev-1.13-20260917", Answers: answers, Verdict: verdict, Label: Block,
	}}}
	path := filepath.Join(t.TempDir(), "answers.jsonl")
	if err := writeAnswers(path, answerRows([]ArmResult{arm}, pol, "a round trip")); err != nil {
		t.Fatal(err)
	}
	row := readBack(t, path)[0]
	if row.Risk == nil || row.FromUntrusted == nil {
		t.Fatalf("the file dropped an answer the gate reads: %+v", row)
	}
	got := gateAnswers{Risk: *row.Risk, Approval: row.Approval, UserRequested: row.UserRequested, FromUntrusted: *row.FromUntrusted}
	if got != answer {
		t.Fatalf("read back %+v, wrote %+v", got, answer)
	}
	back, complete := rowAnswers(pol, row)
	if !complete {
		t.Fatal("a row written by this run is not re-scorable, which is the gap the run is meant to close")
	}
	again, _, err := decide(back, pol)
	if err != nil {
		t.Fatal(err)
	}
	if again != verdict {
		t.Fatalf("re-scoring the artifact gives %s where the run gave %s", again, verdict)
	}
}

func readBack(t *testing.T, path string) []AnswerRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	rows, err := parseAnswers(data, path)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return rows
}
