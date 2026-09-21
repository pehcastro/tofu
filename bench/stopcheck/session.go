package stopcheck

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofu/bench/corpus"
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
		recorded, err := corpus.ReadTurn(filepath.Join(dir, name))
		if err != nil {
			skipped = append(skipped, Skipped{File: name, Why: err.Error()})
			continue
		}
		turns = append(turns, convertTurn(recorded))
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].ID < turns[j].ID })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].File < skipped[j].File })
	return turns, skipped, nil
}

func convertTurn(recorded corpus.RecordedTurn) Turn {
	turn := Turn{ID: recorded.ID, Task: recorded.Task}
	for _, step := range recorded.Steps {
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
	return turn
}
