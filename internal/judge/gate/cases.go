package gate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"tofu/internal/sys"
)

type BlockCase struct {
	ID      string
	Tool    string
	Input   json.RawMessage
	Cwd     string
	Task    string
	Outcome string
	At      time.Time
}

type caseContext struct {
	UserRecentMessages      []string  `json:"user_recent_messages"`
	FlaggedUntrustedContent *struct{} `json:"flagged_untrusted_content"`
}

type caseState struct {
	Agent   string      `json:"agent"`
	Tool    string      `json:"tool"`
	Input   any         `json:"input"`
	Cwd     string      `json:"cwd"`
	Context caseContext `json:"context"`
}

type caseLine struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Recorded    bool      `json:"recorded"`
	RecordedAt  string    `json:"recorded_at"`
	StateShape  string    `json:"state_shape"`
	Label       string    `json:"label"`
	LabelBy     string    `json:"label_by"`
	LabelNote   string    `json:"label_note"`
	HookRefusal string    `json:"hook_refusal"`
	State       caseState `json:"state"`
}

func CasesFile(projectState string) string {
	return filepath.Join(projectState, "gate", "cases.jsonl")
}

func (r BlockCase) AppendTo(file string) error {
	var input any = string(r.Input)
	if json.Valid(r.Input) {
		input = r.Input
	}
	line, err := json.Marshal(caseLine{ID: r.ID, Origin: "tofu-gate", Recorded: true, RecordedAt: r.At.UTC().Format(time.RFC3339Nano),
		StateShape: "tool_gate.request", Label: "block", LabelBy: "agent", LabelNote: r.Outcome,
		State: caseState{Agent: "tofu", Tool: r.Tool, Input: input, Cwd: r.Cwd, Context: caseContext{UserRecentMessages: []string{r.Task}}}})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	cases, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = cases.Write(append(line, '\n'))
	return errors.Join(err, cases.Close())
}

func HomeCasesFiles() ([]string, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	projects := filepath.Join(home, sys.ProjectsDirName)
	entries, err := os.ReadDir(projects)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		file := CasesFile(filepath.Join(projects, entry.Name()))
		if _, err := os.Stat(file); err == nil {
			files = append(files, file)
		}
	}
	return files, nil
}
