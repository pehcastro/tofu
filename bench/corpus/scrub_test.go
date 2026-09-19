package corpus

import (
	"strings"
	"testing"
)

func rawGateCases(t *testing.T) string {
	t.Helper()
	raw, err := gateFiles.ReadFile("gate/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestScrubbingTwiceGivesTheSameBytesAsScrubbingOnce(t *testing.T) {
	raw := rawGateCases(t)
	first := Scrub(raw)
	second := Scrub(raw)
	if first != second {
		t.Fatal("two runs of Scrub over the same input disagree, so a case recorded tomorrow would not line up with one recorded today")
	}
	if again := Scrub(first); again != first {
		t.Fatal("scrubbing an already scrubbed corpus changed it, so the replacement is not a fixed point")
	}
}

func TestNoTwoRealPathsCollapseIntoOneScrubbedPath(t *testing.T) {
	seen := map[string]string{}
	for _, substitution := range identitySubstitutions {
		for _, tail := range []string{"", "/one", "/two", `\one`} {
			real := substitution.real + tail
			clean := Scrub(real)
			if earlier, taken := seen[clean]; taken {
				t.Errorf("%q and %q both scrub to %q", earlier, real, clean)
			}
			seen[clean] = real
		}
	}
}

func TestTheScrubReplacesRatherThanDeletesSoThePathShapeSurvives(t *testing.T) {
	for _, substitution := range identitySubstitutions {
		real, clean := substitution.real, Scrub(substitution.real)
		if clean == "" {
			t.Errorf("%q scrubs to nothing, which changes what the state builder reads", real)
		}
		if strings.Count(real, "/") != strings.Count(clean, "/") {
			t.Errorf("%q has %d separators and scrubs to %q with %d", real, strings.Count(real, "/"), clean, strings.Count(clean, "/"))
		}
	}
}

func TestTheCorpusCarriesNoneOfTheOwnersIdentity(t *testing.T) {
	raw := rawGateCases(t)
	for _, substitution := range identitySubstitutions {
		for _, separator := range separatorsInOrderOfLength {
			identity := strings.ReplaceAll(substitution.real, "/", separator)
			if strings.Contains(raw, identity) {
				t.Errorf("gate/cases.jsonl still carries %q", identity)
			}
		}
	}
}
