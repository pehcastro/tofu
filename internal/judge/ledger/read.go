package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"tofu/internal/platform/fs"
)

const lineCeiling = 1 << 20

const newestChunkBytes = 1 << 16

type Filter struct {
	Point   string
	Verdict Verdict
	Mode    Mode
	Version int
	TurnID  string
	Turns   map[string]bool
	Origin  Origin
	Since   time.Time
	Until   time.Time
}

type Corrupt struct {
	File string
	Line int
	Err  error
}

type Report struct {
	Files   int
	Scanned int
	Matched int
	Corrupt []Corrupt
}

type Reader struct {
	dir string
}

func NewReader(dir string) *Reader {
	return &Reader{dir: dir}
}

var errRowFound = errors.New("ledger: row found")

type EscapingStateError struct {
	File string
	Dir  string
}

func (e EscapingStateError) Error() string {
	return fmt.Sprintf("ledger: a row names %s as its state, which is outside the ledger at %s: refused, not read", e.File, e.Dir)
}

func (r *Reader) State(row Row) (json.RawMessage, error) {
	if row.StateElision == nil {
		return row.State, nil
	}
	named := row.StateElision.File
	segments := strings.FieldsFunc(named, func(letter rune) bool { return letter == '/' || letter == '\\' })
	if len(segments) == 0 || filepath.IsAbs(named) || slices.Contains(segments, "..") {
		return nil, EscapingStateError{File: named, Dir: r.dir}
	}
	return fs.ReadFile(filepath.Join(r.dir, named))
}

func (r *Reader) ByID(id string) (Row, bool, error) {
	day, err := dayOfID(id)
	if err != nil {
		return Row{}, false, err
	}
	start, err := time.Parse(dayLayout, day)
	if err != nil {
		return Row{}, false, err
	}
	filter := Filter{Since: start, Until: start.AddDate(0, 0, 1)}
	var found Row
	_, err = r.Each(filter, func(row Row) error {
		if row.ID != id {
			return nil
		}
		found = row
		return errRowFound
	})
	if err != nil && !errors.Is(err, errRowFound) {
		return Row{}, false, err
	}
	return found, found.ID == id, nil
}

func (r *Reader) Each(filter Filter, fn func(Row) error) (Report, error) {
	var report Report
	days, err := r.days()
	if err != nil {
		return report, err
	}
	for _, day := range days {
		if !filter.coversDay(day) {
			continue
		}
		report.Files++
		if err := r.eachInDay(day, filter, &report, fn); err != nil {
			return report, err
		}
	}
	return report, nil
}

func (r *Reader) days() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var days []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, outcomeSuffix) || !strings.HasSuffix(name, logSuffix) {
			continue
		}
		day := strings.TrimSuffix(name, logSuffix)
		if _, err := time.Parse(dayLayout, day); err != nil {
			continue
		}
		days = append(days, day)
	}
	sort.Strings(days)
	return days, nil
}

func (r *Reader) eachInDay(day string, filter Filter, report *Report, fn func(Row) error) error {
	outcomes, err := r.outcomes(day)
	if err != nil {
		return err
	}
	path := r.dayPath(day)
	cut, err := eachLine(path, func(number int, line []byte) error {
		report.Scanned++
		row, err := parseRow(line)
		if err != nil {
			report.Corrupt = append(report.Corrupt, Corrupt{File: path, Line: number, Err: err})
			return nil
		}
		if outcome, ok := outcomes[row.ID]; ok {
			row.Outcome = &outcome
		}
		if !filter.match(row) {
			return nil
		}
		report.Matched++
		return fn(row)
	})
	if cut != nil {
		report.Corrupt = append(report.Corrupt, *cut)
	}
	return err
}

func (r *Reader) dayPath(day string) string { return filepath.Join(r.dir, day+logSuffix) }

func eachLine(path string, each func(number int, line []byte) error) (*Corrupt, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), lineCeiling)
	number := 0
	for scanner.Scan() {
		number++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		if err := each(number, scanner.Bytes()); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return &Corrupt{File: path, Line: number + 1, Err: err}, nil
	}
	return nil, nil
}

func atOnce(n int, work func(i int)) {
	workers := min(n, runtime.GOMAXPROCS(0))
	var group sync.WaitGroup
	for first := range workers {
		group.Go(func() {
			for i := first; i < n; i += workers {
				work(i)
			}
		})
	}
	group.Wait()
}

func parseRow(line []byte) (Row, error) {
	var row Row
	if err := json.Unmarshal(line, &row); err != nil {
		return Row{}, err
	}
	return row, readableSchema(row.Schema)
}

