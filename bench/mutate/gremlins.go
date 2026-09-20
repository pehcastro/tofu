package mutate

import (
	"fmt"
	"strconv"
	"strings"
)

type Status string

const (
	Killed     Status = "KILLED"
	Lived      Status = "LIVED"
	NotCovered Status = "NOT COVERED"
	TimedOut   Status = "TIMED OUT"
	NotViable  Status = "NOT VIABLE"
	Skipped    Status = "SKIPPED"
)

type Mutant struct {
	Status  Status
	Mutator string
	File    string
	Line    int
	Column  int
}

type Score struct {
	Killed     int
	Lived      int
	NotCovered int
	TimedOut   int
	NotViable  int
	Skipped    int
}

func Parse(output string) ([]Mutant, error) {
	var mutants []Mutant
	for i, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		at := strings.LastIndex(line, " at ")
		if at < 0 {
			continue
		}
		status, mutator, found := cutLast(line[:at], " ")
		if !found || !known(Status(status)) {
			continue
		}
		m, err := position(line[at+len(" at "):])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		m.Status = Status(status)
		m.Mutator = mutator
		mutants = append(mutants, m)
	}
	return mutants, nil
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

func known(s Status) bool {
	switch s {
	case Killed, Lived, NotCovered, TimedOut, NotViable, Skipped:
		return true
	}
	return false
}

func position(text string) (Mutant, error) {
	file, place, found := cutLast(text, ":")
	if !found {
		return Mutant{}, fmt.Errorf("%q is not a file:line:column", text)
	}
	file, lineText, found := cutLast(file, ":")
	if !found {
		return Mutant{}, fmt.Errorf("%q is not a file:line:column", text)
	}
	line, err := strconv.Atoi(lineText)
	if err != nil {
		return Mutant{}, fmt.Errorf("%q has no line number: %w", text, err)
	}
	column, err := strconv.Atoi(place)
	if err != nil {
		return Mutant{}, fmt.Errorf("%q has no column: %w", text, err)
	}
	return Mutant{File: file, Line: line, Column: column}, nil
}

func Tally(mutants []Mutant) Score {
	var s Score
	for _, m := range mutants {
		switch m.Status {
		case Killed:
			s.Killed++
		case Lived:
			s.Lived++
		case NotCovered:
			s.NotCovered++
		case TimedOut:
			s.TimedOut++
		case NotViable:
			s.NotViable++
		case Skipped:
			s.Skipped++
		}
	}
	return s
}

func (s Score) Efficacy() float64 {
	tested := s.Killed + s.Lived
	if tested == 0 {
		return 0
	}
	return 100 * float64(s.Killed) / float64(tested)
}

func Survivors(mutants []Mutant) []Mutant {
	var lived []Mutant
	for _, m := range mutants {
		if m.Status == Lived {
			lived = append(lived, m)
		}
	}
	return lived
}
