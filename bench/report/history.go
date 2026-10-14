package report

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const HistoryPath = "bench/report/history.json"

type Verdict string

const (
	Ahead     Verdict = "ahead"
	Level     Verdict = "level"
	Behind    Verdict = "behind"
	Described Verdict = "described, not ranked"
)

var ranked = []Verdict{Ahead, Level, Behind}

type Run struct {
	Label    string  `json:"label"`
	Date     string  `json:"date"`
	Commit   string  `json:"commit"`
	Version  string  `json:"version"`
	Build    string  `json:"build"`
	Figure   string  `json:"figure"`
	Value    float64 `json:"value"`
	Low      float64 `json:"low,omitempty"`
	High     float64 `json:"high,omitempty"`
	Source   string  `json:"source"`
	Evidence string  `json:"evidence"`
}

func (r Run) span() (float64, float64) {
	if r.Low == 0 && r.High == 0 {
		return r.Value, r.Value
	}
	return r.Low, r.High
}

func (r Run) Conditions() string {
	return fmt.Sprintf("%s, %s, %s, tofu %s, %s", r.Source, r.Date, r.Commit, r.Version, r.Build)
}

type Measure struct {
	Bench  string `json:"bench"`
	Task   string `json:"task"`
	Name   string `json:"measure"`
	Unit   string `json:"unit"`
	Better Better `json:"better"`
	Tofu   []Run  `json:"tofu"`
	Rivals []Run  `json:"rivals,omitempty"`
}

func (m Measure) Latest() Run { return m.Tofu[len(m.Tofu)-1] }

func (m Measure) Shown(run Run) Figure { return Figure(run.Figure + " " + m.Unit) }

func (m Measure) Against(rival Run) Verdict {
	if m.Better == BetterNeither {
		return Described
	}
	ourLow, ourHigh := m.Latest().span()
	theirLow, theirHigh := rival.span()
	switch {
	case ourHigh >= theirLow && theirHigh >= ourLow:
		return Level
	case (m.Better == BetterLower) == (ourHigh < theirLow):
		return Ahead
	default:
		return Behind
	}
}

func (m Measure) Change(at int) string {
	if at == 0 {
		return "first run"
	}
	before, after := m.Tofu[at-1], m.Tofu[at]
	moved := "no change"
	switch {
	case after.Value < before.Value:
		moved = "lower"
	case after.Value > before.Value:
		moved = "higher"
	}
	if moved == "no change" || m.Better == BetterNeither {
		return fmt.Sprintf("%s to %s, %s", m.Shown(before), m.Shown(after), moved)
	}
	judged := "worse"
	if (moved == "lower") == (m.Better == BetterLower) {
		judged = "better"
	}
	if before.Value != 0 && after.Value != 0 {
		moved = string(multipleFigure(before.Value, after.Value)) + " " + moved
	}
	return fmt.Sprintf("%s to %s, %s, %s", m.Shown(before), m.Shown(after), moved, judged)
}

func (m Measure) Runs() []Run { return append(append([]Run{}, m.Tofu...), m.Rivals...) }

type History struct {
	ReadOn   string    `json:"read_on"`
	Tree     string    `json:"tree"`
	Version  string    `json:"version"`
	Rule     string    `json:"level_rule"`
	Measures []Measure `json:"measures"`
}

type Standing struct {
	Measure Measure
	Verdict Verdict
	Rivals  []Run
}

func (h History) Standings() []Standing {
	var all []Standing
	for _, verdict := range ranked {
		for _, m := range h.Measures {
			one := Standing{Measure: m, Verdict: verdict}
			for _, rival := range m.Rivals {
				if m.Against(rival) == verdict {
					one.Rivals = append(one.Rivals, rival)
				}
			}
			if len(one.Rivals) > 0 {
				all = append(all, one)
			}
		}
	}
	return all
}

func (s Standing) Sentence() string {
	theirs := make([]string, 0, len(s.Rivals))
	for _, rival := range s.Rivals {
		theirs = append(theirs, fmt.Sprintf("%s %s", rival.Label, s.Measure.Shown(rival)))
	}
	latest := s.Measure.Latest()
	return fmt.Sprintf("%s, %s: tofu %s against %s (bench/%s, %s, %s, %s, tofu %s)",
		s.Measure.Name, s.Measure.Task, s.Measure.Shown(latest), strings.Join(theirs, ", "), s.Measure.Bench, latest.Label, latest.Date, latest.Commit, latest.Version)
}

func (h History) DateLine() string {
	newest := ""
	for _, m := range h.Measures {
		for _, run := range m.Runs() {
			newest = max(newest, run.Date)
		}
	}
	return fmt.Sprintf("Read on %s from the tree at %s, tofu %s. The newest measurement ran on %s. Every number names its bench, the day it ran, the commit and the tofu version it measured.", h.ReadOn, h.Tree, h.Version, newest)
}

var numberIn = regexp.MustCompile(`[0-9][0-9,.]*[0-9]|[0-9]`)

func readHistory(path, tree string) (History, error) {
	var history History
	if err := readJSON(path, &history); err != nil {
		return History{}, err
	}
	if history.ReadOn == "" || history.Tree == "" || history.Version == "" || history.Rule == "" {
		return History{}, fmt.Errorf("%s: says no day it was read on, no tree, no version or no rule for level", HistoryPath)
	}
	texts := map[string]string{}
	for _, m := range history.Measures {
		switch m.Better {
		case BetterHigher, BetterLower, BetterNeither:
		default:
			return History{}, fmt.Errorf("%s: %s carries the unknown direction %q", HistoryPath, m.Name, m.Better)
		}
		if m.Bench == "" || m.Task == "" || m.Name == "" || m.Unit == "" || len(m.Tofu) == 0 {
			return History{}, fmt.Errorf("%s: a measure needs a bench, a task, a name, a unit and one tofu run: %+v", HistoryPath, m)
		}
		for _, run := range m.Runs() {
			if err := checkRun(m, run, tree, texts); err != nil {
				return History{}, err
			}
		}
	}
	return history, nil
}

func checkRun(m Measure, run Run, tree string, texts map[string]string) error {
	where := fmt.Sprintf("%s: %s, %s, %s", HistoryPath, m.Name, m.Task, run.Label)
	if run.Label == "" || run.Commit == "" || run.Version == "" || run.Build == "" || run.Figure == "" {
		return fmt.Errorf("%s: a run names who ran, the commit, the version, the build and the figure", where)
	}
	if _, err := time.Parse("2006-01-02", run.Date); err != nil {
		return fmt.Errorf("%s: the date %q is not a day", where, run.Date)
	}
	if low, high := run.span(); low > run.Value || run.Value > high {
		return fmt.Errorf("%s: %v lies outside its own spread %v to %v", where, run.Value, low, high)
	}
	text, read := texts[run.Source]
	if !read {
		body, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(run.Source)))
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		text = flatten(string(body))
		texts[run.Source] = text
	}
	evidence := flatten(run.Evidence)
	if evidence == "" || !strings.Contains(text, evidence) {
		return fmt.Errorf("%s: %q is not in %s", where, run.Evidence, run.Source)
	}
	for _, number := range numberIn.FindAllString(run.Figure, -1) {
		if !strings.Contains(evidence, number) {
			return fmt.Errorf("%s: the figure %q carries %s, which the line it cites does not print", where, run.Figure, number)
		}
	}
	return nil
}
