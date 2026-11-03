package airbnb

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Arm string

const (
	ArmA  Arm = "A"
	ArmB1 Arm = "B1"
	ArmB2 Arm = "B2"
	ArmB3 Arm = "B3"
	ArmB4 Arm = "B4"
	ArmC  Arm = "C"
)

const browserArmPrefix = "B:"

func BrowserArm(slug string) Arm { return Arm(browserArmPrefix + slug) }

func (a Arm) Folder() string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '.' {
			return r
		}
		return '-'
	}, string(a))
}

const MainModel = "claude-sub/claude-opus-5"

type ArmSettings struct {
	Driver       string
	BrowserModel string
}

func (a Arm) Settings() (ArmSettings, error) {
	switch a {
	case ArmA:
		return ArmSettings{Driver: "steps"}, nil
	case ArmB1:
		return ArmSettings{Driver: "subagent", BrowserModel: "meta/muse-spark-1.3-contributor"}, nil
	case ArmB2:
		return ArmSettings{Driver: "subagent", BrowserModel: "claude-sub/claude-sonnet-5-5"}, nil
	case ArmB3:
		return ArmSettings{Driver: "subagent", BrowserModel: "codex-sub/gpt-5.6-sol"}, nil
	case ArmB4:
		return ArmSettings{Driver: "subagent", BrowserModel: "codex-sub/gpt-5.6-luna"}, nil
	case ArmC:
		return ArmSettings{Driver: "goal"}, nil
	}
	if slug, named := strings.CutPrefix(string(a), browserArmPrefix); named && strings.Contains(slug, "/") {
		return ArmSettings{Driver: "subagent", BrowserModel: slug}, nil
	}
	return ArmSettings{}, fmt.Errorf("unknown arm %q: A, B1, B2, B3, B4, C, or a browser model as -browser-model source/model", a)
}

type Conditions struct {
	Date              string `json:"date"`
	Machine           string `json:"machine"`
	Credential        string `json:"credential"`
	BrowserCredential string `json:"browser_credential"`
	BrowserModel      string `json:"browser_model,omitempty"`
	Wire              string `json:"wire"`
	MainBuild         string `json:"main_build"`
	BrowserBuild      string `json:"browser_build"`
}

func credentialOf(slug string) string {
	source, _, _ := strings.Cut(slug, "/")
	if strings.HasSuffix(source, "-sub") {
		return "subscription"
	}
	return "key"
}

func (a Arm) Stamp(run *Run, date, machine string) error {
	if _, err := a.Settings(); err != nil {
		return err
	}
	run.Conditions.Date, run.Conditions.Machine = date, machine
	run.Conditions.Credential, run.Conditions.BrowserCredential = credentialOf(MainModel), credentialOf(cmp.Or(run.Conditions.BrowserModel, MainModel))
	return nil
}

type Tab struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type Phases struct {
	WallMS          int64 `json:"wall_ms"`
	ModelMS         int64 `json:"model_ms"`
	BrowserMS       int64 `json:"browser_ms"`
	Steps           int   `json:"steps"`
	ModelMedianMS   int64 `json:"model_median_ms"`
	BrowserMedianMS int64 `json:"browser_median_ms"`
}

func (p Phases) OtherMS() int64 { return p.WallMS - p.ModelMS - p.BrowserMS }

type Page struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Text    string `json:"text,omitempty"`
	Reached string `json:"reached,omitempty"`
}

type Run struct {
	Arm           Arm        `json:"arm"`
	Seed          int64      `json:"seed"`
	Conditions    Conditions `json:"conditions"`
	WallMS        int64      `json:"wall_ms"`
	Forks         int        `json:"forks"`
	SubForks      int        `json:"sub_agent_forks"`
	MainTokens    int        `json:"main_tokens"`
	BrowserTokens *int       `json:"browser_tokens"`
	Repeated      int        `json:"repeated"`
	Refused       int        `json:"refused"`
	Mixed         bool       `json:"mixed"`
	Capped        bool       `json:"capped"`
	Phases        Phases     `json:"phases"`
	TabsClosed    int        `json:"tabs_closed_before"`
	Tabs          []Tab      `json:"tabs"`
	Visits        []string   `json:"visits"`
	Pages         []Page     `json:"pages,omitempty"`
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
	if _, err := run.Arm.Settings(); err != nil {
		return run, fmt.Errorf("%s: %w", dir, err)
	}
	snapshot, err := os.ReadFile(filepath.Join(dir, "snapshot.txt"))
	if err != nil {
		return run, err
	}
	report, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	run.Snapshot, run.Report = string(snapshot), string(report)
	return run, err
}

func SaveRun(dir string, run Run) error {
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	for name, body := range map[string]string{"run.json": string(raw) + "\n", "snapshot.txt": run.Snapshot, "report.txt": run.Report} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
