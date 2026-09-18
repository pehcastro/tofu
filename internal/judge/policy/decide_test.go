package policy

import "testing"

func TestDeadBandEscalatesOnBothSides(t *testing.T) {
	pol := fixturePolicy()
	cases := []struct {
		name string
		risk float64
		want Verdict
		band bool
	}{
		{name: "clearly below risk_ask_at", risk: 1.30, want: VerdictAllow, band: false},
		{name: "just below risk_ask_at, inside the band", risk: 1.45, want: VerdictAsk, band: true},
		{name: "just above risk_ask_at, inside the band", risk: 1.55, want: VerdictAsk, band: true},
		{name: "clearly above risk_ask_at", risk: 1.70, want: VerdictAsk, band: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answers := neutralAnswers(pol)
			answers[pol.RiskQuestion] = scoreAnswerFixture(c.risk)
			got, reason, err := Decide(answers, pol)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got != c.want {
				t.Errorf("risk %.2f: verdict = %s, want %s", c.risk, got, c.want)
			}
			if reason.DeadBand != c.band {
				t.Errorf("risk %.2f: dead band = %v, want %v", c.risk, reason.DeadBand, c.band)
			}
		})
	}
}

func TestAuthorityNoulRelaxesAndNeverTightens(t *testing.T) {
	pol := fixturePolicy()

	t.Run("user_requested relaxes deny to ask", func(t *testing.T) {
		answers := neutralAnswers(pol)
		answers[pol.RiskQuestion] = scoreAnswerFixture(3.0)
		answers[pol.UserRequestedQuestion] = noulAnswerFixture(0.95)
		got, reason, err := Decide(answers, pol)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictAsk {
			t.Errorf("verdict = %s, want %s", got, VerdictAsk)
		}
		if reason.RelaxedBy != pol.UserRequestedQuestion {
			t.Errorf("relaxed by = %q, want %q", reason.RelaxedBy, pol.UserRequestedQuestion)
		}
	})

	t.Run("approval cannot tighten allow", func(t *testing.T) {
		answers := neutralAnswers(pol)
		answers[pol.RiskQuestion] = scoreAnswerFixture(0.1)
		answers[pol.ApprovalQuestion] = noulAnswerFixture(1.0)
		got, reason, err := Decide(answers, pol)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictAllow {
			t.Errorf("a noul tightened the verdict: got %s, want %s", got, VerdictAllow)
		}
		if reason.RelaxedBy != "" {
			t.Errorf("relaxed by = %q, want none", reason.RelaxedBy)
		}
	})

	t.Run("from_untrusted cannot tighten allow", func(t *testing.T) {
		answers := neutralAnswers(pol)
		answers[pol.RiskQuestion] = scoreAnswerFixture(0.1)
		answers[pol.FromUntrustedQuestion] = noulAnswerFixture(1.0)
		got, _, err := Decide(answers, pol)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictAllow {
			t.Errorf("from_untrusted tightened the verdict: got %s, want %s", got, VerdictAllow)
		}
	})

	t.Run("from_untrusted blocks a relax it did not earn", func(t *testing.T) {
		answers := neutralAnswers(pol)
		answers[pol.RiskQuestion] = scoreAnswerFixture(3.0)
		answers[pol.UserRequestedQuestion] = noulAnswerFixture(0.95)
		answers[pol.FromUntrustedQuestion] = noulAnswerFixture(0.95)
		got, reason, err := Decide(answers, pol)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictDeny {
			t.Errorf("verdict = %s, want %s (the relax should have been blocked)", got, VerdictDeny)
		}
		if !reason.Blocked {
			t.Errorf("reason.Blocked = false, want true")
		}
	})

	t.Run("user_requested inside the relax band does not relax and is marked ambiguous", func(t *testing.T) {
		answers := neutralAnswers(pol)
		answers[pol.RiskQuestion] = scoreAnswerFixture(3.0)
		answers[pol.UserRequestedQuestion] = noulAnswerFixture(pol.Thresholds.UserRequestedRelaxAt - 0.02)
		got, reason, err := Decide(answers, pol)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictDeny {
			t.Errorf("verdict = %s, want %s (ambiguous, so it should not relax)", got, VerdictDeny)
		}
		if reason.Ambiguous != pol.UserRequestedQuestion {
			t.Errorf("ambiguous = %q, want %q", reason.Ambiguous, pol.UserRequestedQuestion)
		}
	})
}
