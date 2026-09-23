package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/secret"
)

const recordedSessionsFromThisPackage = "../../.tofu/sessions"

const valueCharacters = "-_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func readableBytesUnder(t *testing.T, path string) string {
	t.Helper()
	var joined strings.Builder
	err := filepath.WalkDir(path, func(inner string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(inner)
		if err != nil {
			return err
		}
		joined.Write(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return joined.String()
}

func valueCharactersAfter(text, marker string) int {
	longest := 0
	for rest := text; ; {
		at := strings.Index(rest, marker)
		if at < 0 {
			return longest
		}
		rest = rest[at+len(marker):]
		if run := len(rest) - len(strings.TrimLeft(rest, valueCharacters)); run > longest {
			longest = run
		}
	}
}

func TestHowManyRecordedTurnsOnThisMachineCarrySomethingCredentialShaped(t *testing.T) {
	entries, err := os.ReadDir(recordedSessionsFromThisPackage)
	if os.IsNotExist(err) {
		t.Skip("no .tofu/sessions on this machine, so there is nothing recorded to measure")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the sessions directory is empty, so the measurement proved nothing")
	}
	flagged, withValueShape := 0, 0
	for _, entry := range entries {
		text := readableBytesUnder(t, filepath.Join(recordedSessionsFromThisPackage, entry.Name()))
		markers := secret.CredentialsIn(text)
		if len(markers) == 0 {
			continue
		}
		flagged++
		for _, marker := range markers {
			run := valueCharactersAfter(text, marker)
			if run > 0 {
				withValueShape++
			}
			t.Logf("one recorded turn names the %s marker, followed by %d value characters", marker, run)
		}
	}
	t.Logf("%d recorded turns walked, %d carry a credential marker, %d of those have anything value shaped after it", len(entries), flagged, withValueShape)
}
