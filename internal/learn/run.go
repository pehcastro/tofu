package learn

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"tofu/internal/memory"
	"tofu/internal/sys"
)

const (
	dirName       = "learn"
	runsDir       = "runs"
	upstreamDir   = "upstream"
	decisionsFile = "decisions.jsonl"
	keyFile       = "mark.key"
	runStamp      = "20060102-150405"
	markKeyBytes  = 32
)

type Run struct {
	ID       string    `json:"id"`
	At       time.Time `json:"at"`
	Scope    string    `json:"scope"`
	Mode     Mode      `json:"mode"`
	Model    string    `json:"grouped_by,omitempty"`
	Read     Read      `json:"read"`
	Summary  Summary   `json:"summary"`
	Findings []Finding `json:"findings"`
	Held     []Finding `json:"held,omitempty"`
	Watching []Finding `json:"watching,omitempty"`
	Fixed    []Finding `json:"fixed,omitempty"`
	Project  []Finding `json:"project,omitempty"`
	Decided  []Finding `json:"decided,omitempty"`
	Sent     Sent      `json:"sent"`
	Labels   []Label   `json:"labels,omitempty"`
	Said     []Said    `json:"said"`
}

type Known struct {
	Memory    []memory.Entry
	Decisions []Decision
	Releases  []Release
	Settings  map[string]string
}

func (r Run) All() []Finding {
	return slices.Concat(r.Findings, r.Held, r.Watching, r.Fixed, r.Project, r.Decided)
}

func (r Run) Find(id int) (Finding, bool) {
	for _, f := range slices.Concat(r.Findings, r.Held) {
		if f.ID == id {
			return f, true
		}
	}
	return Finding{}, false
}

func Scan(sources []Source, known Known, at time.Time) (Run, error) {
	said, read, err := Distil(sources)
	if err != nil {
		return Run{}, err
	}
	run := Run{ID: at.Format(runStamp), At: at, Read: read, Said: said, Mode: ModeLocal}
	run.settle(localFindings(said, requests{}), known)
	return run, nil
}

type Action string

const (
	ActionApply  Action = "apply"
	ActionReject Action = "reject"
	ActionDraft  Action = "upstream"
)

type Decision struct {
	At     time.Time `json:"at"`
	Run    string    `json:"run"`
	ID     int       `json:"id"`
	Key    string    `json:"key"`
	Action Action    `json:"action"`
	Places int       `json:"places"`
	Reason string    `json:"reason,omitempty"`
}

func quiet(f Finding, decisions []Decision) bool {
	return slices.ContainsFunc(decisions, func(d Decision) bool { return d.Key == f.Key && f.Sessions <= d.Places })
}

type Home struct{ Dir string }

func OpenHome() (Home, error) {
	home, err := sys.HomeConfigDir()
	return Home{Dir: filepath.Join(home, dirName)}, err
}

func (h Home) UpstreamDir() string { return filepath.Join(h.Dir, upstreamDir) }

func (h Home) Save(run Run) (string, error) {
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", err
	}
	file := filepath.Join(h.Dir, runsDir, run.ID+".json")
	return file, sys.WriteFile(file, data, 0o600)
}

func (h Home) Last() (Run, error) {
	names, err := filepath.Glob(filepath.Join(h.Dir, runsDir, "*.json"))
	if err != nil || len(names) == 0 {
		return Run{}, errors.Join(err, errors.New("no learn run yet: tofu learn scan --local makes one"))
	}
	slices.Sort(names)
	data, err := os.ReadFile(names[len(names)-1])
	if err != nil {
		return Run{}, err
	}
	var run Run
	return run, json.Unmarshal(data, &run)
}

func (h Home) Decisions() ([]Decision, error) {
	file, err := os.Open(filepath.Join(h.Dir, decisionsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var decisions []Decision
	for lines := bufio.NewScanner(file); lines.Scan(); {
		var d Decision
		if err := json.Unmarshal(lines.Bytes(), &d); err != nil {
			return nil, err
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

func (h Home) Decide(d Decision) error {
	line, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(h.Dir, decisionsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return errors.Join(err, file.Close())
}
