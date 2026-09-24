package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tofu/internal/recall"
)

var ErrNoWallClock = errors.New("bench/corpus: the recorded turn carries no wall clock")

type Schema string

const (
	SchemaSingleFile  Schema = "single file"
	SchemaHeaderJSONL Schema = "header and jsonl"
)

type RecordedCall struct {
	ID             string          `json:"id,omitempty"`
	Parent         string          `json:"parent,omitempty"`
	Author         string          `json:"author,omitempty"`
	Tool           string          `json:"tool"`
	Args           json.RawMessage `json:"args,omitempty"`
	Command        string          `json:"command,omitempty"`
	ExitCode       *int            `json:"exit_code,omitempty"`
	Error          string          `json:"error,omitempty"`
	ResultBytes    int64           `json:"result_bytes,omitempty"`
	RenderedBytes  int64           `json:"rendered_bytes,omitempty"`
	ResultHandle   string          `json:"result_handle,omitempty"`
	ResultHash     string          `json:"result_hash,omitempty"`
	GateDecisionID string          `json:"gate_decision_id,omitempty"`
}

func (c *RecordedCall) UnmarshalJSON(data []byte) error {
	type alias RecordedCall
	var lower alias
	if err := json.Unmarshal(data, &lower); err != nil {
		return err
	}
	*c = RecordedCall(lower)
	if c.ExitCode != nil && c.ResultBytes != 0 && c.RenderedBytes != 0 && c.ResultHash != "" {
		return nil
	}
	var upper struct {
		ExitCode      *int   `json:"ExitCode"`
		ResultBytes   int64  `json:"ResultBytes"`
		RenderedBytes int64  `json:"RenderedBytes"`
		ResultHash    string `json:"ResultHash"`
	}
	if err := json.Unmarshal(data, &upper); err != nil {
		return err
	}
	if c.ExitCode == nil {
		c.ExitCode = upper.ExitCode
	}
	if c.ResultBytes == 0 {
		c.ResultBytes = upper.ResultBytes
	}
	if c.RenderedBytes == 0 {
		c.RenderedBytes = upper.RenderedBytes
	}
	if c.ResultHash == "" {
		c.ResultHash = upper.ResultHash
	}
	return nil
}

