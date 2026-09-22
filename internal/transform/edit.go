package transform

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type Kind string

const (
	Create  Kind = "create"
	Replace Kind = "replace_range"
	Prepend Kind = "prepend"
)

const (
	anchorField = "anchor"
	untilField  = "until"
)

type Edit struct {
	Kind             Kind   `json:"kind"`
	Anchor           string `json:"anchor,omitempty"`
	AnchorOccurrence int    `json:"anchor_occurrence,omitempty"`
	Until            string `json:"until,omitempty"`
	UntilOccurrence  int    `json:"until_occurrence,omitempty"`
	Text             string `json:"text"`
}

type AnchorNotFound struct {
	Field  string
	Anchor string
}

func (e AnchorNotFound) Error() string {
	return fmt.Sprintf("%s %q matches no line in the file", e.Field, e.Anchor)
}

type AmbiguousAnchor struct {
	Field   string
	Anchor  string
	Matches []int
}

func (e AmbiguousAnchor) Error() string {
	named := make([]string, 0, len(e.Matches))
	for _, line := range e.Matches {
		named = append(named, strconv.Itoa(line))
	}
	return fmt.Sprintf("%s %q matches %d lines (%s) and an edit must name exactly one",
		e.Field, e.Anchor, len(e.Matches), strings.Join(named, ", "))
}

type OccurrenceOutOfRange struct {
	Field  string
	Anchor string
	Wanted int
	Found  int
}

func (e OccurrenceOutOfRange) Error() string {
	return fmt.Sprintf("%s %q was asked for occurrence %d and the file holds %d",
		e.Field, e.Anchor, e.Wanted, e.Found)
}

func trimLine(line string) string { return strings.TrimRight(line, "\r\n") }

func matchLines(lines []string, anchor string, from int) []int {
	var found []int
	for i := from; i < len(lines); i++ {
		if trimLine(lines[i]) == anchor {
			found = append(found, i+1)
		}
	}
	return found
}

func pick(lines []string, field, anchor string, occurrence, from int) (int, error) {
	matches := matchLines(lines, anchor, from)
	switch {
	case len(matches) == 0:
		return 0, AnchorNotFound{Field: field, Anchor: anchor}
	case occurrence > len(matches):
		return 0, OccurrenceOutOfRange{Field: field, Anchor: anchor, Wanted: occurrence, Found: len(matches)}
	case occurrence > 0:
		return matches[occurrence-1] - 1, nil
	case len(matches) > 1:
		return 0, AmbiguousAnchor{Field: field, Anchor: anchor, Matches: matches}
	}
	return matches[0] - 1, nil
}

func (e Edit) On(before string) (string, error) {
	switch e.Kind {
	case Create:
		return e.Text, nil
	case Prepend:
		return e.Text + before, nil
	case Replace:
		lines := splitLines(before)
		from, err := pick(lines, anchorField, e.Anchor, e.AnchorOccurrence, 0)
		if err != nil {
			return "", err
		}
		to, err := pick(lines, untilField, e.Until, e.UntilOccurrence, from)
		if err != nil {
			return "", err
		}
		text := e.Text
		if text != "" && !strings.HasSuffix(text, "\n") && strings.HasSuffix(lines[to], "\n") {
			text += "\n"
		}
		return strings.Join(lines[:from], "") + text + strings.Join(lines[to+1:], ""), nil
	default:
		return "", fmt.Errorf("edit kind %q is not one this transform holds", e.Kind)
	}
}

func Derive(before, after string) ([]Edit, error) {
	if before == "" {
		return []Edit{{Kind: Create, Text: after}}, nil
	}
	old := splitLines(before)
	var edits []Edit
	for _, hunk := range slices.Backward(Hunks(before, after)) {
		added := strings.Join(hunk.Added, "")
		if len(hunk.Removed) == 0 && hunk.BeforeStart == 0 {
			edits = append(edits, Edit{Kind: Prepend, Text: added})
			continue
		}
		from, to := hunk.BeforeStart, hunk.BeforeStart+len(hunk.Removed)-1
		edit := Edit{Kind: Replace, Text: added}
		if len(hunk.Removed) == 0 {
			from, to = hunk.BeforeStart-1, hunk.BeforeStart-1
			edit.Text = old[from] + added
		}
		edit.Anchor, edit.Until = trimLine(old[from]), trimLine(old[to])
		if matches := matchLines(old, edit.Anchor, 0); len(matches) > 1 {
			edit.AnchorOccurrence = slices.Index(matches, from+1) + 1
		}
		if matches := matchLines(old, edit.Until, from); len(matches) > 1 {
			edit.UntilOccurrence = slices.Index(matches, to+1) + 1
		}
		edits = append(edits, edit)
	}
	rebuilt := before
	for _, edit := range edits {
		next, err := edit.On(rebuilt)
		if err != nil {
			return nil, err
		}
		rebuilt = next
	}
	if rebuilt != after {
		return nil, errors.New("the derived edits do not rebuild the file they were derived from")
	}
	return edits, nil
}
