package stopcheck

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const benchRoot = ".."

var repeatsLeftForTheirOwners = map[string]string{
	"harness/testdata/tofu-v1/session.json and stopcheck/corpus/turn-18d69bfed2bf52a0.json": "decided by TOFU-458 and kept: tofu-v1 is a named fixture pairing this transcript with its 91 ledger rows, and bench/cmd/harness.go ships it as the default transcript for bench harness --offline. a runtime default reading out of a test corpus is worse than the second copy. both provenance files name the twin",
}

func recordingsByTurnID(t *testing.T, root string) map[string][]string {
	t.Helper()
	byID := map[string][]string{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(name, ".json") {
			return err
		}
		body, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		var recorded struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(body, &recorded) != nil || !strings.HasPrefix(recorded.ID, "turn-") {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		byID[recorded.ID] = append(byID[recorded.ID], filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return byID
}

func repeatedRecordings(byID map[string][]string) []string {
	var repeats []string
	for _, paths := range byID {
		if len(paths) < 2 {
			continue
		}
		slices.Sort(paths)
		repeats = append(repeats, strings.Join(paths, " and "))
	}
	slices.Sort(repeats)
	return repeats
}

func TestNoRecordedTurnIsCommittedTwiceUnderBench(t *testing.T) {
	byID := recordingsByTurnID(t, benchRoot)
	repeats := repeatedRecordings(byID)
	for _, repeat := range repeats {
		why, known := repeatsLeftForTheirOwners[repeat]
		if !known {
			t.Errorf("one recorded turn is committed twice, at %s, and two copies drift apart with nothing noticing. one directory keeps it and the other reads from there", repeat)
			continue
		}
		t.Logf("known repeat, %s: %s", repeat, why)
	}
	for repeat, why := range repeatsLeftForTheirOwners {
		if !slices.Contains(repeats, repeat) {
			t.Logf("no longer a repeat, drop it from the list: %s, which was left because %s", repeat, why)
		}
	}
	t.Logf("%d distinct recorded turns under %s, %d of them in more than one file", len(byID), benchRoot, len(repeats))
}

func TestARecordingCopiedIntoASecondDirectoryIsRefused(t *testing.T) {
	root := t.TempDir()
	body, err := os.ReadFile(filepath.Join(corpusDir, "turn-18d6a067066b0a60.json"))
	if err != nil {
		t.Fatal(err)
	}
	drifted := append(slices.Clone(body), '\n')
	for dir, content := range map[string][]byte{"keeper": body, "copy": drifted} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "session.json"), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repeats := repeatedRecordings(recordingsByTurnID(t, root))
	planted := "copy/session.json and keeper/session.json"
	if len(repeats) != 1 || repeats[0] != planted {
		t.Fatalf("the planted copy was not caught, the walk found %v", repeats)
	}
	t.Logf("caught by the turn id both files carry, under a name neither shares with the corpus and with one byte of drift between them: %s", planted)
}
