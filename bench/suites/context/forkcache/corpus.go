package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

var recordedTurns = []string{
	"bench/suites/context/recall/testdata/recorded-turn.json",
	"bench/stopcheck/corpus/turn-18d6a27c7dfb1644.json",
	"bench/stopcheck/corpus/turn-18d69bfed2bf52a0.json",
	"bench/stopcheck/corpus/turn-18d6a5df2caeac68.json",
}

type recordedCall struct {
	Tool          string          `json:"tool"`
	Args          json.RawMessage `json:"args"`
	Command       string          `json:"command"`
	RenderedBytes int             `json:"rendered_bytes"`
}

type recordedTurn struct {
	ID    string `json:"id"`
	Task  string `json:"task"`
	Steps []struct {
		AssistantText string         `json:"assistant_text"`
		ToolCalls     []recordedCall `json:"tool_calls"`
	} `json:"steps"`
}

type mockCall struct {
	tool   string
	target string
	bytes  int
}

type mockStep struct {
	saidBytes int
	calls     []mockCall
}

type script struct {
	name      string
	taskBytes int
	steps     []mockStep
}

func readCorpus(root string) ([]script, error) {
	var scripts []script
	for _, path := range recordedTurns {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		var turn recordedTurn
		if err := json.Unmarshal(data, &turn); err != nil {
			return nil, fmt.Errorf("%s is not a recorded turn: %w", path, err)
		}
		played, err := mocked(turn)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		scripts = append(scripts, played)
	}
	return scripts, nil
}

func mocked(turn recordedTurn) (script, error) {
	names := map[string]string{}
	named := func(kind, real string) string {
		if name, seen := names[kind+real]; seen {
			return name
		}
		names[kind+real] = "mock/" + kind + strconv.Itoa(len(names)+1) + ".txt"
		return names[kind+real]
	}
	played := script{name: turn.ID, taskBytes: len(turn.Task)}
	for _, step := range turn.Steps {
		mock := mockStep{saidBytes: len(step.AssistantText)}
		for _, call := range step.ToolCalls {
			var args struct {
				Path    string `json:"path"`
				Handle  string `json:"handle"`
				Offset  int    `json:"offset"`
				Content string `json:"content"`
			}
			_ = json.Unmarshal(call.Args, &args)
			switch call.Tool {
			case "read":
				mock.calls = append(mock.calls, mockCall{tool: "read", target: named("file", args.Path), bytes: call.RenderedBytes})
			case "artifact_fetch":
				mock.calls = append(mock.calls, mockCall{tool: "read", target: named("fetched", args.Handle+strconv.Itoa(args.Offset)), bytes: call.RenderedBytes})
			case "bash":
				mock.calls = append(mock.calls, mockCall{tool: "bash", target: named("output", call.Command), bytes: call.RenderedBytes})
			case "write":
				mock.calls = append(mock.calls, mockCall{tool: "write", target: named("file", args.Path), bytes: len(args.Content)})
			case "edit":
				mock.calls = append(mock.calls, mockCall{tool: "write", target: named("file", args.Path), bytes: len(call.Args)})
			case "glob", "grep":
				mock.calls = append(mock.calls, mockCall{tool: call.Tool})
			default:
				return script{}, fmt.Errorf("a %s call has no mock in this replay", call.Tool)
			}
		}
		played.steps = append(played.steps, mock)
	}
	return played, nil
}
