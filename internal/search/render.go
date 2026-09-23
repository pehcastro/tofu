package search

import (
	"fmt"
	"regexp"
	"strings"

	"tofu/internal/konst"
)

var unitHeaderPattern = regexp.MustCompile(`(?m)^(\S+):\d+-\d+ `)

func OfferedPaths(rendered string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, match := range unitHeaderPattern.FindAllStringSubmatch(rendered, -1) {
		path := match[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func (r Result) render() (string, error) {
	units, stats := r.Units, r.Stats
	var out strings.Builder
	fmt.Fprintf(&out, "%d text candidates, %d units, %d returned, %s, about %d tokens\n",
		stats.Candidates, stats.Units, stats.Returned, plural(stats.Fallbacks, "fallback"), stats.Tokens)
	r.sayWhichFormAnswered(&out)
	if stats.Fallbacks > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Fallback, fmt.Sprintf("%d of the %d units are the lines around the match rather than a declaration", stats.Fallbacks, stats.Units)))
	}
	if stats.ScanStopped {
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("the scan stopped after %d of %d files because %d candidate lines had already been found: narrow the pattern or the path to see the rest", stats.Scanned, stats.TotalFiles, stats.Candidates)))
	}
	if stats.Returned < stats.Units {
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("%d units matched and %d fit the %d token budget: raise max_tokens or narrow the pattern to see the rest", stats.Units, stats.Returned, stats.Budget)))
	}
	if stats.Skipped > 0 {
		fmt.Fprintf(&out, "%s\n", Note(BinarySkipped, fmt.Sprintf("%d files hold a null byte and were not read", stats.Skipped)))
	}
	if len(units) == 0 {
		out.WriteString("no code unit holds that pattern")
		if r.Answered != NoCandidate {
			out.WriteString(". the files were read: this is an answer, not a failure\n")
			return out.String(), nil
		}
		out.WriteString(", and no candidate form finds one either. the files were read: this is an absence, not a failure\n")
		for _, attempt := range r.Tried {
			fmt.Fprintf(&out, "  %s\n", attempt)
		}
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

func (r Result) sayWhichFormAnswered(out *strings.Builder) {
	for _, attempt := range r.Tried {
		switch {
		case attempt.Candidate == r.Answered && r.Answered != Literal:
			fmt.Fprintf(out, "the literal pattern %s matched no line, and the %s form %s matched %s in %s: the pattern was wrong, not the answer absent\n",
				r.Tried[0].Pattern, attempt.Candidate, attempt.Pattern, plural(attempt.Lines, "line"), plural(attempt.Files, "file"))
		case attempt.Candidate == GoSymbol && attempt.Lines > 0 && r.Stats.Returned < r.Stats.Units:
			fmt.Fprintf(out, "%s is declared or called in %s across %s: ask for the symbol instead of narrowing the pattern by hand\n",
				attempt.Pattern, plural(attempt.Lines, "place"), plural(attempt.Files, "file"))
		}
	}
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
