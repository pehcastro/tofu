package secret

import (
	"slices"
	"strings"
	"testing"
)

var madeUp = identitiesOf(machine{
	home:    "C:/Users/Ada",
	user:    "Ada",
	email:   "ada@example.org",
	gitName: "adalovelace",
	workdir: "Q:/code/lab/tofu/bench",
})

func TestEveryFormOfEveryMachineValueIsScrubbed(t *testing.T) {
	for _, text := range []string{
		`C:\\Users\\Ada\\.env`, `C:\Users\Ada\.env`, "C:/Users/Ada/.env", "/c/Users/Ada/.env", "C--Users-Ada-proj",
		`Q:\\code\\lab`, `Q:\code\lab`, "Q:/code/lab", "/q/code/lab", "Q--code-lab-tofu",
		"mail ada@example.org", "handle adalovelace", "signed Ada",
	} {
		scrubbed := madeUp.Scrub(text)
		if scrubbed == text {
			t.Errorf("%q came through the scrub unchanged", text)
		}
		if leaks := madeUp.LeaksIn(scrubbed); len(leaks) > 0 {
			t.Errorf("%q scrubs to %q and still carries %q", text, scrubbed, leaks)
		}
	}
}

func TestTheUserNameInsideTheHomePathDoesNotBreakTheHomePlaceholder(t *testing.T) {
	if got := madeUp.Scrub(`C:\Users\Ada\go and C:/Users/Ada/go`); got != `C:\Users\owner\go and C:/Users/owner/go` {
		t.Fatalf("got %q", got)
	}
}

func TestAnEmptyMachineValueNeverBecomesASubstitution(t *testing.T) {
	table := identitiesOf(machine{home: "/home/ada"})
	for _, entry := range table {
		if entry.real == "" {
			t.Fatalf("an empty value is in the table as %+v, and ReplaceAll would insert its placeholder between every byte", entry)
		}
	}
	if got := table.Scrub("plain text"); got != "plain text" {
		t.Fatalf("plain text scrubbed to %q", got)
	}
}

func TestAWorkingDirectoryInsideHomeAddsNoProjectsEntry(t *testing.T) {
	for _, entry := range identitiesOf(machine{home: "/home/ada", workdir: "/home/ada/src/tofu"}) {
		if strings.Contains(entry.real, "src") {
			t.Fatalf("%q is in the table, and the home entry already covers it", entry.real)
		}
	}
}

func TestTheProjectsEntryIsNeverAnAncestorOfHome(t *testing.T) {
	reals := map[string]bool{}
	for _, entry := range identitiesOf(machine{home: "/home/ada", workdir: "/home/work/tofu"}) {
		reals[entry.real] = true
	}
	if reals["/home"] {
		t.Fatal("/home is in the table, so every path under it would be rewritten")
	}
	if !reals["/home/work"] {
		t.Fatalf("the projects directory /home/work is missing from %v", reals)
	}
}

func TestScrubbingTwiceGivesTheSameBytesAsScrubbingOnce(t *testing.T) {
	first := madeUp.Scrub("a path Q:/code/lab/bob owned by Ada, and the key is sk-or-v1-0123456789abcdef")
	if again := madeUp.Scrub(first); again != first {
		t.Fatalf("scrubbing %q again gave %q, so the replacement is not a fixed point", first, again)
	}
}

func TestNoTwoRealPathsCollapseIntoOneScrubbedPath(t *testing.T) {
	seen := map[string]string{}
	for _, entry := range madeUp {
		for _, tail := range []string{"", "/one", "/two", `\one`} {
			real := entry.real + tail
			clean := madeUp.Scrub(real)
			if earlier, taken := seen[clean]; taken && earlier != real {
				t.Errorf("%q and %q both scrub to %q", earlier, real, clean)
			}
			seen[clean] = real
		}
	}
}

func TestTheScrubReplacesRatherThanDeletesSoThePathShapeSurvives(t *testing.T) {
	for _, entry := range madeUp {
		if entry.scrubbed == "" {
			t.Errorf("%q scrubs to nothing, which changes what the state builder reads", entry.real)
		}
		for _, separator := range []string{"/", `\`} {
			if strings.Count(entry.real, separator) != strings.Count(entry.scrubbed, separator) {
				t.Errorf("%q scrubs to %q and the count of %q changed", entry.real, entry.scrubbed, separator)
			}
		}
	}
}

func TestScrubRemovesEveryCredentialMarkerThatLeaksInReports(t *testing.T) {
	for _, entry := range credentialSubstitutions {
		scrubbed := madeUp.Scrub("the key is " + entry.real + "0123456789abcdef and that is all of it")
		if leaks := CredentialsIn(scrubbed); len(leaks) > 0 {
			t.Errorf("%q survives its own scrub as %q", entry.real, leaks)
		}
		if !strings.Contains(scrubbed, entry.scrubbed) {
			t.Errorf("%q scrubbed to %q, which does not say what kind of credential was there", entry.real, scrubbed)
		}
	}
}

func TestNothingScrubsIntoSomethingAnotherScrubWouldCatch(t *testing.T) {
	for _, entry := range append(slices.Clone(madeUp), credentialSubstitutions...) {
		if leaks := madeUp.LeaksIn(entry.scrubbed); len(leaks) > 0 {
			t.Errorf("%q is a replacement and it carries %q, so a scrubbed fixture reads as leaking", entry.scrubbed, leaks)
		}
	}
}

func TestCredentialsInReportsOnlyTheCredentialPatternsAndLeaksInCarriesBoth(t *testing.T) {
	text := "Ada ran it and the key is sk-ant-oat01-0123456789 for one call"
	if credentials := CredentialsIn(text); len(credentials) != 1 || credentials[0] != "sk-ant-oat" {
		t.Fatalf("CredentialsIn reported %q, want the one anthropic oauth marker", credentials)
	}
	if leaks := madeUp.LeaksIn(text); len(leaks) != 2 {
		t.Fatalf("LeaksIn reported %q, want the identity and the credential marker", leaks)
	}
}
