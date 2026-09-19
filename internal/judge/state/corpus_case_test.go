package state

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const gateCasesPath = "../../../bench/corpus/gate/cases.jsonl"

type recordedContext struct {
	UserRecentMessages      []string         `json:"user_recent_messages"`
	FlaggedUntrustedContent *ToolGateFlagged `json:"flagged_untrusted_content"`
}

type recordedState struct {
	Agent   string          `json:"agent"`
	Tool    string          `json:"tool"`
	Input   map[string]any  `json:"input"`
	Cwd     string          `json:"cwd"`
	Context recordedContext `json:"context"`
}

type recordedCase struct {
	ID    string        `json:"id"`
	Label string        `json:"label"`
	State recordedState `json:"state"`
}

func gateCases(t *testing.T) map[string]recordedCase {
	t.Helper()
	file, err := os.Open(gateCasesPath)
	if err != nil {
		t.Fatalf("open the gate corpus: %v", err)
	}
	defer func() { _ = file.Close() }()

	out := map[string]recordedCase{}
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for lines.Scan() {
		var one recordedCase
		if err := json.Unmarshal(lines.Bytes(), &one); err != nil {
			t.Fatalf("decode a gate corpus line: %v", err)
		}
		out[one.ID] = one
	}
	if err := lines.Err(); err != nil {
		t.Fatalf("read the gate corpus: %v", err)
	}
	return out
}

func recordedInput(t *testing.T, one recordedCase) ToolGateInput {
	t.Helper()
	raw, err := json.Marshal(one.State.Input)
	if err != nil {
		t.Fatalf("re-encode %s: %v", one.ID, err)
	}
	return ToolGateInput{
		Agent:          one.State.Agent,
		Tool:           one.State.Tool,
		Input:          one.State.Input,
		Cwd:            one.State.Cwd,
		ProjectDir:     recordedProjectDir(one.State.Cwd),
		ScratchDir:     recordedScratchDir(string(raw)),
		InputTruncated: strings.Contains(string(raw), recordedTruncationMark),
		Context: ToolGateContext{
			UserRecentMessages:      one.State.Context.UserRecentMessages,
			FlaggedUntrustedContent: one.State.Context.FlaggedUntrustedContent,
		},
	}
}

const recordedTruncationMark = " ...[truncated]"

const recordedRepoFolder = "/bob"

func recordedProjectDir(cwd string) string {
	slashed := strings.ReplaceAll(cwd, "\\", "/")
	cut := strings.Index(strings.ToLower(slashed), recordedRepoFolder+"/")
	if cut < 0 {
		return slashed
	}
	return slashed[:cut+len(recordedRepoFolder)]
}

const recordedScratchFolder = "/scratchpad"

func recordedScratchDir(state string) string {
	slashed := strings.ReplaceAll(strings.ReplaceAll(state, `\\`, "/"), "\\", "/")
	cut := strings.Index(slashed, recordedScratchFolder)
	if cut < 0 {
		return ""
	}
	start := strings.LastIndexAny(slashed[:cut], "\"' ") + 1
	return slashed[start : cut+len(recordedScratchFolder)]
}
