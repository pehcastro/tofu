package search

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const (
	FilesPerPage = 20
	Unbudgeted   = 1 << 30
)

var unitHeaderPattern = sys.LazyRegexp(`(?m)^(\S+):\d+-\d+ `)

func OfferedPaths(rendered string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, match := range unitHeaderPattern().FindAllStringSubmatch(rendered, -1) {
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
	return r.Page(0, r.Stats.Budget)
}

func (r Result) Page(skip, budget int) (string, error) {
	fileAt := map[string]int{}
	for _, unit := range r.Units {
		if _, seen := fileAt[unit.Path]; !seen {
			fileAt[unit.Path] = len(fileAt)
		}
	}
	files := len(fileAt)
	first, last := min(skip, files), min(skip+FilesPerPage, files)
	listsLines := files > FilesPerPage
	stats := &r.Stats
	stats.Budget, stats.Tokens = budget, konst.SearchResultOverhead
	var units []Unit
	inWindow := 0
	for _, unit := range r.Units {
		if at := fileAt[unit.Path]; at < first || at >= last {
			continue
		}
		inWindow++
		if listsLines {
			unit.Body = matchedLines(unit)
			unit.Tokens = (len(unit.Path) + len(unit.Symbol) + len(unit.Body)) / konst.SearchBytesPerToken
		}
		if stats.Tokens+unit.Tokens > budget {
			continue
		}
		stats.Tokens += unit.Tokens
		units = append(units, unit)
	}
	slices.SortStableFunc(units, func(left, right Unit) int {
		return cmp.Or(cmp.Compare(fileAt[left.Path], fileAt[right.Path]), cmp.Compare(left.FirstLine, right.FirstLine))
	})
	stats.Returned = len(units)

	var out strings.Builder
	fmt.Fprintf(&out, "%d text candidates, %d units in %s, %d returned, %s, about %d tokens\n",
		stats.Candidates, stats.Units, plural(files, "file"), stats.Returned, plural(stats.Fallbacks, "fallback"), stats.Tokens)
	r.sayWhichFormAnswered(&out)
	if stats.Fallbacks > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Fallback, fmt.Sprintf("%d of the %d units are the lines around the match rather than a declaration", stats.Fallbacks, stats.Units)))
	}
	if stats.ScanStopped {
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("the scan stopped after %d of %d files because %d candidate lines had already been found: narrow the pattern or the path to see the rest", stats.Scanned, stats.TotalFiles, stats.Candidates)))
	}
	switch {
	case files > 0 && first == files:
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("skip %d is past the end, since %s hold a match: skip 0 shows the first page", skip, plural(files, "file"))))
	case first > 0 || last < files:
		next := "this is the last page"
		if last < files {
			next = fmt.Sprintf("skip %d shows the next page, or narrow the path or the pattern", last)
		}
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("%s hold a match and files %d to %d are shown, %d a page: %s", plural(files, "file"), first+1, last, FilesPerPage, next)))
	}
	if stats.Returned < inWindow {
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("%d units on this page matched and %d fit the %d token budget: raise max_tokens or narrow the pattern to see the rest", inWindow, stats.Returned, stats.Budget)))
	}
	if stats.Skipped > 0 {
		fmt.Fprintf(&out, "%s\n", Note(BinarySkipped, fmt.Sprintf("%d files hold a null byte and were not read", stats.Skipped)))
	}
	if stats.TooLarge > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Truncated, fmt.Sprintf("%s over the %d MB cap went unread", plural(stats.TooLarge, "file"), konst.SearchFileByteCap>>20)))
	}
	if stats.Unreadable > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Partial, fmt.Sprintf("%s could not be read, the first %s", plural(stats.Unreadable, "file"), stats.FirstFailed)))
	}
	if len(r.Units) == 0 {
		read := "the files were read"
		switch {
		case stats.TooLarge > 0 && stats.Unreadable > 0:
			read = "every readable file under the size cap was read, and the unreadable ones and the ones over the cap may hold a match"
		case stats.TooLarge > 0:
			read = "every file under the size cap was read, and the ones over it may hold a match"
		case stats.Unreadable > 0:
			read = "every readable file was read, and the unreadable ones may hold a match"
		}
		out.WriteString("no code unit holds that pattern")
		if r.Answered != NoCandidate {
			fmt.Fprintf(&out, ". %s: this is an answer, not a failure\n", read)
			return out.String(), nil
		}
		fmt.Fprintf(&out, ", and no candidate form finds one either. %s: this is an absence, not a failure\n", read)
		for _, attempt := range r.Tried {
			fmt.Fprintf(&out, "  %s\n", attempt)
		}
		return out.String(), nil
	}
	switch {
	case len(units) == 0:
	case listsLines:
		fmt.Fprintf(&out, "the matches span more than %d files, so each result is the declaration a match sits in and its matching lines: narrow the path to read whole declarations\n", FilesPerPage)
	default:
		out.WriteString("each result is the whole declaration the match sits inside, not the matching line\n")
	}
	for _, unit := range units {
		what := string(unit.Kind)
		if unit.Symbol != "" {
			what += " " + unit.Symbol
		}
		if listsLines {
			fmt.Fprintf(&out, "%s:%d-%d %s\n%s\n", unit.Path, unit.FirstLine, unit.LastLine, what, unit.Body)
			continue
		}
		note, err := matchNote(unit.Matches)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "\n%s:%d-%d %s, %s\n%s\n", unit.Path, unit.FirstLine, unit.LastLine, what, note, unit.Body)
	}
	return out.String(), nil
}

func matchedLines(unit Unit) string {
	body := strings.Split(unit.Body, "\n")
	var lines []string
	for index, match := range unit.Matches {
		if index == konst.SearchMatchLinesListed {
			lines = append(lines, fmt.Sprintf("  and %d more matching lines", len(unit.Matches)-index))
			break
		}
		if at := match.Line - unit.FirstLine; at >= 0 && at < len(body) {
			lines = append(lines, fmt.Sprintf("  %d: %s", match.Line, strings.TrimSpace(body[at])))
		}
	}
	return strings.Join(lines, "\n")
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
