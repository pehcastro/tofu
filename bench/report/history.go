package report

import (
	"fmt"
	"math"
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
	Bench   string `json:"bench"`
	Task    string `json:"task"`
	Name    string `json:"measure"`
	Short   string `json:"short"`
	Pairing string `json:"pairing,omitempty"`
	Unit    string `json:"unit"`
	Gain    string `json:"better_word"`
	Loss    string `json:"worse_word"`
	Moved   bool   `json:"corpus_moved,omitempty"`
	Rough   bool   `json:"magnitude_only,omitempty"`
	Better  Better `json:"better"`
	Tofu    []Run  `json:"tofu"`
	Rivals  []Run  `json:"rivals,omitempty"`
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
	return fmt.Sprintf("%s to %s, %s", m.Shown(before), m.Shown(after), m.Ratio(before, after))
}

func (m Measure) Ratio(before, after Run) string {
	moved := "no change"
	switch {
	case after.Value < before.Value:
		moved = "lower"
	case after.Value > before.Value:
		moved = "higher"
	}
	if moved == "no change" || m.Better == BetterNeither {
		return moved
	}
	judged := "worse"
	if (moved == "lower") == (m.Better == BetterLower) {
		judged = "better"
	}
	if before.Value != 0 && after.Value != 0 {
		moved = string(multipleFigure(before.Value, after.Value)) + " " + moved
	}
	return moved + ", " + judged
}

func (m Measure) Runs() []Run { return append(append([]Run{}, m.Tofu...), m.Rivals...) }

func (m Measure) Words(ours, theirs Run) string {
	switch {
	case ours.Value == theirs.Value:
		return "same"
	case ours.Value == 0 || theirs.Value == 0:
		return cellNumber(ours) + " against " + cellNumber(theirs)
	}
	word, ratio := m.Loss, math.Max(ours.Value, theirs.Value)/math.Min(ours.Value, theirs.Value)
	gained := (m.Better == BetterLower) == (ours.Value < theirs.Value)
	if gained {
		word = m.Gain
	}
	switch {
	case m.Rough && ratio >= 10:
		return fmt.Sprintf("over %.0fx %s", math.Pow(10, math.Floor(math.Log10(ratio))), word)
	case ratio >= 1.5:
		return fmt.Sprintf("%.1fx %s", ratio, word)
	case gained:
		return fmt.Sprintf("%.0f%% %s", (1-1/ratio)*100, word)
	default:
		return fmt.Sprintf("%.0f%% %s", (ratio-1)*100, word)
	}
}

type Headline struct {
	Pairing string `json:"pairing"`
	Short   string `json:"short"`
	Rival   string `json:"rival"`
	As      string `json:"as"`
	Says    string `json:"says"`
}

type Stated struct {
	Value string
	Says  string
}

func (h History) state(one Headline) (Stated, error) {
	for _, m := range h.Measures {
		if m.Pairing != one.Pairing || m.Short != one.Short {
			continue
		}
		for _, rival := range m.Rivals {
			if rival.Label != one.Rival {
				continue
			}
			if verdict := m.Against(rival); verdict != Ahead {
				return Stated{}, fmt.Errorf("%s: the headline %q is tofu %s against %s, and only a win is a headline", HistoryPath, one.Says, verdict, one.Rival)
			}
			ratio := math.Max(m.Latest().Value, rival.Value) / math.Min(m.Latest().Value, rival.Value)
			switch one.As {
			case "ratio":
				return Stated{Value: fmt.Sprintf("%.1fx", ratio), Says: one.Says}, nil
			case "percent":
				return Stated{Value: fmt.Sprintf("%.0f%%", (1-1/ratio)*100), Says: one.Says}, nil
			default:
				return Stated{}, fmt.Errorf("%s: the headline %q is stated as %q, which is neither ratio nor percent", HistoryPath, one.Says, one.As)
			}
		}
	}
	return Stated{}, fmt.Errorf("%s: the headline %q names %s, %s against %s, which no measure carries", HistoryPath, one.Says, one.Pairing, one.Short, one.Rival)
}

type History struct {
	ReadOn    string     `json:"read_on"`
	Tree      string     `json:"tree"`
	Version   string     `json:"version"`
	Rule      string     `json:"level_rule"`
	Headlines []Headline `json:"headlines"`
	Measures  []Measure  `json:"measures"`
	Stated    []Stated   `json:"-"`
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
		if m.Bench == "" || m.Task == "" || m.Name == "" || m.Short == "" || m.Unit == "" || m.Gain == "" || m.Loss == "" || len(m.Tofu) == 0 {
			return History{}, fmt.Errorf("%s: a measure needs a bench, a task, a name, a short name, a unit, the words for better and worse, and one tofu run: %+v", HistoryPath, m)
		}
		for _, run := range m.Runs() {
			if err := checkRun(m, run, tree, texts); err != nil {
				return History{}, err
			}
		}
	}
	if len(history.Headlines) < 3 || len(history.Headlines) > 6 {
		return History{}, fmt.Errorf("%s: the page opens on 3 to 6 headlines and this names %d", HistoryPath, len(history.Headlines))
	}
	for _, one := range history.Headlines {
		stated, err := history.state(one)
		if err != nil {
			return History{}, err
		}
		history.Stated = append(history.Stated, stated)
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
