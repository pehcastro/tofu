package report

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofu/internal/judge/method"
)

const AnswersPath = "bench/report/answers.json"

type Better string

const (
	BetterHigher Better = "higher"
	BetterLower  Better = "lower"
	aTie                = "a tie"
)

type Axis struct {
	Name       string `json:"name"`
	Unit       string `json:"unit"`
	BetterWhen Better `json:"better_when"`
}

func (a Axis) Sentence() string {
	return "measured as " + a.Name + ", " + a.Unit + ", " + string(a.BetterWhen) + " is better"
}

type Method struct {
	Name     string  `json:"name"`
	Does     string  `json:"does,omitempty"`
	DoesFrom string  `json:"does_from,omitempty"`
	Percent  float64 `json:"percent"`
	Hits     float64 `json:"hits,omitempty"`
	OutOf    float64 `json:"out_of,omitempty"`
	Evidence string  `json:"evidence"`
}

type Placement struct {
	Point           string        `json:"point"`
	Question        string        `json:"question"`
	Axis            Axis          `json:"axis"`
	Free            Method        `json:"free_arm"`
	Judged          Method        `json:"judged_arm"`
	SampleSize      int           `json:"sample_size"`
	SampleOf        string        `json:"sample_of"`
	Decides         string        `json:"decides"`
	InUse           method.Method `json:"wiring"`
	Source          string        `json:"source"`
	Missing         string        `json:"unanswerable_because,omitempty"`
	MissingEvidence string        `json:"unanswerable_evidence,omitempty"`
}

type Claim struct {
	Text     string `json:"text"`
	Evidence string `json:"evidence,omitempty"`
}

