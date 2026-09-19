package transform

import (
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
	Kind            Kind   `json:"kind"`
	Anchor          string `json:"anchor,omitempty"`
	Until           string `json:"until,omitempty"`
	UntilOccurrence int    `json:"until_occurrence,omitempty"`
	Text            string `json:"text"`
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
	Anchor string
	Wanted int
	Found  int
}

func (e OccurrenceOutOfRange) Error() string {
	return fmt.Sprintf("until %q was asked for occurrence %d and only %d follow the anchor",
		e.Anchor, e.Wanted, e.Found)
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

func (e Edit) locate(lines []string) (int, int, error) {
	start := matchLines(lines, e.Anchor, 0)
	if len(start) == 0 {
		return 0, 0, AnchorNotFound{Field: anchorField, Anchor: e.Anchor}
	}
	if len(start) > 1 {
		return 0, 0, AmbiguousAnchor{Field: anchorField, Anchor: e.Anchor, Matches: start}
	}
	from := start[0] - 1
	end := matchLines(lines, e.Until, from)
	switch {
	case len(end) == 0:
		return 0, 0, AnchorNotFound{Field: untilField, Anchor: e.Until}
	case e.UntilOccurrence > len(end):
		return 0, 0, OccurrenceOutOfRange{Anchor: e.Until, Wanted: e.UntilOccurrence, Found: len(end)}
	case e.UntilOccurrence > 0:
		return from, end[e.UntilOccurrence-1] - 1, nil
	case len(end) > 1:
		return 0, 0, AmbiguousAnchor{Field: untilField, Anchor: e.Until, Matches: end}
	}
	return from, end[0] - 1, nil
}

func (e Edit) On(before string) (string, error) {
	switch e.Kind {
	case Create:
		return e.Text, nil
	case Prepend:
		return e.Text + before, nil
	case Replace:
		lines := splitLines(before)
		from, to, err := e.locate(lines)
		if err != nil {
			return "", err
		}
		text := e.Text
		if text != "" && !strings.HasSuffix(text, "\n") && strings.HasSuffix(lines[to], "\n") {
			text += "\n"
		}
		return strings.Join(lines[:from], "") + text + strings.Join(lines[to+1:], ""), nil
	default:
		return "", fmt.Errorf("edit kind %q is not one this catalogue holds", e.Kind)
	}
}

func Derive(before, after string) ([]Edit, error) {
	if before == "" {
		return []Edit{{Kind: Create, Text: after}}, nil
	}
	old := splitLines(before)
	var edits []Edit
	for _, hunk := range Hunks(before, after) {
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
		if matches := matchLines(old, edit.Until, from); len(matches) > 1 {
			edit.UntilOccurrence = slices.Index(matches, to+1) + 1
		}
		gotFrom, gotTo, err := edit.locate(old)
		if err != nil {
			return nil, err
		}
		if gotFrom != from || gotTo != to {
			return nil, fmt.Errorf("the anchors name lines %d to %d where the change is %d to %d",
				gotFrom+1, gotTo+1, from+1, to+1)
		}
		edits = append(edits, edit)
	}
	return edits, nil
}
