package search

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"boji/internal/konst"
)

type Placement string

const (
	InCode    Placement = "code"
	InComment Placement = "comment"
	InString  Placement = "string"
	NotParsed Placement = "unparsed"
)

type Kind string

const (
	KindFunc   Kind = "func"
	KindMethod Kind = "method"
	KindType   Kind = "type"
	KindValue  Kind = "value"
	KindImport Kind = "import"
	KindLines  Kind = "lines"
)

type Match struct {
	Line      int
	Placement Placement
}

type Unit struct {
	Path      string
	Kind      Kind
	Symbol    string
	FirstLine int
	LastLine  int
	Matches   []Match
	Body      string
	Fallback  bool
	Tokens    int
}

type Stats struct {
	Candidates int
	Units      int
	Returned   int
	Fallbacks  int
	Skipped    int
	Budget     int
	Tokens     int
}

type Result struct {
	Units []Unit
	Stats Stats
	Text  string
}

type Request struct {
	Root      string
	Files     []string
	Pattern   *regexp.Regexp
	MaxTokens int
}

type hit struct {
	line   int
	column int
}

func Find(req Request) (Result, error) {
	if req.Pattern == nil {
		return Result{}, errors.New("search: a pattern is required")
	}
	budget := req.MaxTokens
	if budget <= 0 {
		budget = konst.SearchTokenBudget
	}

	var found []Unit
	var stats Stats
	for _, rel := range req.Files {
		body, err := os.ReadFile(filepath.Join(req.Root, filepath.FromSlash(rel)))
		if err != nil {
			return Result{}, fmt.Errorf("search: %w", err)
		}
		if bytes.IndexByte(body, 0) >= 0 {
			stats.Skipped++
			continue
		}
		source := string(body)
		hits := hitLines(source, req.Pattern)
		if len(hits) == 0 {
			continue
		}
		stats.Candidates += len(hits)
		found = append(found, unitsIn(rel, source, hits)...)
	}

	stats.Units = len(found)
	for i := range found {
		found[i].Tokens = konst.SearchUnitHeaderTokens + len(found[i].Body)/konst.SearchBytesPerToken
		if found[i].Fallback {
			stats.Fallbacks++
		}
	}

	kept := choose(found, req.Pattern, budget)
	stats.Returned = len(kept)
	stats.Budget = budget
	stats.Tokens = konst.SearchResultOverhead
	for _, unit := range kept {
		stats.Tokens += unit.Tokens
	}
	text, err := render(kept, stats)
	if err != nil {
		return Result{}, err
	}
	return Result{Units: kept, Stats: stats, Text: text}, nil
}

func hitLines(body string, pattern *regexp.Regexp) []hit {
	var hits []hit
	for index, line := range strings.Split(body, "\n") {
		at := pattern.FindStringIndex(strings.TrimSuffix(line, "\r"))
		if at == nil {
			continue
		}
		hits = append(hits, hit{line: index + 1, column: at[0]})
	}
	return hits
}

func unitsIn(rel, body string, hits []hit) []Unit {
	lines := strings.Split(body, "\n")
	if strings.HasSuffix(rel, ".go") {
		if units, parsed := goUnits(rel, body, lines, hits); parsed {
			return units
		}
	}
	var units []Unit
	for _, where := range hits {
		units = merge(units, frameUnit(rel, lines, where, NotParsed, true), lines)
	}
	return units
}

func frameUnit(rel string, lines []string, where hit, placement Placement, fallback bool) Unit {
	first := max(1, where.line-konst.SearchFrameLines)
	last := min(len(lines), where.line+konst.SearchFrameLines)
	return Unit{
		Path:      rel,
		Kind:      KindLines,
		FirstLine: first,
		LastLine:  last,
		Matches:   []Match{{Line: where.line, Placement: placement}},
		Body:      strings.Join(lines[first-1:last], "\n"),
		Fallback:  fallback,
	}
}

