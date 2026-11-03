package flake

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os/exec"
	"slices"
)

type Outcome string

const (
	Passed  Outcome = "pass"
	Failed  Outcome = "fail"
	Skipped Outcome = "skip"
	Absent  Outcome = "absent"
)

var ErrNoRuns = errors.New("a flakiness measurement needs at least two runs")

type TestOutcomes struct {
	Test     string
	Outcomes []Outcome
}

func (t TestOutcomes) Disagrees() bool {
	for _, outcome := range t.Outcomes {
		if outcome != t.Outcomes[0] {
			return true
		}
	}
	return false
}

func (t TestOutcomes) SkippedEveryRun() bool {
	for _, outcome := range t.Outcomes {
		if outcome != Skipped {
			return false
		}
	}
	return true
}

type Report struct {
	Package string
	Runs    int
	Tests   []TestOutcomes
}

func (r Report) Disagreeing() []TestOutcomes {
	return r.selectTests(TestOutcomes.Disagrees)
}

func (r Report) AlwaysSkipped() []TestOutcomes {
	return r.selectTests(TestOutcomes.SkippedEveryRun)
}

func (r Report) selectTests(keep func(TestOutcomes) bool) []TestOutcomes {
	var selected []TestOutcomes
	for _, test := range r.Tests {
		if keep(test) {
			selected = append(selected, test)
		}
	}
	return selected
}

func parseRun(reader io.Reader) (map[string]Outcome, error) {
	decoder := json.NewDecoder(reader)
	outcomes := map[string]Outcome{}
	for {
		var event struct {
			Action string
			Test   string
		}
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			return outcomes, nil
		}
		if err != nil {
			return nil, err
		}
		if event.Test == "" {
			continue
		}
		switch Outcome(event.Action) {
		case Passed, Failed, Skipped:
			outcomes[event.Test] = Outcome(event.Action)
		}
	}
}

func Measure(pkg string, runs int) (Report, error) {
	if runs < 2 {
		return Report{}, ErrNoRuns
	}
	perRun := make([]map[string]Outcome, 0, runs)
	for range runs {
		command := exec.Command("go", "test", "-count=1", "-json", pkg)
		raw, err := command.Output()
		if len(raw) == 0 && err != nil {
			return Report{}, err
		}
		outcomes, err := parseRun(bytes.NewReader(raw))
		if err != nil {
			return Report{}, err
		}
		perRun = append(perRun, outcomes)
	}
	return Collate(pkg, perRun), nil
}

func Collate(pkg string, perRun []map[string]Outcome) Report {
	names := map[string]bool{}
	for _, run := range perRun {
		for name := range run {
			names[name] = true
		}
	}
	report := Report{Package: pkg, Runs: len(perRun)}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		test := TestOutcomes{Test: name}
		for _, run := range perRun {
			outcome, seen := run[name]
			if !seen {
				outcome = Absent
			}
			test.Outcomes = append(test.Outcomes, outcome)
		}
		report.Tests = append(report.Tests, test)
	}
	return report
}
