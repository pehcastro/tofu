package ledger

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestAScoreAnswerCarriesTheLegendItWasAnsweredWith(t *testing.T) {
	const raw = `{"question":"risk","wording":6,"kind":"score","score":2,` +
		`"dist":[{"option":"0","p":0.01},{"option":"1","p":0.05},{"option":"2","p":0.94}],` +
		`"legend":["reversible","easy to undo","hard to undo"]}`
	var answer Answer
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		t.Fatal(err)
	}
	want := []string{"reversible", "easy to undo", "hard to undo"}
	if !slices.Equal(answer.Legend, want) {
		t.Fatalf("legend = %#v, want %#v", answer.Legend, want)
	}
	again, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	var back Answer
	if err := json.Unmarshal(again, &back); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(back.Legend, want) {
		t.Fatalf("the legend written back reads %#v, want %#v", back.Legend, want)
	}
}

func TestAnAnswerRecordedBeforeTheLegendReadsAndWritesWithoutOne(t *testing.T) {
	const recorded = `{"dist":[{"option":"0","p":0},{"option":"1","p":0},{"option":"2","p":0.94},` +
		`{"option":"3","p":0.06}],"kind":"score","question":"risk","score":2.05,"wording":3}`
	var answer Answer
	if err := json.Unmarshal([]byte(recorded), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Legend != nil {
		t.Fatalf("legend = %#v, want none: this answer predates the field", answer.Legend)
	}
	if answer.Score != 2.05 || answer.Kind != AnswerScore {
		t.Fatalf("answer = %+v, want the score the row recorded", answer)
	}
	again, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(again, &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["legend"]; present {
		t.Fatalf("an answer with no legend writes the key anyway: %s", again)
	}
}
