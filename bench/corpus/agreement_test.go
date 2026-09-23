package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/secret"
)

type frozenSubstitution struct {
	real     string
	scrubbed string
}

var frozenIdentities = []frozenSubstitution{
	{"pehcastro@gmail.com", "owner@example.com"},
	{"F--localhost-ephem-sh-bob", "R--work-ephem-sh-bob"},
	{"C:/Users/Luiz", "C:/Users/owner"},
	{"F:/localhost", "R:/work"},
	{"/f/localhost", "/r/work"},
	{"F:/", "R:/"},
	{"Luiz", "owner"},
	{"pehcastro", "owner-handle"},
}

var frozenSeparators = []string{`\\`, `\`, "/"}

var frozenCredentials = []frozenSubstitution{
	{"sk-or-v1-", "redacted-openrouter-key-"},
	{"sk-ant-api", "redacted-anthropic-api-key"},
	{"sk-ant-oat", "redacted-anthropic-oauth-token"},
	{"sk-proj-", "redacted-openai-key-"},
	{"ghp_", "redacted-github-token-"},
	{"gho_", "redacted-github-oauth-token-"},
	{"github_pat_", "redacted-github-pat-"},
	{"AKIA", "redacted-aws-key-"},
	{"xoxb-", "redacted-slack-bot-token-"},
	{"xoxp-", "redacted-slack-user-token-"},
	{"eyJhbGciOi", "redacted-jwt-algorithm-"},
	{"eyJ0eXAiOi", "redacted-jwt-type-"},
	{"-----BEGIN ", "redacted-pem-block "},
}

func frozenScrub(text string) string {
	for _, substitution := range frozenIdentities {
		for _, separator := range frozenSeparators {
			from := strings.ReplaceAll(substitution.real, "/", separator)
			to := strings.ReplaceAll(substitution.scrubbed, "/", separator)
			text = strings.ReplaceAll(text, from, to)
		}
	}
	for _, substitution := range frozenCredentials {
		text = strings.ReplaceAll(text, substitution.real, substitution.scrubbed)
	}
	return text
}

func frozenLeaksIn(text string) []string {
	var found []string
	for _, substitution := range frozenIdentities {
		for _, separator := range frozenSeparators {
			identity := strings.ReplaceAll(substitution.real, "/", separator)
			if strings.Contains(text, identity) {
				found = append(found, identity)
				break
			}
		}
	}
	for _, substitution := range frozenCredentials {
		if strings.Contains(text, substitution.real) {
			found = append(found, substitution.real)
		}
	}
	return found
}

func TestTheMovedDetectorAgreesWithTheOneThatProvedTheFixturesClean(t *testing.T) {
	fixtures := recordedFixtures(t)
	if len(fixtures) == 0 {
		t.Fatal("the walk found no recorded fixtures, so the agreement proved nothing")
	}
	t.Logf("comparing the frozen detector against internal/secret over %d recorded fixtures", len(fixtures))
	for _, relative := range fixtures {
		raw, err := os.ReadFile(filepath.Join(repositoryRootFromThisPackage, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if secret.Scrub(text) != frozenScrub(text) {
			t.Errorf("%s scrubs to different bytes under the moved detector", relative)
		}
		if strings.Join(secret.LeaksIn(text), ",") != strings.Join(frozenLeaksIn(text), ",") {
			t.Errorf("%s reports a different leak list under the moved detector", relative)
		}
	}
}

func TestTheMovedDetectorAgreesWithTheFrozenOneOnEveryPatternItCarries(t *testing.T) {
	var planted []string
	for _, substitution := range append(append([]frozenSubstitution{}, frozenIdentities...), frozenCredentials...) {
		for _, separator := range frozenSeparators {
			marker := strings.ReplaceAll(substitution.real, "/", separator)
			planted = append(planted, marker, "before "+marker+"0123456789abcdef after", substitution.scrubbed)
		}
	}
	for _, text := range planted {
		if secret.Scrub(text) != frozenScrub(text) {
			t.Errorf("the two detectors scrub a planted marker differently")
		}
		if strings.Join(secret.LeaksIn(text), ",") != strings.Join(frozenLeaksIn(text), ",") {
			t.Errorf("the two detectors report different leaks for a planted marker")
		}
	}
	t.Logf("compared %d planted strings across %d frozen patterns", len(planted), len(frozenIdentities)+len(frozenCredentials))
}
