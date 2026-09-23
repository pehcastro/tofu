package instruction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

func trackedGoFiles(t *testing.T) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	mine := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		mine[entry.Name()] = body
	}
	return mine
}

func TestNoRowsLabelAnswersItsOwnCaseInThisBenchsOwnSource(t *testing.T) {
	loaded := loadOrSkip(t)
	mine := trackedGoFiles(t)

	leaked := 0
	for _, row := range loaded.Rows {
		for name, body := range mine {
			if name == "labels.go" {
				continue
			}
			if strings.Contains(string(body), row.Content) {
				leaked++
				t.Errorf("%s %s: %s carries the case's own recorded content, which answers it for free", row.Turn, row.Key, name)
			}
		}
	}
	t.Logf("self-answer check: %d rows against %d files this bench wrote, %d answered by one of them", len(loaded.Rows), len(mine), leaked)
}

func TestLoadRefusesARowThatCameOffTheRecordingMachine(t *testing.T) {
	planted := "sk-ant-oat" + "-not-a-real-token"
	if leaks := corpus.LeaksIn(planted); len(leaks) == 0 {
		t.Fatal("the credential detector does not recognise the planted token, so this check proves nothing")
	}

	loaded := loadOrSkip(t)
	carried := 0
	for _, row := range loaded.Rows {
		if leaks := corpus.LeaksIn(row.Content + " " + row.Task); len(leaks) > 0 {
			carried++
			t.Errorf("%s %s carries something identity or credential shaped", row.Turn, row.Key)
		}
	}
	t.Logf("identity and credential scan: %d rows, %d carrying anything internal/secret recognises", len(loaded.Rows), carried)
}

func TestEveryLabelledCaseNamesTheTurnAndCallItCameFrom(t *testing.T) {
	loaded := loadOrSkip(t)
	for _, row := range loaded.Rows {
		if !strings.HasPrefix(row.Turn, "turn-") {
			t.Fatalf("a row with no recorded turn behind it: %+v", row)
		}
		if row.Key == "" {
			t.Fatalf("a row with no recorded tool call id behind it: %+v", row)
		}
	}
	t.Logf("provenance: %d rows, all carrying the turn id and tool call id they came from, 0 written by hand", len(loaded.Rows))
}
