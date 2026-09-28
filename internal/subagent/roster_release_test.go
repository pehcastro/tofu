package subagent

import (
	"errors"
	"testing"
)

func TestASettledSubAgentReleasesItsPaths(t *testing.T) {
	for _, state := range States() {
		t.Run(state.String(), func(t *testing.T) {
			var roster Roster
			if err := roster.Hold(SubAgent{ID: "sub-1", Owns: []string{"src/routes/**"}}); err != nil {
				t.Fatal(err)
			}
			roster.Reached("sub-1", state, "")
			err := roster.Hold(SubAgent{ID: "sub-2", Owns: []string{"src/routes/a.ts"}})
			var collision CollisionError
			refused := errors.As(err, &collision)
			settled := state == Finished || state == Errored
			if settled && err != nil {
				t.Fatalf("sub-1 is %s and still blocks sub-2: %v", state, err)
			}
			if !settled && !refused {
				t.Fatalf("sub-1 is %s and sub-2 was not refused: %v", state, err)
			}
		})
	}
}