type Stat struct {
	Label    string `json:"label"`
	Figure   Figure `json:"figure"`
	Note     string `json:"note"`
	Source   string `json:"source,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

type Refused struct {
	Question        string  `json:"question"`
	Tab             string  `json:"tab"`
	Because         string  `json:"because"`
	WhatExists      []Claim `json:"what_exists"`
	WhatItWouldTake []Claim `json:"what_it_would_take"`
	Source          string  `json:"source"`
}

type Answers struct {
	JevCosts   []Stat      `json:"jev_costs"`
	Arms       Refused     `json:"arms"`
	Versions   Refused     `json:"versions"`
	Placements []Placement `json:"judgments"`
}

type Reading struct {
	Name    string  `json:"name"`
	Does    string  `json:"does"`
	Reading Figure  `json:"reading"`
	Percent float64 `json:"percent"`
}

type JudgmentRow struct {
	Point       string  `json:"point"`
	Question    string  `json:"question"`
	Axis        string  `json:"axis"`
	Without     Reading `json:"free_arm"`
	With        Reading `json:"judged_arm"`
	Winner      string  `json:"winner"`
	Gap         Figure  `json:"gap"`
	Multiple    Figure  `json:"multiple"`
	Sample      Figure  `json:"sample"`
	SwitchedOn  string  `json:"switched_on"`
	Wired       bool    `json:"wired"`
	Source      string  `json:"source"`
	NotCompared string  `json:"unanswerable_because,omitempty"`
}

func (r JudgmentRow) Compared() bool { return r.NotCompared == "" }

func gapOf(one Placement) float64 {
	if one.Missing != "" {
		return math.Inf(-1)
	}
	if one.Axis.BetterWhen == BetterLower {
		return one.Free.Percent - one.Judged.Percent
	}
	return one.Judged.Percent - one.Free.Percent
}

func winnerOf(one Placement) string {
	switch gain := gapOf(one); {
	case gain > 0:
		return one.Judged.Name
	case gain < 0:
		return one.Free.Name
	default:
		return aTie
	}
}

func readingOf(method Method) Reading {
	return Reading{Name: method.Name, Does: method.Does, Reading: percentFigure(method.Percent), Percent: method.Percent}
}

func rowsOf(answers Answers, wiring map[string]Wiring) []JudgmentRow {
	ordered := append([]Placement{}, answers.Placements...)
	sort.SliceStable(ordered, func(i, j int) bool { return gapOf(ordered[i]) > gapOf(ordered[j]) })
	rows := make([]JudgmentRow, 0, len(ordered))
	for _, one := range ordered {
		decided := wiring[one.Decides]
		row := JudgmentRow{
			Point:       one.Point,
			Question:    one.Question,
			Axis:        one.Axis.Sentence(),
			Sample:      countFigure(one.SampleSize, one.SampleOf),
			SwitchedOn:  decided.SwitchedOn,
			Wired:       decided.On,
			Source:      one.Source,
			NotCompared: one.Missing,
		}
		if one.Missing == "" {
			row.Without = readingOf(one.Free)
			row.With = readingOf(one.Judged)
			row.Winner = winnerOf(one)
			row.Gap = pointsFigure(math.Abs(one.Judged.Percent - one.Free.Percent))
			row.Multiple = multipleFigure(one.Judged.Percent, one.Free.Percent)
		}
		rows = append(rows, row)
	}
	return rows
}

func checkAnswers(answers Answers, table method.Table, tree string, bodies map[string]string) error {
	if len(answers.JevCosts) == 0 {
		return fmt.Errorf("%s: a reader comparing Jev with a regular expression is told what a Jev call costs", AnswersPath)
	}
	for _, stat := range answers.JevCosts {
		if err := checkStat(stat, bodies); err != nil {
			return err
		}
	}
	for _, section := range []Refused{answers.Arms, answers.Versions} {
		if err := checkRefused(section, bodies); err != nil {
			return err
		}
	}
	if len(answers.Placements) == 0 {
		return fmt.Errorf("%s names no Jev decision, and that is the one table this project can fill", AnswersPath)
	}
	for _, one := range answers.Placements {
		if err := checkPlacement(one, table, tree, bodies); err != nil {
			return err
		}
	}
	return nil
}

func checkStat(stat Stat, bodies map[string]string) error {
	if stat.Label == "" || stat.Figure == "" || stat.Note == "" {
		return fmt.Errorf("%s: a card needs a name, a figure and what the figure rests on, and one of the three is empty", AnswersPath)
	}
	if stat.Figure.IsBareCount() {
		return fmt.Errorf("%s: the card %q reads %q, which is a bare count", AnswersPath, stat.Label, stat.Figure)
	}
	return checkEvidence(stat.Evidence, stat.Source, bodies)
}

func checkRefused(section Refused, bodies map[string]string) error {
	if section.Question == "" || section.Tab == "" || section.Because == "" {
		return fmt.Errorf("%s: a table that cannot be filled says which question it would answer, what to call it, and why it cannot", AnswersPath)
	}
	if !strings.ContainsAny(section.Because, "0123456789") {
		return fmt.Errorf("%s: %q carries no number, and a line on this page without one is prose", AnswersPath, section.Because)
	}
	for _, group := range [][]Claim{section.WhatExists, section.WhatItWouldTake} {
		for _, claim := range group {
			if err := checkClaim(claim, section.Source, bodies); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkClaim(claim Claim, source string, bodies map[string]string) error {
	if claim.Text == "" {
		return fmt.Errorf("%s: an empty line is listed under %s", AnswersPath, source)
	}
	if !strings.ContainsAny(claim.Text, "0123456789") {
		return nil
	}
	if claim.Evidence == "" {
		return fmt.Errorf("%s: %q carries a number and cites nothing in %s", AnswersPath, claim.Text, source)
	}
	return checkEvidence(claim.Evidence, source, bodies)
}

func checkEvidence(evidence, source string, bodies map[string]string) error {
	body, known := bodies[source]
	if !known {
		return fmt.Errorf("%s cites %s, which is not a dated report under bench/", AnswersPath, source)
	}
	if !strings.Contains(body, evidence) {
		return fmt.Errorf("%s: %q is not in %s, so the number it carries was not read from the report", AnswersPath, evidence, source)
	}
	return nil
}

func checkPlacement(one Placement, table method.Table, tree string, bodies map[string]string) error {
	if one.Point == "" || one.Question == "" {
		return fmt.Errorf("%s: a decision is listed with no name, or with no plain question a person would ask", AnswersPath)
	}
	if one.SampleSize <= 0 || one.SampleOf == "" {
		return fmt.Errorf("%s: %s states a percentage with no sample, which is the same noise in a different shape", AnswersPath, one.Point)
	}
	if err := checkAgainstTheTable(one, table); err != nil {
		return err
	}
	switch one.Axis.BetterWhen {
	case BetterHigher, BetterLower:
	default:
		return fmt.Errorf("%s: %s is measured on an axis that says neither higher nor lower is better", AnswersPath, one.Point)
	}
	if one.Axis.Name == "" || one.Axis.Unit == "" {
		return fmt.Errorf("%s: %s declares no axis, and the viewer fixes none of its own", AnswersPath, one.Point)
	}
	if one.Missing != "" {
		if one.Free.Name != "" || one.Judged.Name != "" {
			return fmt.Errorf("%s: %s says it cannot be compared and still names a method", AnswersPath, one.Point)
		}
		return checkEvidence(one.MissingEvidence, one.Source, bodies)
	}
	for _, method := range []Method{one.Free, one.Judged} {
		if err := checkMethod(method, one, tree, bodies); err != nil {
			return err
		}
	}
	return nil
}

func checkAgainstTheTable(one Placement, table method.Table) error {
	chosen, err := table.Of(one.Decides)
	if err != nil {
		return fmt.Errorf("%s: %s says it decides %q, and %s names no such point", AnswersPath, one.Point, one.Decides, table.File)
	}
	if one.InUse != chosen.Method {
		return fmt.Errorf("%s: %s says %q decides it and %s:%d says %q", AnswersPath, one.Point, string(one.InUse), table.File, chosen.Line, string(chosen.Method))
	}
	return nil
}

func checkMethod(method Method, one Placement, tree string, bodies map[string]string) error {
	if method.Name == "" {
		return fmt.Errorf("%s: %s leaves a method unnamed, so it cannot say what it was compared against", AnswersPath, one.Point)
	}
	if method.Does == "" {
		return fmt.Errorf("%s: %s names %q and never says what it does, which is a name a reader cannot act on", AnswersPath, one.Point, method.Name)
	}
	if _, err := os.Stat(filepath.Join(tree, filepath.FromSlash(method.DoesFrom))); err != nil {
		return fmt.Errorf("%s: %s says %q does %q and points at %s, which is not in the tree", AnswersPath, one.Point, method.Name, method.Does, method.DoesFrom)
	}
	if err := checkEvidence(method.Evidence, one.Source, bodies); err != nil {
		return err
	}
	if method.OutOf > 0 {
		if want := math.Round(method.Hits/method.OutOf*1000) / 10; want != method.Percent {
			return fmt.Errorf("%s: %s says %s scored %.1f%% and %.0f of %.0f is %.1f%%", AnswersPath, one.Point, method.Name, method.Percent, method.Hits, method.OutOf, want)
		}
		return nil
	}
	if !strings.Contains(method.Evidence, string(percentFigure(method.Percent))) {
		return fmt.Errorf("%s: %s says %s scored %.1f%% and cites %q, which does not print it", AnswersPath, one.Point, method.Name, method.Percent, method.Evidence)
	}
	return nil
}
