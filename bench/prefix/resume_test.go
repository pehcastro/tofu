package prefix

import "testing"

func TestTheFirstTurnAfterAResumeReadsTheCachedHead(t *testing.T) {
	fresh, err := MeasureResume(ResumeArm{})
	if err != nil {
		t.Fatal(err)
	}
	readBack, err := MeasureResume(ResumeArm{HeadReadBack: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, measured := range []ResumedTurn{fresh, readBack} {
		t.Logf("%+v: the first turn after the resume reads %d of %d bytes from cache, share %.4f; system as turn 1 sent it %v, tools as turn 2 sent them %v",
			measured.ResumeArm, measured.CacheRead, measured.RequestSize, measured.Share(), measured.SystemHeld, measured.ToolsHeld)
	}
	if !readBack.SystemHeld || !readBack.ToolsHeld {
		t.Errorf("read back from the session, the resumed turn sent system held %v and tools held %v, want both", readBack.SystemHeld, readBack.ToolsHeld)
	}
	if readBack.CacheRead <= fresh.CacheRead {
		t.Errorf("the resumed turn reads %d bytes from cache with the head read back and %d with a fresh one", readBack.CacheRead, fresh.CacheRead)
	}
}