type RecordedToolCallName struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type RecordedMessage struct {
	Role       string                 `json:"role,omitempty"`
	Content    string                 `json:"content,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	ToolCalls  []RecordedToolCallName `json:"tool_calls,omitempty"`
}

type RecordedStep struct {
	Index            int            `json:"index"`
	Attempt          int            `json:"attempt"`
	ToolCalls        []RecordedCall `json:"tool_calls,omitempty"`
	AssistantText    string         `json:"assistant_text,omitempty"`
	StopReason       string         `json:"stop_reason,omitempty"`
	PromptTokens     int            `json:"prompt_tokens,omitempty"`
	CompletionTokens int            `json:"completion_tokens,omitempty"`
	CacheReadTokens  *int           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int           `json:"cache_write_tokens,omitempty"`
}

func (s *RecordedStep) UnmarshalJSON(data []byte) error {
	type alias RecordedStep
	var lower alias
	if err := json.Unmarshal(data, &lower); err != nil {
		return err
	}
	*s = RecordedStep(lower)
	if s.AssistantText != "" && (s.PromptTokens != 0 || s.CompletionTokens != 0) && len(s.ToolCalls) > 0 {
		return nil
	}
	var upper struct {
		AssistantText    string         `json:"AssistantText"`
		PromptTokens     int            `json:"PromptTokens"`
		CompletionTokens int            `json:"CompletionTokens"`
		ToolCalls        []RecordedCall `json:"ToolCalls"`
	}
	if err := json.Unmarshal(data, &upper); err != nil {
		return err
	}
	if s.AssistantText == "" {
		s.AssistantText = upper.AssistantText
	}
	if s.PromptTokens == 0 && s.CompletionTokens == 0 {
		s.PromptTokens = upper.PromptTokens
		s.CompletionTokens = upper.CompletionTokens
	}
	if len(s.ToolCalls) == 0 {
		s.ToolCalls = upper.ToolCalls
	}
	return nil
}

type RecordedTurn struct {
	ID             string            `json:"id"`
	Task           string            `json:"task"`
	At             time.Time         `json:"at"`
	Steps          []RecordedStep    `json:"steps"`
	WallClockMS    int64             `json:"wall_clock_ms"`
	Outcome        string            `json:"outcome,omitempty"`
	ContextCeiling int               `json:"context_ceiling,omitempty"`
	ContextTarget  int               `json:"context_target,omitempty"`
	AutoCompaction string            `json:"auto_compaction,omitempty"`
	Budget         recall.Budget     `json:"budget,omitempty"`
	Account        int64             `json:"account,omitempty"`
	Messages       []RecordedMessage `json:"-"`
}

func (t *RecordedTurn) UnmarshalJSON(data []byte) error {
	type alias RecordedTurn
	shadow := struct {
		*alias
		Outcome json.RawMessage `json:"outcome"`
	}{alias: (*alias)(t)}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return err
	}
	var outcome string
	if json.Unmarshal(shadow.Outcome, &outcome) == nil {
		t.Outcome = outcome
	}
	return nil
}

type Turn struct {
	RecordedTurn
	Schema            Schema
	WallClockRecorded bool
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
	recorded.Outcome = Scrub(recorded.Outcome)
	recorded.AutoCompaction = Scrub(recorded.AutoCompaction)
	recorded.Budget.Source = Scrub(recorded.Budget.Source)
	recorded.Budget.WindowSource = Scrub(recorded.Budget.WindowSource)
	for i, step := range recorded.Steps {
		recorded.Steps[i].AssistantText = Scrub(step.AssistantText)
		recorded.Steps[i].StopReason = Scrub(step.StopReason)
		for j, call := range step.ToolCalls {
			if len(call.Args) > 0 {
				call.Args = json.RawMessage(Scrub(string(call.Args)))
			}
			call.Command = Scrub(call.Command)
			call.Error = Scrub(call.Error)
			call.GateDecisionID = Scrub(call.GateDecisionID)
			recorded.Steps[i].ToolCalls[j] = call
		}
	}
	for i, message := range recorded.Messages {
		recorded.Messages[i].Content = Scrub(message.Content)
	}
	return recorded
}

func finishedTurn(recorded RecordedTurn, place string) (RecordedTurn, error) {
	scrubbed := scrubTurn(recorded)
	if len(scrubbed.Steps) == 0 || scrubbed.WallClockMS == 0 {
		return scrubbed, fmt.Errorf("bench/corpus: %s carries no step and no wall clock: %w", place, ErrNoWallClock)
	}
	return scrubbed, nil
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
	Kind    string          `json:"kind"`
	Attempt int             `json:"attempt"`
	Body    json.RawMessage `json:"body"`
}

func ReadTurnDir(dir string) (RecordedTurn, error) {
	body, err := os.ReadFile(filepath.Join(dir, "body.jsonl"))
	if err != nil {
		return RecordedTurn{}, err
	}
	var recorded RecordedTurn
	if header, err := os.ReadFile(filepath.Join(dir, "header.json")); err == nil {
		if err := json.Unmarshal(header, &recorded); err != nil {
			return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/header.json is not the expected shape: %w", dir, err)
		}
	}
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
			step.Attempt = entry.Attempt
			recorded.Steps = append(recorded.Steps, step)
		case "message":
			var message RecordedMessage
			if err := json.Unmarshal(entry.Body, &message); err != nil {
				return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/body.jsonl message is not the expected shape: %w", dir, err)
			}
			recorded.Messages = append(recorded.Messages, message)
		case "outcome":
			if err := json.Unmarshal(entry.Body, &recorded); err != nil {
				return RecordedTurn{}, fmt.Errorf("bench/corpus: %s/body.jsonl outcome is not the expected shape: %w", dir, err)
			}
			outcomeFound = true
		}
	}
	if !outcomeFound {
		return scrubTurn(recorded), fmt.Errorf("bench/corpus: %s/body.jsonl carries no outcome line, so it has no wall clock: %w", dir, ErrNoWallClock)
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
		if !entry.IsDir() && filepath.Ext(name) != ".json" {
			walked.Skipped = append(walked.Skipped, SkippedTurn{Path: name, Reason: "not a .json file"})
			continue
		}
		schema, read := SchemaSingleFile, ReadTurn
		if entry.IsDir() {
			schema, read = SchemaHeaderJSONL, ReadTurnDir
		}
		recorded, err := read(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, ErrNoWallClock) {
			walked.Skipped = append(walked.Skipped, SkippedTurn{Path: name, Reason: err.Error()})
			continue
		}
		walked.Turns = append(walked.Turns, Turn{RecordedTurn: recorded, Schema: schema, WallClockRecorded: err == nil})
	}
	sort.Slice(walked.Turns, func(i, j int) bool { return walked.Turns[i].ID < walked.Turns[j].ID })
	walked.WalkElapsed = time.Since(started)
	return walked, nil
}
