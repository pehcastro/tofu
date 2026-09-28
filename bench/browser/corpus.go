package browserbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"tofu/internal/browser"
)

type Case struct {
	ID     string
	Source string
	Goal   string
	Page   browser.Page
	Want   browser.Action
}

func Load(dir string) ([]Case, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "corpus.json"))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID     string `json:"id"`
		Source string `json:"source"`
		Goal   string `json:"goal"`
		Want   struct {
			Op      string `json:"op"`
			Element int    `json:"element"`
			Value   string `json:"value"`
		} `json:"want"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("corpus.json: %w", err)
	}
	cases := make([]Case, 0, len(rows))
	for _, row := range rows {
		op, err := browser.ParseOp(row.Want.Op)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", row.ID, err)
		}
		snapshot, err := os.ReadFile(filepath.Join(dir, row.ID+".json"))
		if err != nil {
			return nil, err
		}
		page, err := browser.ParsePage(snapshot)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", row.ID, err)
		}
		want := browser.Action{Op: op, Element: row.Want.Element, Value: row.Want.Value}
		if target, found := page.Element(want.Element); want.Element != 0 && (!found || !op.Accepts(target.Role)) {
			return nil, fmt.Errorf("%s wants %s on element %d, which the page does not offer for it", row.ID, op, want.Element)
		}
		cases = append(cases, Case{ID: row.ID, Source: row.Source, Goal: row.Goal, Page: page, Want: want})
	}
	return cases, nil
}

var namedIndex = regexp.MustCompile(`(?i)\b(element|index|item)\s*#?\d+|\[\d+\]|#\d+`)

var number = regexp.MustCompile(`\b\d+\b`)

func Leaks(c Case) []string {
	var leaks []string
	if named := namedIndex.FindString(c.Goal); named != "" {
		leaks = append(leaks, fmt.Sprintf("%s: the goal names an element index, %q", c.ID, named))
	}
	for _, digits := range number.FindAllString(c.Goal, -1) {
		if at, _ := strconv.Atoi(digits); c.Want.Element != 0 && at == c.Want.Element {
			leaks = append(leaks, fmt.Sprintf("%s: the goal carries its target's index %d", c.ID, at))
		}
	}
	target, found := c.Page.Element(c.Want.Element)
	if !found {
		return leaks
	}
	labels := []string{target.Label}
	for _, option := range target.Options {
		if option.Value == c.Want.Value {
			labels = append(labels, option.Label)
		}
	}
	for _, label := range labels {
		if strings.Contains(strings.ToLower(c.Goal), strings.ToLower(label)) {
			leaks = append(leaks, fmt.Sprintf("%s: the goal quotes its target's label %q", c.ID, label))
		}
	}
	return leaks
}
