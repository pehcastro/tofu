package stopcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Call struct {
	Tool         string
	Command      string
	Failed       bool
	GateDecision string
}

type Step struct {
	Index         int
	AssistantText string
	StopReason    string
	Calls         []Call
}

type Turn struct {
	ID    string
	Task  string
	Steps []Step
}

type Skipped struct {
	File string
	Why  string
}

type wireCall struct {
	Tool           string `json:"tool"`
	Command        string `json:"command"`
	ExitCode       *int   `json:"exit_code"`
	Error          string `json:"error"`
	GateDecisionID string `json:"gate_decision_id"`
}

type wireStep struct {
	Index         int        `json:"index"`
	AssistantText string     `json:"assistant_text"`
	StopReason    string     `json:"stop_reason"`
	ToolCalls     []wireCall `json:"tool_calls"`
}

type wireTurn struct {
	ID      string          `json:"id"`
	Task    string          `json:"task"`
	Outcome json.RawMessage `json:"outcome"`
	Steps   []wireStep      `json:"steps"`
}

func ReadSessions(dir string) ([]Turn, []Skipped, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var turns []Turn
	var skipped []Skipped
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, err
		}
		turn, why := decodeTurn(raw)
		if why != "" {
			skipped = append(skipped, Skipped{File: name, Why: why})
			continue
		}
		turns = append(turns, turn)
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].ID < turns[j].ID })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].File < skipped[j].File })
	return turns, skipped, nil
}

func decodeTurn(raw []byte) (Turn, string) {
	var wire wireTurn
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Turn{}, fmt.Sprintf("the file is not a turn row this build can read: %v", err)
	}
	var outcomeText string
	if err := json.Unmarshal(wire.Outcome, &outcomeText); err != nil {
		return Turn{}, fmt.Sprintf("outcome is %s, a number written by a schema older than this one, so no field in the file can be trusted to mean what it means today", wire.Outcome)
	}
	turn := Turn{ID: wire.ID, Task: wire.Task}
	for _, step := range wire.Steps {
		converted := Step{Index: step.Index, AssistantText: step.AssistantText, StopReason: step.StopReason}
		for _, call := range step.ToolCalls {
			converted.Calls = append(converted.Calls, Call{
				Tool:         call.Tool,
				Command:      call.Command,
				Failed:       call.Error != "" || (call.ExitCode != nil && *call.ExitCode != 0),
				GateDecision: call.GateDecisionID,
			})
		}
		turn.Steps = append(turn.Steps, converted)
	}
	return turn, ""
}
