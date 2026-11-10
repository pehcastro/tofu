package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type prediction struct {
	Task         string             `json:"task"`
	WrittenAt    time.Time          `json:"written_at"`
	PromptSHA256 string             `json:"prompt_sha256"`
	P50S         float64            `json:"p50_s"`
	P80LowS      float64            `json:"p80_low_s"`
	P80HighS     float64            `json:"p80_high_s"`
	P90S         float64            `json:"p90_s"`
	SelfGuessS   float64            `json:"self_guess_s"`
	PhaseShare   map[string]float64 `json:"phase_share"`
}

func taskDir(task string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(here(), "testdata", "tasks"))
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.IsDir() && e.Name() == task }) {
		return "", fmt.Errorf("no task %q under testdata/tasks", task)
	}
	return filepath.Join(here(), "testdata", "tasks", task), nil
}

func loadPrediction(dir, task string, prompt []byte, start time.Time) (prediction, error) {
	path := filepath.Join(dir, task+".json")
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return prediction{}, fmt.Errorf("no prediction at %s: predict before running", path)
	}
	if err != nil {
		return prediction{}, err
	}
	var p prediction
	if err := json.Unmarshal(body, &p); err != nil {
		return prediction{}, fmt.Errorf("prediction %s: %w", path, err)
	}
	sum := sha256.Sum256(prompt)
	switch {
	case p.Task != task:
		return prediction{}, fmt.Errorf("prediction %s is for task %q, not %q", path, p.Task, task)
	case p.PromptSHA256 != hex.EncodeToString(sum[:]):
		return prediction{}, fmt.Errorf("prediction %s hashes prompt %s, the task's prompt.md hashes %s", path, p.PromptSHA256, hex.EncodeToString(sum[:]))
	case p.WrittenAt.IsZero() || !p.WrittenAt.Before(start):
		return prediction{}, fmt.Errorf("prediction %s was written at %s, not before the run start %s", path, p.WrittenAt.Format(time.RFC3339), start.Format(time.RFC3339))
	case p.P50S <= 0 || p.SelfGuessS <= 0 || p.P80LowS <= 0 || p.P80LowS > p.P80HighS:
		return prediction{}, fmt.Errorf("prediction %s needs p50_s, self_guess_s and 0 < p80_low_s <= p80_high_s", path)
	}
	return p, nil
}
