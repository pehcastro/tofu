package server

import (
	"path/filepath"
	"testing"
)

func loadBoth(t *testing.T) (Moment, Moment) {
	t.Helper()
	first, err := Load(Session1Dir, "dev")
	if err != nil {
		t.Fatalf("Load session 1: %v", err)
	}
	second, err := Load(Session2Dir, "dev")
	if err != nil {
		t.Fatalf("Load session 2: %v", err)
	}
	return first, second
}

func TestRecordedStepsMatchTheTicket(t *testing.T) {
	first, second := loadBoth(t)

	if first.StepsTotal != 18 || first.GuessStep != 4 {
		t.Fatalf("session 1: got %d steps, guess at step %d, want 18 steps and a guess at step 4", first.StepsTotal, first.GuessStep)
	}
	if second.StepsTotal != 24 || second.GuessStep != 2 {
		t.Fatalf("session 2: got %d steps, guess at step %d, want 24 steps and a guess at step 2", second.StepsTotal, second.GuessStep)
	}
	t.Logf("session 1 %s: %d steps, guess at step %d, %d steps removed if asked", first.TurnID, first.StepsTotal, first.GuessStep, first.StepsRemoved())
	t.Logf("session 2 %s: %d steps, guess at step %d, %d steps removed if asked", second.TurnID, second.StepsTotal, second.GuessStep, second.StepsRemoved())
}

func TestNameMatchActsAndItActsWrong(t *testing.T) {
	first, second := loadBoth(t)
	for _, m := range []Moment{first, second} {
		candidates := CandidatesByName(m.Task, m.Scripts)
		if len(candidates) != 1 {
			t.Fatalf("%s: name match found %v, want exactly one candidate", m.TurnID, candidates)
		}
		if Decide(candidates) != FitAct {
			t.Fatalf("%s: one candidate must act", m.TurnID)
		}
		if candidates[0] == m.Ideal {
			t.Fatalf("%s: name match picked %q, expected it to be wrong against ideal %q", m.TurnID, candidates[0], m.Ideal)
		}
		t.Logf("%s: name match acts on %q, wrong against ideal %q", m.TurnID, candidates[0], m.Ideal)
	}
}

func TestShapeMatchAsksAndDoesNotPickWrong(t *testing.T) {
	first, second := loadBoth(t)
	for _, m := range []Moment{first, second} {
		candidates := CandidatesByShape(m.Scripts)
		if len(candidates) < 2 {
			t.Fatalf("%s: shape match found %v, want two or more candidates", m.TurnID, candidates)
		}
		if Decide(candidates) != FitAsk {
			t.Fatalf("%s: two or more candidates must ask", m.TurnID)
		}
		t.Logf("%s: shape match asks among %v", m.TurnID, candidates)
	}
}

func TestFireShareAcrossTheWholeCorpus(t *testing.T) {
	share, err := Scan(BobSessionsDirFromPackage, filepath.Dir(Session1Dir))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if share.InDomain < 2 {
		t.Fatalf("the two named sessions must both be in the intent domain, found %d turns", share.InDomain)
	}
	t.Logf("%d turns total, %d in the start-server intent domain, %d of those the arm would ask on: %v",
		share.TotalTurns, share.InDomain, share.Fires, share.DomainIDs)
}
