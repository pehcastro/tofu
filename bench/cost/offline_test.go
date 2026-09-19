package cost

import (
	"testing"

	"boji/bench/corpus"
)

func halves(t *testing.T) (train, heldOut []corpus.Record) {
	t.Helper()
	records, err := corpus.GateRecords()
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	split, err := corpus.GateSplit()
	if err != nil {
		t.Fatalf("loading the split: %v", err)
	}
	inHeldOut := make(map[string]bool, len(split.Heldout))
	for _, id := range split.Heldout {
		inHeldOut[id] = true
	}
	for _, record := range records {
		if inHeldOut[record.ID] {
			heldOut = append(heldOut, record)
			continue
		}
		train = append(train, record)
	}
	return train, heldOut
}

func TestTheDeterministicArmsScoreWithoutTouchingTheNetwork(t *testing.T) {
	train, heldOut := halves(t)
	for _, half := range []struct {
		name    string
		records []corpus.Record
	}{{"train", train}, {"heldout", heldOut}} {
		regexArm, floorArm := runRegex(half.records), runAlwaysProceed(half.records)
		pair := mcNemar(regexArm, floorArm)
		t.Logf("%s: %d cases, regex %d correct (%d blocks caught, %d false blocks), always-proceed %d correct, McNemar p %.4f",
			half.name, len(half.records), regexArm.CorrectCount, regexArm.CaughtBlocks, regexArm.FalseBlocks, floorArm.CorrectCount, pair.P)
		if regexArm.CorrectCount < floorArm.CorrectCount {
			t.Errorf("%s: the regex arm scores %d and the constant floor scores %d, so the rules cost accuracy", half.name, regexArm.CorrectCount, floorArm.CorrectCount)
		}
	}
}

func TestTheSignTestSeparatesOnlyWhenTheDiscordantCountIsLopsided(t *testing.T) {
	if p := twoSidedSignP(0, 0); p != 1 {
		t.Errorf("two arms that never differ have p %.3f, want 1", p)
	}
	if p := twoSidedSignP(5, 5); p < separationAlpha {
		t.Errorf("an even split of ten discordant cases has p %.3f, which would claim a separation that is not there", p)
	}
	if p := twoSidedSignP(10, 0); p >= separationAlpha {
		t.Errorf("ten discordant cases all one way has p %.3f, want a separation", p)
	}
}
