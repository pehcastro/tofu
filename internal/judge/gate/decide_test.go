package gate

import (
	"testing"

	"tofu/internal/konst"
)

func TestDeadBandEscalatesOnBothSides(t *testing.T) {
	r := fixtureRule()
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
			answers := neutralAnswers(r)
			answers[r.RiskQuestion] = scoreAnswerFixture(c.risk)
			got, reason, err := Decide(answers, r)
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

func TestAScoreOneDeadBandFromTheThresholdIsInsideTheBand(t *testing.T) {
	r := fixtureRule()
	r.Thresholds.RiskAskAt = 0.25
	t.Logf("risk_ask_at is 0.25 here because 0.25 plus or minus %g is exact in binary, so the edge of the band is the edge and not a rounding of it", konst.ThresholdDeadBand)
	for _, risk := range []float64{r.Thresholds.RiskAskAt - konst.ThresholdDeadBand, r.Thresholds.RiskAskAt + konst.ThresholdDeadBand} {
		answers := neutralAnswers(r)
		answers[r.RiskQuestion] = scoreAnswerFixture(risk)
		got, reason, err := Decide(answers, r)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictAsk {
			t.Errorf("risk %v, one dead band from %v: verdict = %s, want %s", risk, r.Thresholds.RiskAskAt, got, VerdictAsk)
		}
		if !reason.DeadBand {
			t.Errorf("risk %v, one dead band from %v: the row does not record it as borderline", risk, r.Thresholds.RiskAskAt)
		}
	}
}

func TestTheReasonNamesTheThresholdItComparedAgainst(t *testing.T) {
	r := fixtureRule()
	cases := []struct {
		name       string
		risk       float64
		comparison Comparison
		threshold  float64
	}{
		{name: "clear of both thresholds", risk: 0.1, comparison: ComparisonRiskAskAt, threshold: r.Thresholds.RiskAskAt},
		{name: "above the ask threshold", risk: 2.0, comparison: ComparisonRiskAskAt, threshold: r.Thresholds.RiskAskAt},
		{name: "above the deny threshold", risk: 3.0, comparison: ComparisonRiskDenyAt, threshold: r.Thresholds.RiskDenyAt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answers := neutralAnswers(r)
			answers[r.RiskQuestion] = scoreAnswerFixture(c.risk)
			_, reason, err := Decide(answers, r)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if reason.Comparison != c.comparison {
				t.Fatalf("comparison = %s, want %s", reason.Comparison, c.comparison)
			}
			if reason.Threshold != c.threshold {
				t.Fatalf("threshold = %v, want %v, the number the row says decided it", reason.Threshold, c.threshold)
			}
			if reason.Value != c.risk {
				t.Fatalf("value = %v, want %v", reason.Value, c.risk)
			}
		})
	}
}

func TestTheApprovalRelaxationBandSitsWhereTheRulePutIt(t *testing.T) {
	r := fixtureRule()
	relax := r.Thresholds.ApprovalRelaxAt
	cases := []struct {
		name      string
		approval  float64
		want      Verdict
		relaxedBy string
	}{
		{name: "clear below the band", approval: relax - 2*konst.ThresholdDeadBand, want: VerdictAsk, relaxedBy: r.ApprovalQuestion},
		{name: "on the lower edge of the band", approval: relax - konst.ThresholdDeadBand, want: VerdictDeny},
		{name: "on the threshold itself", approval: relax, want: VerdictDeny},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answers := neutralAnswers(r)
			answers[r.RiskQuestion] = scoreAnswerFixture(3.0)
			answers[r.ApprovalQuestion] = noulAnswerFixture(c.approval)
			got, reason, err := Decide(answers, r)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got != c.want {
				t.Errorf("approval %v against approval_relax_at %v: verdict = %s, want %s", c.approval, relax, got, c.want)
			}
			if reason.RelaxedBy != c.relaxedBy {
				t.Errorf("approval %v: relaxed by = %q, want %q", c.approval, reason.RelaxedBy, c.relaxedBy)
			}
		})
	}
}

func TestTheUserRequestedRelaxationBandSitsWhereTheRulePutIt(t *testing.T) {
	r := fixtureRule()
	relax := r.Thresholds.UserRequestedRelaxAt
	cases := []struct {
		name          string
		userRequested float64
		want          Verdict
		relaxedBy     string
		ambiguous     string
	}{
		{name: "clear above the band", userRequested: relax + 2*konst.ThresholdDeadBand, want: VerdictAsk, relaxedBy: r.UserRequestedQuestion},
		{name: "on the upper edge of the band", userRequested: relax + konst.ThresholdDeadBand, want: VerdictDeny, ambiguous: r.UserRequestedQuestion},
		{name: "on the threshold itself", userRequested: relax, want: VerdictDeny, ambiguous: r.UserRequestedQuestion},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answers := neutralAnswers(r)
			answers[r.RiskQuestion] = scoreAnswerFixture(3.0)
			answers[r.UserRequestedQuestion] = noulAnswerFixture(c.userRequested)
			got, reason, err := Decide(answers, r)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got != c.want {
				t.Errorf("user_requested %v against user_requested_relax_at %v: verdict = %s, want %s", c.userRequested, relax, got, c.want)
			}
			if reason.RelaxedBy != c.relaxedBy {
				t.Errorf("user_requested %v: relaxed by = %q, want %q", c.userRequested, reason.RelaxedBy, c.relaxedBy)
			}
			if reason.Ambiguous != c.ambiguous {
				t.Errorf("user_requested %v: ambiguous = %q, want %q", c.userRequested, reason.Ambiguous, c.ambiguous)
			}
		})
	}
}

