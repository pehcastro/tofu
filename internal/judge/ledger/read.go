package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const lineCeiling = 1 << 20

type Filter struct {
	Point   string
	Verdict Verdict
	Mode    Mode
	Version int
	TurnID  string
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
	path := filepath.Join(r.dir, day+logSuffix)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	outcomes, err := r.outcomes(day)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), lineCeiling)
	number := 0
	for scanner.Scan() {
		number++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		report.Scanned++
		var row Row
		if err := json.Unmarshal(line, &row); err != nil {
			report.Corrupt = append(report.Corrupt, Corrupt{File: path, Line: number, Err: err})
			continue
		}
		if row.Schema < 1 || row.Schema > SchemaVersion {
			report.Corrupt = append(report.Corrupt, Corrupt{File: path, Line: number,
				Err: fmt.Errorf("ledger: row schema %d, this build reads up to %d", row.Schema, SchemaVersion)})
			continue
		}
		if outcome, ok := outcomes[row.ID]; ok {
			attached := outcome
			row.Outcome = &attached
		}
		if !filter.match(row) {
			continue
		}
		report.Matched++
		if err := fn(row); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		report.Corrupt = append(report.Corrupt, Corrupt{File: path, Line: number + 1, Err: err})
	}
	return nil
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
	if f.TurnID != "" && row.TurnID != f.TurnID {
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