func readableSchema(schema int) error {
	if schema < 1 || schema > SchemaVersion {
		return fmt.Errorf("ledger: row schema %d, this build reads up to %d", schema, SchemaVersion)
	}
	return nil
}

func (r *Reader) Newest(filter Filter) (Row, bool, error) {
	days, err := r.days()
	if err != nil {
		return Row{}, false, err
	}
	days = slices.DeleteFunc(days, func(day string) bool { return !filter.coversDay(day) })
	slices.Reverse(days)
	var point []byte
	if filter.Point != "" {
		point, _ = json.Marshal(filter.Point)
	}
	if point == nil || len(days) < 2 {
		return r.newestIn(days, filter, point)
	}
	if row, found, err := r.newestIn(days[:1], filter, point); found || err != nil {
		return row, found, err
	}
	days, err = r.naming(days[1:], point)
	if err != nil {
		return Row{}, false, err
	}
	return r.newestIn(days, filter, point)
}

func (r *Reader) naming(days []string, point []byte) ([]string, error) {
	named, errs := make([]bool, len(days)), make([]error, len(days))
	atOnce(len(days), func(i int) { named[i], errs[i] = fileContains(r.dayPath(days[i]), point) })
	var kept []string
	for i, day := range days {
		if named[i] {
			kept = append(kept, day)
		}
	}
	return kept, errors.Join(errs...)
}

func fileContains(path string, text []byte) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	chunk, carried := make([]byte, newestChunkBytes+len(text)), 0
	for {
		read, err := file.Read(chunk[carried:])
		seen := chunk[:carried+read]
		if bytes.Contains(seen, text) {
			return true, nil
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		carried = copy(chunk, seen[max(len(seen)-len(text)+1, 0):])
	}
}

func (r *Reader) newestIn(days []string, filter Filter, point []byte) (Row, bool, error) {
	for _, day := range days {
		row, found, err := r.lastInDay(day, filter, point)
		if err != nil {
			return Row{}, false, err
		}
		if !found {
			continue
		}
		outcomes, err := r.outcomes(day)
		if outcome, ok := outcomes[row.ID]; ok {
			row.Outcome = &outcome
		}
		return row, true, err
	}
	return Row{}, false, nil
}

func (r *Reader) lastInDay(day string, filter Filter, point []byte) (Row, bool, error) {
	file, err := os.Open(r.dayPath(day))
	if err != nil {
		return Row{}, false, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Row{}, false, err
	}
	unread, rest := info.Size(), []byte{}
	for {
		cut := bytes.LastIndexByte(rest, '\n')
		if cut < 0 && unread > 0 {
			from := max(unread-newestChunkBytes, 0)
			chunk := make([]byte, unread-from)
			if _, err := file.ReadAt(chunk, from); err != nil {
				return Row{}, false, err
			}
			rest, unread = append(chunk, rest...), from
			continue
		}
		if line := rest[cut+1:]; point == nil || bytes.Contains(line, point) {
			if row, err := parseRow(line); err == nil && filter.match(row) {
				return row, true, nil
			}
		}
		if cut < 0 {
			return Row{}, false, nil
		}
		rest = rest[:cut]
	}
}

func (r *Reader) outcomes(day string) (map[string]Outcome, error) {
	path := filepath.Join(r.dir, day+outcomeSuffix)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	found := make(map[string]Outcome)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), lineCeiling)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record outcomeRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.ID == "" || record.Schema < 1 || record.Schema > SchemaVersion {
			continue
		}
		found[record.ID] = record.Outcome
	}
	return found, scanner.Err()
}

func (f Filter) coversDay(day string) bool {
	parsed, err := time.Parse(dayLayout, day)
	if err != nil {
		return false
	}
	if !f.Since.IsZero() && parsed.AddDate(0, 0, 1).Before(f.Since.UTC()) {
		return false
	}
	if !f.Until.IsZero() && parsed.After(f.Until.UTC()) {
		return false
	}
	return true
}

func (f Filter) match(row Row) bool {
	if f.Point != "" && row.Point != f.Point {
		return false
	}
	if f.Verdict != VerdictUnset && row.Verdict != f.Verdict {
		return false
	}
	if f.Mode != ModeUnknown && row.Mode() != f.Mode {
		return false
	}
	if f.Version != 0 && row.Version != f.Version {
		return false
	}
	if f.TurnID != "" && row.TurnID != f.TurnID || f.Turns != nil && !f.Turns[row.TurnID] {
		return false
	}
	if f.Origin != OriginAny && row.Origin() != f.Origin {
		return false
	}
	if !f.Since.IsZero() && row.At.Before(f.Since.UTC()) {
		return false
	}
	if !f.Until.IsZero() && row.At.After(f.Until.UTC()) {
		return false
	}
	return true
}
