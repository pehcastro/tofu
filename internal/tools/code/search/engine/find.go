package search

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
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
	"tofu/internal/safe"
	"tofu/internal/sys"
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
	Unreadable  int
	FirstFailed string
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
	Sizes     []int64
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
	if req.Sizes != nil && len(req.Sizes) != len(req.Files) {
		return Result{}, fmt.Errorf("search: %d sizes for %d files", len(req.Sizes), len(req.Files))
	}
	budget := req.MaxTokens
	if budget <= 0 {
		budget = konst.SearchTokenBudget
	}

	source := req.Pattern.String()
	matched := make(map[string]string)
	declared := make(map[string][]Unit)
	var stats Stats
	stats.TotalFiles = len(req.Files)

	literal := &sweep{candidate: Literal, pattern: req.Pattern, hits: map[string][]hit{}}
	scanTree(req, literal, matched, declared, &stats)
	insensitive := caseInsensitiveSweep(source)
	answering := literal
	switch {
	case literal.lines > 0:
		insensitive.notRun = "the literal answered, and a case insensitive form can only add lines that differ from it in case"
	case insensitive.pattern != nil:
		scanTree(req, insensitive, matched, declared, &stats)
		answering = insensitive
	}

	answered, pattern := NoCandidate, req.Pattern
	var found []Unit
	if answering.lines > 0 {
		answered, pattern, stats.Candidates = answering.candidate, answering.pattern, answering.lines
		for _, rel := range req.Files {
			found = append(found, declared[rel]...)
		}
	}

	narrowOver := map[string]string{}
	if literal.lines > 0 {
		narrowOver = matched
	}
	tried := []Attempt{
		literal.attempt(),
		wordBoundarySweep(source, literal, narrowOver).attempt(),
		insensitive.attempt(),
		goSymbolAttempt(source, narrowOver),
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
	text, err := result.render()
	if err != nil {
		return Result{}, err
	}
	result.Text = text
	return result, nil
}

type scannedFile struct {
	source   string
	hits     []hit
	units    []Unit
	binary   bool
	tooLarge bool
	read     time.Duration
	match    time.Duration
	err      error
}

func scanTree(req Request, into *sweep, matched map[string]string, declared map[string][]Unit, stats *Stats) {
	stats.Scanned, stats.Skipped, stats.TooLarge, stats.Unreadable, stats.FirstFailed, stats.ScanStopped = 0, 0, 0, 0, "", false
	scanned := make([]chan scannedFile, len(req.Files))
	for index := range scanned {
		scanned[index] = make(chan scannedFile, 1)
	}
	var next atomic.Int64
	var stop atomic.Bool
	defer stop.Store(true)
	report := sys.Crashes(req.Root)
	for range min(runtime.GOMAXPROCS(0), len(req.Files)) {
		safe.Go(report, "search scan", func() {
			var buffer []byte
			for !stop.Load() {
				index := int(next.Add(1)) - 1
				if index >= len(req.Files) {
					return
				}
				size := int64(-1)
				if req.Sizes != nil {
					size = req.Sizes[index]
				}
				var file scannedFile
				if err := safe.Call(report, "search scan", func() error {
					file, buffer = scanFile(req.Root, req.Files[index], size, into.pattern, buffer)
					return nil
				}); err != nil {
					file = scannedFile{err: err}
				}
				scanned[index] <- file
			}
		})
	}
	for index, rel := range req.Files {
		if into.lines >= konst.SearchCandidateScanCap {
			stats.ScanStopped = true
			return
		}
		file := <-scanned[index]
		stats.Read += file.read
		into.spent += file.match
		stats.Scanned++
		switch {
		case file.err != nil:
			if stats.Unreadable == 0 {
				stats.FirstFailed = failedRead(rel, file.err)
			}
			stats.Unreadable++
			continue
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
			declared[rel] = file.units
		}
	}
}

func failedRead(rel string, err error) string {
	return rel + ": " + strings.TrimSuffix(cmp.Or(errors.Unwrap(err), err).Error(), ".")
}

func scanFile(root, rel string, size int64, pattern *regexp.Regexp, buffer []byte) (scannedFile, []byte) {
	started := time.Now()
	opened, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return scannedFile{err: err}, buffer
	}
	defer func() { _ = opened.Close() }()
	if size < 0 || size > konst.SearchFileByteCap {
		info, err := opened.Stat()
		if err != nil {
			return scannedFile{err: err}, buffer
		}
		size = info.Size()
	}
	if size > konst.SearchFileByteCap {
		return scannedFile{tooLarge: true}, buffer
	}
	body, err := readSized(opened, size, buffer)
	if len(body) > konst.SearchFileByteCap {
		return scannedFile{tooLarge: true}, body
	}
	file := scannedFile{read: time.Since(started), err: err, binary: bytes.IndexByte(body, 0) >= 0}
	if err != nil || file.binary {
		return file, body
	}
	started = time.Now()
	if prefix, _ := pattern.LiteralPrefix(); bytes.Contains(body, []byte(prefix)) {
		file.source = string(body)
		file.hits = hitLines(file.source, pattern)
	}
	file.match = time.Since(started)
	if len(file.hits) > 0 {
		file.units = unitsIn(rel, file.source, file.hits)
	}
	return file, body
}

func readSized(opened *os.File, size int64, buffer []byte) ([]byte, error) {
	body := slices.Grow(buffer[:0], int(size)+bytes.MinRead)
	read, err := opened.Read(body[:size+1])
	body = body[:read]
	if errors.Is(err, io.EOF) || err == nil && int64(read) == size {
		return body, nil
	}
	if err != nil {
		return body, err
	}
	rest := bytes.NewBuffer(body)
	_, err = rest.ReadFrom(opened)
	return rest.Bytes(), err
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
	if units, parsed := declaredUnits(rel, body, lines, hits); parsed {
		return units
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
