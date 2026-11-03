package prefix

import "testing"

func TestPrefixHeldWhileARuleAddedMidSessionReachesTheModelOnce(t *testing.T) {
	arms := map[LaterArm]LaterTurns{}
	for _, arm := range []LaterArm{{}, {RuleAdded: true}, {RuleAdded: true, HeadRewritten: true}} {
		measured, err := MeasureLaterTurns(arm)
		if err != nil {
			t.Fatal(err)
		}
		arms[arm] = measured
		t.Logf("%+v: turn 2 reads %d of %d bytes from cache, share %.4f; turn 3 reads %d of %d, share %.4f; the rule named %v times per request",
			arm, measured.CacheRead[0], measured.RequestSize[0], measured.Share(2), measured.CacheRead[1], measured.RequestSize[1], measured.Share(3), measured.Carries)
	}
	unchanged, added, rewritten := arms[LaterArm{}], arms[LaterArm{RuleAdded: true}], arms[LaterArm{RuleAdded: true, HeadRewritten: true}]
	for _, held := range []LaterTurns{unchanged, added} {
		if !held.SystemHeld() {
			t.Errorf("%+v: a later turn sent a different system array, so the cached head breaks", held.LaterArm)
		}
	}
	if unchanged.Carries != [3]int{} {
		t.Errorf("no rule was added and a request named it: %v", unchanged.Carries)
	}
	if added.Carries != [3]int{0, 1, 1} {
		t.Errorf("the rule added after turn 1 is named %v times per request, want 0, 1 and 1: absent before it existed, sent once, carried once", added.Carries)
	}
	for at := range added.CacheRead {
		if added.CacheRead[at] < unchanged.CacheRead[at] {
			t.Errorf("turn %d reads %d bytes from cache with the rule added and %d without it", at+2, added.CacheRead[at], unchanged.CacheRead[at])
		}
	}
	if rewritten.CacheRead[0] >= added.CacheRead[0] {
		t.Errorf("rewriting the head reads %d bytes on turn 2 and appending reads %d, so the arm measures nothing", rewritten.CacheRead[0], added.CacheRead[0])
	}
}
