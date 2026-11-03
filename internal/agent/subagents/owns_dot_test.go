package subagent

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAWholeProjectOwnsWritesEveryProjectFileAndNothingOutside(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	cases := []struct {
		path  string
		owns  []string
		allow bool
	}{
		{"account/account.go", []string{"."}, true},
		{"account/account.go", []string{"./"}, true},
		{"go.mod", []string{"."}, true},
		{"./internal/deep/a/b.go", []string{"./"}, true},
		{"account/account.go", []string{"./account/"}, true},
		{"other/x.go", []string{"./account/"}, false},
		{"../outside.txt", []string{"."}, false},
		{"account/../../outside.txt", []string{"./"}, false},
		{"/etc/passwd", []string{"."}, false},
		{outside, []string{"."}, false},
		{filepath.ToSlash(outside), []string{"./"}, false},
		{"account/account.go", []string{".."}, false},
		{"account/account.go", []string{"../"}, false},
		{"account/account.go", []string{""}, false},
		{"account/account.go", []string{" "}, false},
	}
	for _, c := range cases {
		err := Allow(c.path, c.owns)
		if (err == nil) != c.allow {
			t.Errorf("Allow(%q, %q) = %v, want allowed %v", c.path, c.owns, err, c.allow)
		}
	}
}

func TestTwoSubAgentsCannotBothHoldTheWholeProject(t *testing.T) {
	for _, pair := range [][2]string{{".", "."}, {".", "./"}, {"./", "account/**"}, {".", "go.mod"}, {"internal/", "."}} {
		var roster Roster
		if err := roster.Hold(SubAgent{ID: "sub-1", Owns: []string{pair[0]}}); err != nil {
			t.Fatal(err)
		}
		var collision CollisionError
		if err := roster.Hold(SubAgent{ID: "sub-2", Owns: []string{pair[1]}}); !errors.As(err, &collision) {
			t.Errorf("sub-1 holds %q and sub-2 took %q: %v", pair[0], pair[1], err)
		}
	}
}

func TestAWholeProjectOwnsHoldsTheTree(t *testing.T) {
	for _, owns := range []string{".", "./", "**"} {
		if !NewBoundary("t", "", []string{owns}).HoldsTheTree() {
			t.Errorf("owns %q does not hold the tree", owns)
		}
	}
	for _, owns := range []string{"..", "account/**", "./account/"} {
		if NewBoundary("t", "", []string{owns}).HoldsTheTree() {
			t.Errorf("owns %q holds the tree", owns)
		}
	}
}
