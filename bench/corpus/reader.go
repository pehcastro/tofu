package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Schema string

const (
	SchemaSingleFile  Schema = "single file"
	SchemaHeaderJSONL Schema = "header and jsonl"
)

type RecordedCall struct {
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args,omitempty"`
	Error       string          `json:"error,omitempty"`
	ResultBytes int64           `json:"result_bytes,omitempty"`
}

type RecordedStep struct {
	Index     int            `json:"index"`
	ToolCalls []RecordedCall `json:"tool_calls,omitempty"`
}

type RecordedTurn struct {
	ID          string         `json:"id"`
	Task        string         `json:"task"`
	At          time.Time      `json:"at"`
	Steps       []RecordedStep `json:"steps"`
	WallClockMS int64          `json:"wall_clock_ms"`
}

type Turn struct {
	RecordedTurn
	Schema Schema
}

type SkippedTurn struct {
	Path   string
	Reason string
}

type Walked struct {
	Dir         string
	EntryCount  int
	Turns       []Turn
	Skipped     []SkippedTurn
	WalkElapsed time.Duration
}

func scrubTurn(recorded RecordedTurn) RecordedTurn {
	recorded.Task = Scrub(recorded.Task)
	for i, step := range recorded.Steps {
		for j, call := range step.ToolCalls {
			if len(call.Args) > 0 {
				call.Args = json.RawMessage(Scrub(string(call.Args)))
			}
			call.Error = Scrub(call.Error)
			recorded.Steps[i].ToolCalls[j] = call
		}
	}
	return recorded
}

func finishedTurn(recorded RecordedTurn, place string) (RecordedTurn, error) {
	if len(recorded.Steps) == 0 || recorded.WallClockMS == 0 {
		return RecordedTurn{}, fmt.Errorf("bench/corpus: %s carries no step and no wall clock", place)
	}
	return scrubTurn(recorded), nil
}

func ReadTurn(path string) (RecordedTurn, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RecordedTurn{}, err
	}
	var recorded RecordedTurn
	if err := json.Unmarshal(data, &recorded); err != nil {
		return RecordedTurn{}, fmt.Errorf("bench/corpus: %s is not a recorded turn: %w", path, err)
	}
	return finishedTurn(recorded, path)
}

type jsonlLine struct {
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"`
}

func ReadTurnDir(dir string) (RecordedTurn, error) {
	body, err := os.ReadFile(filepath.Join(dir, "body.jsonl"))
	if err != nil {
		return RecordedTurn{}, err
	}
	var recorded RecordedTurn
	outcomeFound := false
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var entry jsonlLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/body.jsonl is not a recorded line: %w", dir, err)
		}
		switch entry.Kind {
		case "step":
			var step RecordedStep
			if err := json.Unmarshal(entry.Body, &step); err != nil {
				return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/body.jsonl step is not the expected shape: %w", dir, err)
			}
			recorded.Steps = append(recorded.Steps, step)
		case "outcome":
			if err := json.Unmarshal(entry.Body, &recorded); err != nil {
				return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/body.jsonl outcome is not the expected shape: %w", dir, err)
			}
			outcomeFound = true
		}
	}
	if !outcomeFound {
		return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/body.jsonl carries no outcome line, so it has no wall clock", dir)
	}
	return finishedTurn(recorded, dir)
}

func WalkSessions(dir string) (Walked, error) {
	started := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Walked{}, err
	}
	walked := Walked{Dir: dir, EntryCount: len(entries)}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			recorded, err := ReadTurnDir(filepath.Join(dir, name))
			if err != nil {
				walked.Skipped = append(walked.Skipped, SkippedTurn{Path: name, Reason: err.Error()})
				continue
			}
			walked.Turns = append(walked.Turns, Turn{RecordedTurn: recorded, Schema: SchemaHeaderJSONL})
			continue
		}
		if filepath.Ext(name) != ".json" {
			walked.Skipped = append(walked.Skipped, SkippedTurn{Path: name, Reason: "not a .json file"})
			continue
		}
		recorded, err := ReadTurn(filepath.Join(dir, name))
		if err != nil {
			walked.Skipped = append(walked.Skipped, SkippedTurn{Path: name, Reason: err.Error()})
			continue
		}
		walked.Turns = append(walked.Turns, Turn{RecordedTurn: recorded, Schema: SchemaSingleFile})
	}
	sort.Slice(walked.Turns, func(i, j int) bool { return walked.Turns[i].ID < walked.Turns[j].ID })
	walked.WalkElapsed = time.Since(started)
	return walked, nil
}
