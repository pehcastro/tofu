package secret

import (
	"strings"
	"testing"
)

func TestScrubbingTwiceGivesTheSameBytesAsScrubbingOnce(t *testing.T) {
	raw := "a path F:/localhost/ephem-sh/bob owned by Luiz, and the key is sk-or-v1-0123456789abcdef"
	first := Scrub(raw)
	if second := Scrub(raw); first != second {
		t.Fatal("two runs of Scrub over the same input disagree, so a case recorded tomorrow would not line up with one recorded today")
	}
	if again := Scrub(first); again != first {
		t.Fatal("scrubbing an already scrubbed text changed it, so the replacement is not a fixed point")
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

func TestScrubRemovesEveryIdentityThatLeaksInReports(t *testing.T) {
	for _, substitution := range identitySubstitutions {
		for _, separator := range separatorsInOrderOfLength {
			identity := strings.ReplaceAll(substitution.real, "/", separator)
			if leaks := LeaksIn(Scrub(identity)); len(leaks) > 0 {
				t.Errorf("%q survives its own scrub as %q, so the two lists have drifted apart", identity, leaks)
			}
		}
	}
}

func TestScrubRemovesEveryCredentialMarkerThatLeaksInReports(t *testing.T) {
	for _, substitution := range credentialSubstitutions {
		planted := "the key is " + substitution.real + "0123456789abcdef and that is all of it"
		scrubbed := Scrub(planted)
		if leaks := LeaksIn(scrubbed); len(leaks) > 0 {
			t.Errorf("%q survives its own scrub as %q, so Scrub and LeaksIn have drifted apart", substitution.real, leaks)
		}
		if !strings.Contains(scrubbed, substitution.scrubbed) {
			t.Errorf("%q scrubbed to %q, which does not say what kind of credential was there", substitution.real, scrubbed)
		}
		if again := Scrub(scrubbed); again != scrubbed {
			t.Errorf("scrubbing %q twice gave %q, so the replacement is not a fixed point", substitution.real, again)
		}
	}
}

func TestNothingScrubsIntoSomethingAnotherScrubWouldCatch(t *testing.T) {
	for _, substitution := range append(append([]substitution{}, identitySubstitutions...), credentialSubstitutions...) {
		if leaks := LeaksIn(substitution.scrubbed); len(leaks) > 0 {
			t.Errorf("%q is a replacement and it carries %q, so a scrubbed fixture reads as leaking", substitution.scrubbed, leaks)
		}
	}
}

func TestCredentialsInReportsOnlyTheCredentialPatternsAndLeaksInCarriesBoth(t *testing.T) {
	text := "Luiz ran it and the key is sk-ant-oat01-0123456789 for one call"
	credentials := CredentialsIn(text)
	if len(credentials) != 1 || credentials[0] != "sk-ant-oat" {
		t.Fatalf("CredentialsIn reported %q, want the one anthropic oauth marker", credentials)
	}
	if leaks := LeaksIn(text); len(leaks) != 2 {
		t.Fatalf("LeaksIn reported %q, want the identity and the credential marker", leaks)
	}
	if identityOnly := CredentialsIn("Luiz ran it under F:/localhost"); len(identityOnly) != 0 {
		t.Fatalf("CredentialsIn reported %q over text with an identity and no credential", identityOnly)
	}
}
