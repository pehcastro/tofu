package search

import (
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Candidate string

const (
	Literal         Candidate = "literal"
	WordBoundary    Candidate = "word boundary"
	CaseInsensitive Candidate = "case insensitive"
	GoSymbol        Candidate = "go symbol"
	NoCandidate     Candidate = "none"
)

type Attempt struct {
	Candidate Candidate
	Pattern   string
	Lines     int
	Files     int
	Spent     time.Duration
	NotRun    string
}

func (a Attempt) String() string {
	if a.NotRun != "" {
		return string(a.Candidate) + ": not run, " + a.NotRun
	}
	return string(a.Candidate) + " " + a.Pattern + ": " + plural(a.Lines, "line") + " in " + plural(a.Files, "file")
}

func plural(count int, word string) string {
	if count == 1 {
		return "1 " + word
	}
	return strconv.Itoa(count) + " " + word + "s"
}

type sweep struct {
	candidate Candidate
	pattern   *regexp.Regexp
	notRun    string
	hits      map[string][]hit
	lines     int
	files     int
	spent     time.Duration
}

func (s *sweep) scan(rel, source string) {
	started := time.Now()
	hits := hitLines(source, s.pattern)
	s.spent += time.Since(started)
	if len(hits) == 0 {
		return
	}
	s.hits[rel] = hits
	s.lines += len(hits)
	s.files++
}

func (s *sweep) attempt() Attempt {
	if s.notRun != "" {
		return Attempt{Candidate: s.candidate, NotRun: s.notRun}
	}
	return Attempt{Candidate: s.candidate, Pattern: s.pattern.String(), Lines: s.lines, Files: s.files, Spent: s.spent}
}

const subsetOfTheLiteral = "the literal matched no line, and this form matches a subset of the literal, so it can only match none either"

func caseInsensitiveSweep(source string) *sweep {
	if strings.Contains(source, "(?i") {
		return &sweep{candidate: CaseInsensitive, notRun: "the pattern already asks for a case insensitive match"}
	}
	return &sweep{candidate: CaseInsensitive, pattern: regexp.MustCompile("(?i)" + source), hits: map[string][]hit{}}
}

func wordBoundarySweep(source string, literal *sweep, matched map[string]string) *sweep {
	if len(matched) == 0 {
		return &sweep{candidate: WordBoundary, notRun: subsetOfTheLiteral}
	}
	if !plainWord(source) {
		return &sweep{candidate: WordBoundary, notRun: "the pattern is not a plain word, so a boundary around it would change what it means"}
	}
	boundary := &sweep{candidate: WordBoundary, pattern: regexp.MustCompile(`\b` + source + `\b`), hits: map[string][]hit{}}
	for rel, body := range matched {
		started := time.Now()
		lines := sourceLines(body)
		var kept []hit
		for _, where := range literal.hits[rel] {
			at := boundary.pattern.FindStringIndex(strings.TrimSuffix(lines[where.line-1], "\r"))
			if at != nil {
				kept = append(kept, hit{line: where.line, column: at[0]})
			}
		}
		boundary.spent += time.Since(started)
		if len(kept) == 0 {
			continue
		}
		boundary.hits[rel] = kept
		boundary.lines += len(kept)
		boundary.files++
	}
	return boundary
}

func plainWord(source string) bool {
	if source == "" {
		return false
	}
	for _, letter := range source {
		if letter != '_' && !unicode.IsLetter(letter) && !unicode.IsDigit(letter) {
			return false
		}
	}
	return true
}

func goSymbolAttempt(root, source string, matched map[string]string) (Attempt, error) {
	if len(matched) == 0 {
		return Attempt{Candidate: GoSymbol, NotRun: subsetOfTheLiteral}, nil
	}
	if !token.IsIdentifier(source) {
		return Attempt{
			Candidate: GoSymbol,
			NotRun:    "the pattern is not a go identifier, and a declaration is looked up here by its name alone",
		}, nil
	}
	var files []string
	for rel := range matched {
		if strings.HasSuffix(rel, ".go") {
			files = append(files, rel)
		}
	}
	if len(files) == 0 {
		return Attempt{
			Candidate: GoSymbol,
			NotRun:    "no go file matched, and go is the only language parsed here: a tree in another language gets the text forms and no symbol form",
		}, nil
	}
	slices.Sort(files)
	started := time.Now()
	graph, err := Symbols(root, files, source)
	if err != nil {
		return Attempt{}, err
	}
	spent := time.Since(started)
	where := make(map[string]bool, len(graph.Definitions)+len(graph.Callers))
	for _, definition := range graph.Definitions {
		where[definition.Path] = true
	}
	for _, site := range graph.Callers {
		where[site.Path] = true
	}
	return Attempt{
		Candidate: GoSymbol,
		Pattern:   source,
		Lines:     len(graph.Definitions) + len(graph.Callers),
		Files:     len(where),
		Spent:     spent,
	}, nil
}
