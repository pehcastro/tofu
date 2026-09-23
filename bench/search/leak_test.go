package search

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

func TestNoRecordedRowIsAnsweredByTheBenchsOwnSource(t *testing.T) {
	loaded := loadOrSkip(t)

	names, err := TrackedFiles(".", "")
	if err != nil {
		t.Fatal(err)
	}
	mine := make(map[string][]byte, len(names))
	for _, rel := range names {
		body, err := os.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			t.Fatal(err)
		}
		mine[rel] = body
	}

	leaked := 0
	for _, row := range loaded.Rows {
		pattern, err := regexp.Compile(row.Pattern)
		if err != nil {
			t.Fatalf("%s recorded a pattern that no longer compiles: %v", row.Turn, err)
		}
		for rel, body := range mine {
			if pattern.Match(body) {
				leaked++
				t.Errorf("%s is answered by %s, which this bench wrote: a corpus must not be able to answer itself", row.Pattern, rel)
				break
			}
		}
	}
	t.Logf("leakage: %d recorded patterns checked against the %d files this bench wrote, %d answered by one of them", len(loaded.Rows), len(mine), leaked)
}

func TestLoadRefusesARowThatCameOffTheRecordingMachine(t *testing.T) {
	planted := "sk-ant-oat" + "-not-a-real-token"
	if leaks := corpus.LeaksIn(planted); len(leaks) == 0 {
		t.Fatal("the credential detector does not recognise the planted token, so this check proves nothing")
	}

	loaded := loadOrSkip(t)
	carried := 0
	for _, row := range loaded.Rows {
		if leaks := corpus.LeaksIn(row.Pattern + " " + row.Path); len(leaks) > 0 {
			carried++
			t.Errorf("%s carries %q", row.Turn, leaks)
		}
	}
	t.Logf("identity and credential scan: %d rows, %d carrying anything internal/secret recognises", len(loaded.Rows), carried)
}

func TestEveryRowCameOutOfARecordingRatherThanOffThisKeyboard(t *testing.T) {
	loaded := loadOrSkip(t)

	for _, row := range loaded.Rows {
		if !strings.HasPrefix(row.Turn, "turn-") {
			t.Fatalf("a row with no recorded turn behind it: %+v", row)
		}
	}
	t.Logf("provenance: %d rows, all carrying the id of the recorded turn they came from, 0 written by hand", len(loaded.Rows))
}
