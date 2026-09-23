package stopcheck

import (
	"testing"

	"tofu/internal/judge/ledger"
)

func TestAnswerValuesNamesTheBatteryTheQuestionAndTheKindOnAChoiceAnswer(t *testing.T) {
	answers := []ledger.Answer{
		{Question: "novelty", Kind: ledger.AnswerChoice, Choice: "new_idea"},
	}
	_, err := answerValues("stop_check", answers)
	if err == nil {
		t.Fatal("answerValues did not fail on a choice answer")
	}
	want := "stopcheck: battery stop_check asks no choice question and novelty came back as one"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestAnswerValuesKeepsNoulAndScore(t *testing.T) {
	answers := []ledger.Answer{
		{Question: "effort", Kind: ledger.AnswerScore, Score: 1.9},
		{Question: "worth_building", Kind: ledger.AnswerNoul, Noul: 0.48},
	}
	got, err := answerValues("stop_check", answers)
	if err != nil {
		t.Fatalf("answerValues: %v", err)
	}
	if got["effort"] != 1.9 || got["worth_building"] != 0.48 {
		t.Fatalf("answerValues = %+v, want both values carried through", got)
	}
}
