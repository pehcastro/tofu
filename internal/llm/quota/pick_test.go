package quota

import (
	"testing"
	"time"
)

func TestThreeTurnsAtEqualHeadroomUseThreeAccounts(t *testing.T) {
	same := func(id int64, used time.Time) Candidate {
		candidate := claudeAccount(id, reported(0.4, fiveHourWindow, recordedNow.Add(time.Hour)))
		candidate.LastUsed = used
		return candidate
	}
	candidates := []Candidate{same(1, recordedNow.Add(-time.Minute)), same(2, time.Time{}), same(3, recordedNow.Add(-time.Hour))}
	var picked []int64
	for turn := range candidates {
		choice, found := Pick(candidates, ClaudeSub, nil, recordedNow)
		if !found {
			t.Fatal("no account picked")
		}
		picked = append(picked, choice.ID)
		candidates[choice.ID-1].LastUsed = recordedNow.Add(time.Duration(turn+1) * time.Second)
	}
	if picked[0] != 2 || picked[1] != 3 || picked[2] != 1 {
		t.Fatalf("three turns at equal headroom picked %v, want 2, 3, 1: never used first, then least recently used", picked)
	}
}
