package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var sourceRoots = []string{"bench", "library", "cmd", "interface", "internal"}

var fixtureDirectoryNames = map[string]bool{"testdata": true, "corpus": true, "answers": true}

const repositoryRootFromThisPackage = "../.."

func isRecordedFixture(relative string) bool {
	segments := strings.Split(relative, "/")
	inFixtureDirectory, underTestdata := false, false
	for _, segment := range segments[:len(segments)-1] {
		inFixtureDirectory = inFixtureDirectory || fixtureDirectoryNames[segment]
		underTestdata = underTestdata || segment == "testdata"
	}
	if !inFixtureDirectory {
		return false
	}
	return underTestdata || !strings.HasSuffix(relative, ".go")
}

func recordedFixtures(t *testing.T) []string {
	t.Helper()
	var fixtures []string
	for _, sourceRoot := range sourceRoots {
		err := filepath.WalkDir(filepath.Join(repositoryRootFromThisPackage, sourceRoot), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(repositoryRootFromThisPackage, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if isRecordedFixture(relative) {
				fixtures = append(fixtures, relative)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return fixtures
}

func TestEveryRecordedFixtureInTheTreeCarriesNothingOffTheRecordingMachine(t *testing.T) {
	fixtures := recordedFixtures(t)
	if len(fixtures) == 0 {
		t.Fatal("the walk found no recorded fixtures, so it proved nothing about the tree")
	}
	t.Logf("walked %d recorded fixtures under %v", len(fixtures), sourceRoots)
	for _, relative := range fixtures {
		raw, err := os.ReadFile(filepath.Join(repositoryRootFromThisPackage, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		if leaks := LeaksIn(string(raw)); len(leaks) > 0 {
			t.Errorf("%s carries %q, which came off the recording machine", relative, leaks)
		}
	}
}

func TestTheFixtureRuleIsTheOneAPersonWouldPredict(t *testing.T) {
	recorded := []string{
		"bench/corpus/gate/cases.jsonl",
		"bench/stopcheck/corpus/turn-18d68bcceb3d56e8.json",
		"internal/turn/testdata/subscription-end-turn.sse",
		"bench/cost/answers/heldout-2026-09-19.jsonl",
		"bench/cost/testdata/spendsum/main.go",
		"interface/tui/testdata/session-80x24.golden",
	}
	notRecorded := []string{
		"bench/corpus/scrub.go",
		"bench/corpus/gate.go",
		"internal/turn/turn.go",
		"bench/report/api-2026-09-18.md",
		"CHANGELOG.md",
	}
	for _, path := range recorded {
		if !isRecordedFixture(path) {
			t.Errorf("%s is a recorded fixture and the rule missed it", path)
		}
	}
	for _, path := range notRecorded {
		if isRecordedFixture(path) {
			t.Errorf("%s is source or a report and the rule claimed it", path)
		}
	}
}
