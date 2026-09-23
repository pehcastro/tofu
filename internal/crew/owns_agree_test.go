package crew

import (
	"math/rand/v2"
	"strings"
	"testing"
)

const (
	generatedPairs       = 2000
	generatorSeedLow     = 0x746f667520343239
	generatorSeedHigh    = 0x6f776e7320676c62
	maxGeneratedSegments = 4
)

func referenceMatch(glob, path string) bool {
	if strings.HasSuffix(glob, "/**") && path == strings.TrimSuffix(glob, "/**") {
		return true
	}
	return referenceConsume(glob, path)
}

func referenceConsume(glob, path string) bool {
	switch {
	case glob == "":
		return path == ""
	case strings.HasPrefix(glob, "**"):
		for i := 0; i <= len(path); i++ {
			if referenceConsume(glob[2:], path[i:]) {
				return true
			}
		}
		return false
	case strings.HasPrefix(glob, "*"):
		for i := 0; i <= len(path) && !strings.Contains(path[:i], "/"); i++ {
			if referenceConsume(glob[1:], path[i:]) {
				return true
			}
		}
		return false
	default:
		return path != "" && glob[0] == path[0] && referenceConsume(glob[1:], path[1:])
	}
}

func TestOverlapAndMatchesReadTheSameGlobLanguage(t *testing.T) {
	cases := []struct {
		name string
		glob string
		path string
		want bool
	}{
		{"a_subtree_glob_owns_the_directory_it_names", "bench/**", "bench", true},
		{"a_subtree_glob_owns_a_file_under_it", "bench/**", "bench/harness/run.go", true},
		{"a_subtree_glob_does_not_own_a_sibling_sharing_its_prefix", "bench/**", "benchx", false},
		{"a_subtree_glob_does_not_own_its_parent", "bench/harness/**", "bench", false},
		{"a_segment_star_stays_inside_one_segment", "bench/*.go", "bench/run.go", true},
		{"a_segment_star_does_not_cross_a_boundary", "bench/*.go", "bench/harness/run.go", false},
		{"a_bare_tree_star_owns_everything", "**", "bench/harness/run.go", true},
		{"a_literal_glob_owns_itself", "cmd/tofu/main.go", "cmd/tofu/main.go", true},
		{"a_literal_glob_does_not_own_a_sibling", "cmd/tofu/main.go", "cmd/tofu/lint.go", false},
		{"a_tree_star_in_the_middle_needs_a_segment_under_it", "bench/**/run.go", "bench/run.go", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matched, err := Matches(c.path, []string{c.glob})
			if err != nil {
				t.Fatalf("Matches(%q, %q) returned error: %v", c.path, c.glob, err)
			}
			if matched != c.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", c.path, c.glob, matched, c.want)
			}
			if walked := overlap(c.glob, c.path); walked != c.want {
				t.Errorf("overlap(%q, %q) = %v, want %v", c.glob, c.path, walked, c.want)
			}
		})
	}
}

var (
	generatedGlobSegments = []string{"bench", "internal", "crew", "cmd", "a", "b-1", "*", "**", "*.go", "run.go"}
	generatedPathSegments = []string{"bench", "internal", "crew", "cmd", "a", "b-1", "run.go", "owns.go"}
)

func generatedJoin(r *rand.Rand, segments []string) string {
	parts := make([]string, 1+r.IntN(maxGeneratedSegments))
	for i := range parts {
		parts[i] = segments[r.IntN(len(segments))]
	}
	return strings.Join(parts, "/")
}

func TestOverlapAndMatchesAgreeOnGeneratedPairs(t *testing.T) {
	r := rand.New(rand.NewPCG(generatorSeedLow, generatorSeedHigh))
	disagreements := 0
	for range generatedPairs {
		glob, path := generatedJoin(r, generatedGlobSegments), generatedJoin(r, generatedPathSegments)
		matched, err := Matches(path, []string{glob})
		if err != nil {
			t.Fatalf("Matches(%q, %q) returned error: %v", path, glob, err)
		}
		walked, wanted := overlap(glob, path), referenceMatch(glob, path)
		if matched != wanted || walked != wanted {
			disagreements++
			t.Errorf("%q against %q: Matches %v, overlap %v, reference %v", glob, path, matched, walked, wanted)
		}
	}
	t.Logf("%d generated pairs, %d disagreements", generatedPairs, disagreements)
}
