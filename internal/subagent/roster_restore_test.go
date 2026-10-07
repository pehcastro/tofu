package subagent

import (
	"errors"
	"testing"
)

func refusedBy(t *testing.T, err error) string {
	t.Helper()
	var collision CollisionError
	if err != nil && !errors.As(err, &collision) {
		t.Fatalf("a hold failed for a reason other than a collision: %v", err)
	}
	return collision.Holder
}

func TestARestoredOrReleasedSubAgentHoldsNoPathsUntilItReclaimsThem(t *testing.T) {
	var roster Roster
	roster.Restore(SubAgent{ID: "rust-dev-3", Owns: []string{"components/**"}, State: Parked})
	if holder := refusedBy(t, roster.Hold(SubAgent{ID: "rust-dev-4", Owns: []string{"components/list.rs"}})); holder != "" {
		t.Fatalf("a restored rust-dev-3 still blocks a spawn on its paths, refused by %s", holder)
	}
	if holder := refusedBy(t, roster.Reclaim("rust-dev-3", "resumed")); holder != "rust-dev-4" {
		t.Fatalf("rust-dev-3 reclaimed components/** while rust-dev-4 holds a file in it, refused by %q", holder)
	}
	if _, found := roster.Release("rust-dev-4"); !found {
		t.Fatal("rust-dev-4 is on the roster and release did not find it")
	}
	if _, found := roster.Release("rust-dev-9"); found {
		t.Fatal("release found rust-dev-9, which was never on the roster")
	}
	if err := roster.Reclaim("rust-dev-3", "resumed"); err != nil {
		t.Fatalf("rust-dev-3 could not reclaim paths nobody holds: %v", err)
	}
	if holder := refusedBy(t, roster.Hold(SubAgent{ID: "rust-dev-5", Owns: []string{"components/tabs.rs"}})); holder != "rust-dev-3" {
		t.Fatalf("after its reclaim rust-dev-3 does not hold its paths, refused by %q", holder)
	}
	roster.Reached("rust-dev-3", Parked, "stopped")
	if holder := refusedBy(t, roster.Hold(SubAgent{ID: "rust-dev-6", Owns: []string{"components/form.rs"}})); holder != "rust-dev-3" {
		t.Fatalf("a parked rust-dev-3 that was not released no longer holds its paths, refused by %q", holder)
	}
	if err := roster.Reclaim("rust-dev-3", "resumed again"); err != nil {
		t.Fatalf("a parked rust-dev-3 still holding its paths collided with itself on its reclaim: %v", err)
	}
	released, _ := roster.Release("rust-dev-3")
	roster.Reached("rust-dev-3", Parked, "the run unwound after the release")
	if holder := refusedBy(t, roster.Hold(SubAgent{ID: "rust-dev-7", Owns: []string{"components/form.rs"}})); holder != "" || !released.Released {
		t.Fatalf("a state change after the release put rust-dev-3 back on its paths, refused by %q, released %v", holder, released.Released)
	}
}
