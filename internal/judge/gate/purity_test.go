package gate

import "testing"

func TestDecideIsPure(t *testing.T) {
	r := fixtureRule()
	answers := neutralAnswers(r)
	answers[r.RiskQuestion] = scoreAnswerFixture(2.7)
	answers[r.UserRequestedQuestion] = noulAnswerFixture(0.9)

	firstVerdict, firstReason, err := Decide(answers, r)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	for i := 0; i < 1000; i++ {
		verdict, reason, err := Decide(answers, r)
		if err != nil {
			t.Fatalf("run %d: Decide: %v", i, err)
		}
		if verdict != firstVerdict {
			t.Fatalf("run %d: verdict = %s, want %s", i, verdict, firstVerdict)
		}
		if reason != firstReason {
			t.Fatalf("run %d: reason = %+v, want %+v", i, reason, firstReason)
		}
	}
}
