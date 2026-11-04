package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/agent/loop"
	"tofu/internal/dummy"
)

const (
	turnThatRecordedNothing = "turn-18d69b47d7028fec.json"
	turnThatWroteTheFile    = "turn-18d690dd45903d58.json"
)

func readRecordedTurn(dir, name string) (loop.Row, error) {
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return loop.Row{}, err
	}
	var row loop.Row
	if err := json.Unmarshal(body, &row); err != nil {
		return loop.Row{}, fmt.Errorf("reading %s: %w", name, err)
	}
	return row, nil
}

func checkDoneReviewTurns(dir string) error {
	nothing, err := readRecordedTurn(dir, turnThatRecordedNothing)
	if err != nil {
		return err
	}
	wrote, err := readRecordedTurn(dir, turnThatWroteTheFile)
	if err != nil {
		return err
	}
	if len(nothing.Steps) != 0 {
		return fmt.Errorf("%s records %d steps, and the live review needs the turn that claimed a file with none", nothing.ID, len(nothing.Steps))
	}
	if len(wrote.Steps) == 0 || len(wrote.Steps[0].ToolCalls) == 0 || wrote.Steps[0].ToolCalls[0].Tool != "write" {
		return fmt.Errorf("%s opens with no write call, and the live review needs the turn that wrote the file", wrote.ID)
	}
	return nil
}

func TestRecordedTurnsTheLiveDoneReviewReadsAreCommitted(t *testing.T) {
	dir := dummy.Path("turns", "labelled")
	if err := checkDoneReviewTurns(dir); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s and %s read offline from %s", turnThatRecordedNothing, turnThatWroteTheFile, dir)
}

func TestRecordedTurnMissingFromACopyFailsTheCheck(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{turnThatRecordedNothing, turnThatWroteTheFile} {
		body, err := os.ReadFile(dummy.Path("turns", "labelled", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkDoneReviewTurns(dir); err != nil {
		t.Fatalf("the untouched copy already fails: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, turnThatWroteTheFile), filepath.Join(dir, "moved.json")); err != nil {
		t.Fatal(err)
	}
	err := checkDoneReviewTurns(dir)
	if err == nil {
		t.Fatal("the copy lost a labelled turn and the check still passed")
	}
	t.Logf("with %s renamed away: %v", turnThatWroteTheFile, err)
}