func merge(units []Unit, next Unit, lines []string) []Unit {
	if len(units) > 0 {
		last := &units[len(units)-1]
		if last.Path == next.Path && last.Kind == next.Kind && last.Fallback == next.Fallback && next.FirstLine <= last.LastLine {
			last.Matches = append(last.Matches, next.Matches...)
			if next.LastLine > last.LastLine {
				last.LastLine = next.LastLine
				last.Body = strings.Join(lines[last.FirstLine-1:last.LastLine], "\n")
			}
			return units
		}
	}
	return append(units, next)
}

func choose(units []Unit, pattern *regexp.Regexp, budget int) []Unit {
	ranked := slices.Clone(units)
	slices.SortStableFunc(ranked, func(left, right Unit) int {
		return cmp.Or(
			cmp.Compare(score(right, pattern), score(left, pattern)),
			cmp.Compare(left.Path, right.Path),
			cmp.Compare(left.FirstLine, right.FirstLine))
	})

	var kept []Unit
	taken := make([]bool, len(ranked))
	seen := make(map[string]bool, len(ranked))
	spent := konst.SearchResultOverhead
	for pass := range 2 {
		for index, unit := range ranked {
			if taken[index] || (pass == 0 && seen[unit.Path]) || spent+unit.Tokens > budget {
				continue
			}
			taken[index] = true
			seen[unit.Path] = true
			spent += unit.Tokens
			kept = append(kept, unit)
		}
	}
	return kept
}

func score(unit Unit, pattern *regexp.Regexp) int {
	points := min(len(unit.Matches), 3)
	if unit.Symbol != "" && pattern.MatchString(unit.Symbol) {
		points += 5
	}
	for _, match := range unit.Matches {
		if match.Placement == InCode {
			points += 2
			break
		}
	}
	return points
}

func render(units []Unit, stats Stats) (string, error) {
	fallbacks := "fallbacks"
	if stats.Fallbacks == 1 {
		fallbacks = "fallback"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%d text candidates, %d units, %d returned, %d %s, about %d tokens\n",
		stats.Candidates, stats.Units, stats.Returned, stats.Fallbacks, fallbacks, stats.Tokens)
	if stats.Fallbacks > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Fallback, fmt.Sprintf("%d of the %d units are the lines around the match rather than a declaration", stats.Fallbacks, stats.Units)))
	}
	if stats.Returned < stats.Units {
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("%d units matched and %d fit the %d token budget: raise max_tokens or narrow the pattern to see the rest", stats.Units, stats.Returned, stats.Budget)))
	}
	if stats.Skipped > 0 {
		fmt.Fprintf(&out, "%s\n", Note(BinarySkipped, fmt.Sprintf("%d files hold a null byte and were not read", stats.Skipped)))
	}
	if len(units) == 0 {
		out.WriteString("no code unit holds that pattern. the files were read: this is an answer, not a failure\n")
		return out.String(), nil
	}
	out.WriteString("each result is the whole declaration the match sits inside, not the matching line\n")
	for _, unit := range units {
		note, err := matchNote(unit.Matches)
		if err != nil {
			return "", err
		}
		what := string(unit.Kind)
		if unit.Symbol != "" {
			what += " " + unit.Symbol
		}
		fmt.Fprintf(&out, "\n%s:%d-%d %s, %s\n%s\n", unit.Path, unit.FirstLine, unit.LastLine, what, note, unit.Body)
	}
	return out.String(), nil
}

func matchNote(matches []Match) (string, error) {
	parts := make([]string, 0, len(matches))
	for index, match := range matches {
		if index == konst.SearchMatchLinesListed {
			parts = append(parts, fmt.Sprintf("and %d more", len(matches)-index))
			break
		}
		phrase, err := match.Placement.phrase()
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("line %d in %s", match.Line, phrase))
	}
	return "matched on " + strings.Join(parts, ", "), nil
}

func (p Placement) phrase() (string, error) {
	switch p {
	case InCode:
		return "code", nil
	case InComment:
		return "a comment", nil
	case InString:
		return "a string literal", nil
	case NotParsed:
		return "a file this tool does not parse", nil
	}
	return "", fmt.Errorf("search: %q is not a placement", string(p))
}
