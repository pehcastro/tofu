package crew

import (
	"errors"
	"testing"
)

func TestRosterRefusesASecondHolderOfAnOverlappingPath(t *testing.T) {
	cases := []struct {
		name     string
		held     []string
		wanted   []string
		collides bool
	}{
		{"a_tree_and_one_file_inside_it", []string{"internal/crew/**"}, []string{"internal/crew/owns.go"}, true},
		{"one_file_inside_a_tree_and_the_tree", []string{"internal/crew/owns.go"}, []string{"internal/crew/**"}, true},
		{"the_same_glob_twice", []string{"internal/crew/**"}, []string{"internal/crew/**"}, true},
		{"two_trees_under_one_parent", []string{"internal/crew/**"}, []string{"internal/turn/**"}, false},
		{"a_segment_star_and_a_nested_file", []string{"internal/turn/*.go"}, []string{"internal/turn/tools/edit.go"}, false},
		{"a_segment_star_and_a_file_beside_it", []string{"internal/turn/*.go"}, []string{"internal/turn/spawn.go"}, true},
		{"two_files_in_one_directory", []string{"internal/turn/spawn.go"}, []string{"internal/turn/loop.go"}, false},
		{"a_tree_star_in_the_middle_and_a_matching_file", []string{"internal/**/edit.go"}, []string{"internal/turn/tools/edit.go"}, true},
		{"a_tree_star_in_the_middle_and_a_file_it_misses", []string{"internal/**/edit.go"}, []string{"internal/turn/tools/glob.go"}, false},
		{"case_does_not_hide_a_collision", []string{"Internal/Crew/**"}, []string{"internal/crew/owns.go"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			roster := &Roster{}
			if err := roster.Hold("child-1", c.held); err != nil {
				t.Fatalf("the first hold failed: %v", err)
			}
			err := roster.Hold("child-2", c.wanted)
			if c.collides != (err != nil) {
				t.Fatalf("holding %v against %v gave %v, wanted a collision: %v", c.wanted, c.held, err, c.collides)
			}
			if !c.collides {
				return
			}
			var collision CollisionError
			if !errors.As(err, &collision) {
				t.Fatalf("got %v, want a CollisionError", err)
			}
			if collision.Child != "child-2" || collision.Holder != "child-1" {
				t.Fatalf("the collision does not name both children: %+v", collision)
			}
		})
	}
}

func TestRosterHoldsNothingWhenOneGlobOfTheListCollides(t *testing.T) {
	roster := &Roster{}
	if err := roster.Hold("child-1", []string{"internal/crew/**"}); err != nil {
		t.Fatal(err)
	}
	if err := roster.Hold("child-2", []string{"internal/turn/**", "internal/crew/owns.go"}); err == nil {
		t.Fatal("expected the second hold to be refused")
	}
	if err := roster.Hold("child-3", []string{"internal/turn/**"}); err != nil {
		t.Fatalf("the refused list left a partial hold behind: %v", err)
	}
}

func TestRosterRefusesAGlobItCannotParse(t *testing.T) {
	roster := &Roster{}
	err := roster.Hold("child-1", []string{"internal/crew/??.go"})
	var target UnparseableGlobError
	if !errors.As(err, &target) {
		t.Fatalf("got %v, want an UnparseableGlobError", err)
	}
}
