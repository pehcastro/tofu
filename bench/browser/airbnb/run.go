package airbnb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Arm string

const (
	ArmGoal     Arm = "goal"
	ArmSteps    Arm = "steps"
	ArmSubagent Arm = "subagent"
)

type Conditions struct {
	Date         string `json:"date"`
	Machine      string `json:"machine"`
	Credential   string `json:"credential"`
	Wire         string `json:"wire"`
	MainBuild    string `json:"main_build"`
	BrowserBuild string `json:"browser_build"`
}

type Tab struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type Run struct {
	Arm           Arm        `json:"arm"`
	Conditions    Conditions `json:"conditions"`
	WallMS        int64      `json:"wall_ms"`
	MainTokens    int        `json:"main_tokens"`
	BrowserTokens int        `json:"browser_tokens"`
	Repeated      int        `json:"repeated"`
	Refused       int        `json:"refused"`
	Tabs          []Tab      `json:"tabs"`
	Visits        []string   `json:"visits"`
	Snapshot      string     `json:"-"`
	Report        string     `json:"-"`
}

func LoadRun(dir string) (Run, error) {
	var run Run
	raw, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return run, err
	}
	if err := json.Unmarshal(raw, &run); err != nil {
		return run, fmt.Errorf("%s: %w", dir, err)
	}
	switch run.Arm {
	case ArmGoal, ArmSteps, ArmSubagent:
	default:
		return run, fmt.Errorf("%s: unknown arm %q", dir, run.Arm)
	}
	snapshot, err := os.ReadFile(filepath.Join(dir, "snapshot.txt"))
	if err != nil {
		return run, err
	}
	report, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	run.Snapshot, run.Report = string(snapshot), string(report)
	return run, err
}
