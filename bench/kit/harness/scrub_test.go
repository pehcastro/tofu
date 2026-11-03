package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

const tofuV1Testdata = "testdata/tofu-v1"

var (
	homeDirectoryPath = regexp.MustCompile(`(?i)[a-z]:[\\/]{1,2}users[\\/]{1,2}[^\\/"\s]+|/home/[^/"\s]+`)
	accountIdentifier = regexp.MustCompile(`"(?:account|account_id|org_id|organization_id|user_id)"\s*:\s*(?:"[^"]+"|[1-9][0-9]*)`)
	unbrokenRun       = regexp.MustCompile(`[A-Za-z0-9_-]{24,}`)
)

func leaksIn(text string) []string {
	found := corpus.LeaksIn(text)
	found = append(found, homeDirectoryPath.FindAllString(text, -1)...)
	found = append(found, accountIdentifier.FindAllString(text, -1)...)
	for _, run := range unbrokenRun.FindAllString(text, -1) {
		if strings.ContainsAny(run, "abcdefghijklmnopqrstuvwxyz") &&
			strings.ContainsAny(run, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") &&
			strings.ContainsAny(run, "0123456789") {
			found = append(found, run)
		}
	}
	return found
}

func fixtureFiles(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(tofuV1Testdata)
	if err != nil {
		t.Fatalf("the committed tofu fixture is what this test exists to guard, and it could not be read: %v", err)
	}
	bodies := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("%s holds a directory %s, and the scrub only reads the files it can see", tofuV1Testdata, entry.Name())
		}
		body, err := os.ReadFile(filepath.Join(tofuV1Testdata, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		bodies[entry.Name()] = string(body)
	}
	if len(bodies) == 0 {
		t.Fatalf("%s is empty, so this test would pass while guarding nothing", tofuV1Testdata)
	}
	return bodies
}

func TestTheCommittedTofuFixtureCarriesNothingCredentialShaped(t *testing.T) {
	for name, body := range fixtureFiles(t) {
		if found := leaksIn(body); len(found) > 0 {
			t.Errorf("%s/%s carries %d thing(s) this fixture may never carry: %s",
				tofuV1Testdata, name, len(found), strings.Join(found, ", "))
			continue
		}
		t.Logf("%s/%s: %d bytes, nothing found", tofuV1Testdata, name, len(body))
	}
}

func TestTheScrubCatchesEveryKindOfPlantedSecret(t *testing.T) {
	clean := fixtureFiles(t)["session.json"]
	if clean == "" {
		t.Fatalf("%s/session.json is the recorded transcript and it is not there to plant into", tofuV1Testdata)
	}

	planted := []struct {
		kind   string
		secret string
	}{
		{"an openrouter key", `"note":"sk-or-v1-0123456789abcdef0123456789abcdef"`},
		{"an anthropic oauth token", `"note":"sk-ant-oat01-notarealtokenatall"`},
		{"an account id", `"account":881224`},
		{"a home directory path", `"note":"C:\\Users\\someone\\.env"`},
		{"an api-key-shaped string", `"note":"Zk3QpR7vTm2XbN9wLd4HsY6c"`},
	}

	for _, plant := range planted {
		found := leaksIn(clean + "\n" + plant.secret)
		if len(found) == 0 {
			t.Errorf("%s was planted in the fixture and the scrub found nothing", plant.kind)
			continue
		}
		t.Logf("%s: caught %s", plant.kind, strings.Join(found, ", "))
	}
}