func TestAuthorityNoulRelaxesAndNeverTightens(t *testing.T) {
	r := fixtureRule()

	t.Run("user_requested relaxes deny to ask", func(t *testing.T) {
		answers := neutralAnswers(r)
		answers[r.RiskQuestion] = scoreAnswerFixture(3.0)
		answers[r.UserRequestedQuestion] = noulAnswerFixture(0.95)
		got, reason, err := Decide(answers, r)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictAsk {
			t.Errorf("verdict = %s, want %s", got, VerdictAsk)
		}
		if reason.RelaxedBy != r.UserRequestedQuestion {
			t.Errorf("relaxed by = %q, want %q", reason.RelaxedBy, r.UserRequestedQuestion)
		}
	})

	t.Run("approval cannot tighten allow", func(t *testing.T) {
		answers := neutralAnswers(r)
		answers[r.RiskQuestion] = scoreAnswerFixture(0.1)
		answers[r.ApprovalQuestion] = noulAnswerFixture(1.0)
		got, reason, err := Decide(answers, r)
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
		answers := neutralAnswers(r)
		answers[r.RiskQuestion] = scoreAnswerFixture(0.1)
		answers[r.FromUntrustedQuestion] = noulAnswerFixture(1.0)
		got, _, err := Decide(answers, r)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictAllow {
			t.Errorf("from_untrusted tightened the verdict: got %s, want %s", got, VerdictAllow)
		}
	})

	t.Run("user_requested inside the relax band does not relax and is marked ambiguous", func(t *testing.T) {
		answers := neutralAnswers(r)
		answers[r.RiskQuestion] = scoreAnswerFixture(3.0)
		answers[r.UserRequestedQuestion] = noulAnswerFixture(r.Thresholds.UserRequestedRelaxAt - 0.02)
		got, reason, err := Decide(answers, r)
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if got != VerdictDeny {
			t.Errorf("verdict = %s, want %s (ambiguous, so it should not relax)", got, VerdictDeny)
		}
		if reason.Ambiguous != r.UserRequestedQuestion {
			t.Errorf("ambiguous = %q, want %q", reason.Ambiguous, r.UserRequestedQuestion)
		}
	})
}

func TestFromUntrustedHoldsADenyAuthorityWouldOtherwiseRelax(t *testing.T) {
	r := fixtureRule()
	block := r.Thresholds.FromUntrustedBlockAt
	cases := []struct {
		name          string
		fromUntrusted float64
		want          Verdict
		blocked       bool
		relaxedBy     string
	}{
		{name: "at the block threshold", fromUntrusted: block, want: VerdictDeny, blocked: true},
		{name: "a dead band below the block threshold", fromUntrusted: block - konst.ThresholdDeadBand, want: VerdictDeny, blocked: true},
		{name: "certainly from untrusted content", fromUntrusted: 0.95, want: VerdictDeny, blocked: true},
		{name: "clear of the block threshold", fromUntrusted: block - 2*konst.ThresholdDeadBand, want: VerdictAsk, relaxedBy: r.UserRequestedQuestion},
		{name: "not from untrusted content at all", fromUntrusted: 0, want: VerdictAsk, relaxedBy: r.UserRequestedQuestion},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answers := neutralAnswers(r)
			answers[r.RiskQuestion] = scoreAnswerFixture(3.0)
			answers[r.UserRequestedQuestion] = noulAnswerFixture(0.95)
			answers[r.FromUntrustedQuestion] = noulAnswerFixture(c.fromUntrusted)
			got, reason, err := Decide(answers, r)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got != c.want {
				t.Errorf("from_untrusted %.2f with user_requested 0.95: verdict = %s, want %s", c.fromUntrusted, got, c.want)
			}
			if reason.Blocked != c.blocked {
				t.Errorf("from_untrusted %.2f: blocked = %v, want %v", c.fromUntrusted, reason.Blocked, c.blocked)
			}
			if reason.RelaxedBy != c.relaxedBy {
				t.Errorf("from_untrusted %.2f: relaxed by = %q, want %q", c.fromUntrusted, reason.RelaxedBy, c.relaxedBy)
			}
		})
	}
}
