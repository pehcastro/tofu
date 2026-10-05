package search

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
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
	Candidates  int
	Units       int
	Returned    int
	Fallbacks   int
	Skipped     int
	TooLarge    int
	Budget      int
	Tokens      int
	Scanned     int
	TotalFiles  int
	ScanStopped bool
	Read        time.Duration
}

type Result struct {
	Units    []Unit
	Stats    Stats
	Answered Candidate
	Tried    []Attempt
	Text     string
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

	source := req.Pattern.String()
	matched := make(map[string]string)
	var stats Stats
	stats.TotalFiles = len(req.Files)

	literal := &sweep{candidate: Literal, pattern: req.Pattern, hits: map[string][]hit{}}
	if err := scanTree(req, literal, matched, &stats); err != nil {
		return Result{}, err
	}
	insensitive := caseInsensitiveSweep(source)
	answering := literal
	switch {
	case literal.lines > 0:
		insensitive.notRun = "the literal answered, and a case insensitive form can only add lines that differ from it in case"
	case insensitive.pattern != nil:
		stats.Scanned, stats.Skipped, stats.TooLarge, stats.ScanStopped = 0, 0, 0, false
		if err := scanTree(req, insensitive, matched, &stats); err != nil {
			return Result{}, err
		}
		answering = insensitive
	}

	answered, pattern := NoCandidate, req.Pattern
	var found []Unit
	if answering.lines > 0 {
		answered, pattern, stats.Candidates = answering.candidate, answering.pattern, answering.lines
		for _, rel := range req.Files {
			if hits, ok := answering.hits[rel]; ok {
				found = append(found, unitsIn(rel, matched[rel], hits)...)
			}
		}
	}

	narrowOver := map[string]string{}
	if literal.lines > 0 {
		narrowOver = matched
	}
	symbol, err := goSymbolAttempt(req.Root, source, narrowOver)
	if err != nil {
		return Result{}, err
	}
	tried := []Attempt{
		literal.attempt(),
		wordBoundarySweep(source, literal, narrowOver).attempt(),
		insensitive.attempt(),
		symbol,
	}

	stats.Units = len(found)
	for i := range found {
		found[i].Tokens = konst.SearchUnitHeaderTokens + len(found[i].Body)/konst.SearchBytesPerToken
		if found[i].Fallback {
			stats.Fallbacks++
		}
	}

	kept := choose(found, pattern, budget)
	stats.Returned = len(kept)
	stats.Budget = budget
	stats.Tokens = konst.SearchResultOverhead
	for _, unit := range kept {
		stats.Tokens += unit.Tokens
	}
	result := Result{Units: kept, Stats: stats, Answered: answered, Tried: tried}
	result.Text, err = result.render()
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

type scannedFile struct {
	source   string
	hits     []hit
	binary   bool
	tooLarge bool
	read     time.Duration
	match    time.Duration
	err      error
}

func scanTree(req Request, into *sweep, matched map[string]string, stats *Stats) error {
	scanned := make([]chan scannedFile, len(req.Files))
	for index := range scanned {
		scanned[index] = make(chan scannedFile, 1)
	}
	var next atomic.Int64
	var stop atomic.Bool
	defer stop.Store(true)
	for range min(runtime.GOMAXPROCS(0), len(req.Files)) {
		go func() {
			for !stop.Load() {
				index := int(next.Add(1)) - 1
				if index >= len(req.Files) {
					return
				}
				scanned[index] <- scanFile(filepath.Join(req.Root, filepath.FromSlash(req.Files[index])), into.pattern)
			}
		}()
	}
	for index, rel := range req.Files {
		if into.lines >= konst.SearchCandidateScanCap {
			stats.ScanStopped = true
			return nil
		}
		file := <-scanned[index]
		stats.Read += file.read
		into.spent += file.match
		if file.err != nil {
			return fmt.Errorf("search: %w", file.err)
		}
		stats.Scanned++
		switch {
		case file.tooLarge:
			stats.TooLarge++
			continue
		case file.binary:
			stats.Skipped++
			continue
		}
		if len(file.hits) > 0 {
			into.record(rel, file.hits)
			matched[rel] = file.source
		}
	}
	return nil
}

func scanFile(full string, pattern *regexp.Regexp) scannedFile {
	started := time.Now()
	opened, err := os.Open(full)
	if err != nil {
		return scannedFile{err: err}
	}
	defer func() { _ = opened.Close() }()
	info, err := opened.Stat()
	if err != nil {
		return scannedFile{err: err}
	}
	if info.Size() > konst.SearchFileByteCap {
		return scannedFile{tooLarge: true}
	}
	var body bytes.Buffer
	body.Grow(int(info.Size()) + bytes.MinRead)
	_, err = body.ReadFrom(opened)
	file := scannedFile{read: time.Since(started), err: err, binary: bytes.IndexByte(body.Bytes(), 0) >= 0}
	if err != nil || file.binary {
		return file
	}
	started = time.Now()
	if prefix, _ := pattern.LiteralPrefix(); bytes.Contains(body.Bytes(), []byte(prefix)) {
		file.source = body.String()
		file.hits = hitLines(file.source, pattern)
	}
	file.match = time.Since(started)
	return file
}

func sourceLines(body string) []string {
	lines := strings.Split(body, "\n")
	if last := len(lines) - 1; last > 0 && lines[last] == "" {
		return lines[:last]
	}
	return lines
}

func hitLines(body string, pattern *regexp.Regexp) []hit {
	var hits []hit
	for index, line := range sourceLines(body) {
		at := pattern.FindStringIndex(strings.TrimSuffix(line, "\r"))
		if at == nil {
			continue
		}
		hits = append(hits, hit{line: index + 1, column: at[0]})
	}
	return hits
}

func unitsIn(rel, body string, hits []hit) []Unit {
	lines := sourceLines(body)
	if strings.HasSuffix(rel, ".go") {
		if units, parsed := goUnits(rel, body, lines, hits); parsed {
			return units
		}
	}
	columns := make(map[int]int)
	for _, where := range hits {
		for number := max(1, where.line-konst.SearchFrameLines); number <= min(len(lines), where.line+konst.SearchFrameLines); number++ {
			columns[number] = 0
		}
	}
	for _, where := range hits {
		columns[where.line] = where.column
	}
	for number, column := range columns {
		lines[number-1] = cutAround(lines[number-1], column)
	}
	var units []Unit
	for _, where := range hits {
		units = merge(units, frameUnit(rel, lines, where, NotParsed, true), lines)
	}
	return units
}

func cutAround(line string, column int) string {
	if len(line) <= konst.SearchLineWidth {
		return line
	}
	line = strings.TrimSuffix(line, "\r")
	start := max(0, min(column-konst.SearchLineWidth/2, len(line)-konst.SearchLineWidth))
	end := min(len(line), start+konst.SearchLineWidth)
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	kept := line[start:end]
	if start > 0 {
		kept = fmt.Sprintf("[%d characters cut] %s", utf8.RuneCountInString(line[:start]), kept)
	}
	if end < len(line) {
		kept = fmt.Sprintf("%s [%d characters cut]", kept, utf8.RuneCountInString(line[end:]))
	}
	return kept
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
