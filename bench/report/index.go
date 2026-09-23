package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type State string

const (
	StateStands          State = "stands"
	StateStale           State = "stale"
	StateWithdrawnInPart State = "withdrawn in part"
	StateWithdrawn       State = "withdrawn"
)

func (s State) Stands() bool { return s == StateStands }

type PackageKind string

const (
	KindLibrary     PackageKind = "library"
	KindRunner      PackageKind = "runner"
	KindUnpublished PackageKind = "measurement with no dated report"
)

const (
	SelfDeclared    = "the file's own text"
	WithdrawalsPath = "bench/report/withdrawals.json"
	PackagesPath    = "bench/report/packages.json"
	Unparsed        = "unparsed: no heading in this file names its own answer"
	RegenerateWith  = "go run ./bench/report/gen"
)

var datedReport = regexp.MustCompile(`^report-(\d{4}-\d{2}-\d{2})(?:-[a-z0-9-]+)?\.[a-z]+$`)

type datedEntry struct {
	Package     string
	Date        string
	Path        string
	Conclusion  string
	State       State
	StateSource string
	StateNote   string
	Fell        []string
}

type withdrawal struct {
	Report string   `json:"report"`
	State  State    `json:"state"`
	Why    string   `json:"why"`
	Fell   []string `json:"fell"`
}

type Package struct {
	Package string      `json:"package"`
	Kind    PackageKind `json:"kind"`
	Note    string      `json:"note"`
}

type Counts struct {
	Reports         int `json:"reports"`
	MeasuredPackage int `json:"packages_with_a_dated_report"`
	NoReport        int `json:"packages_with_no_dated_report"`
	Withdrawn       int `json:"withdrawn_whole_or_in_part"`
	Stale           int `json:"stale"`
	Unparsed        int `json:"unparsed"`
}

type benchIndex struct {
	Entries  []datedEntry
	NoReport []Package
	Counts   Counts
}

func readIndex(benchRoot string) (benchIndex, error) {
	withdrawals, err := readWithdrawals(filepath.Join(benchRoot, "report", "withdrawals.json"))
	if err != nil {
		return benchIndex{}, err
	}
	declared, err := readPackages(filepath.Join(benchRoot, "report", "packages.json"))
	if err != nil {
		return benchIndex{}, err
	}
	dirs, err := os.ReadDir(benchRoot)
	if err != nil {
		return benchIndex{}, fmt.Errorf("reading %s: %w", benchRoot, err)
	}
	var index benchIndex
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		found, err := readPackageReports(benchRoot, dir.Name(), withdrawals)
		if err != nil {
			return benchIndex{}, err
		}
		if len(found) > 0 {
			index.Entries = append(index.Entries, found...)
			index.Counts.MeasuredPackage++
			continue
		}
		pkg, named := declared[dir.Name()]
		if !named {
			return benchIndex{}, fmt.Errorf("bench/%s has no dated report and is not named in %s: say whether it is a library, a runner or a measurement still owing one", dir.Name(), PackagesPath)
		}
		delete(declared, dir.Name())
		index.NoReport = append(index.NoReport, pkg)
	}
	for name := range declared {
		return benchIndex{}, fmt.Errorf("%s names bench/%s, which either has a dated report now or does not exist", PackagesPath, name)
	}
	for path := range withdrawals {
		return benchIndex{}, fmt.Errorf("%s names %s, which is not a dated report under bench/", WithdrawalsPath, path)
	}
	sort.Slice(index.Entries, func(i, j int) bool {
		if index.Entries[i].Date != index.Entries[j].Date {
			return index.Entries[i].Date > index.Entries[j].Date
		}
		return index.Entries[i].Path < index.Entries[j].Path
	})
	sort.Slice(index.NoReport, func(i, j int) bool { return index.NoReport[i].Package < index.NoReport[j].Package })
	index.Counts.Reports = len(index.Entries)
	index.Counts.NoReport = len(index.NoReport)
	for _, entry := range index.Entries {
		switch entry.State {
		case StateWithdrawn, StateWithdrawnInPart:
			index.Counts.Withdrawn++
		case StateStale:
			index.Counts.Stale++
		case StateStands:
		}
		if entry.Conclusion == Unparsed {
			index.Counts.Unparsed++
		}
	}
	return index, nil
}

func readPackageReports(benchRoot, pkg string, withdrawals map[string]withdrawal) ([]datedEntry, error) {
	files, err := os.ReadDir(filepath.Join(benchRoot, pkg))
	if err != nil {
		return nil, fmt.Errorf("reading bench/%s: %w", pkg, err)
	}
	var entries []datedEntry
	for _, file := range files {
		match := datedReport.FindStringSubmatch(file.Name())
		if file.IsDir() || match == nil {
			continue
		}
		path := "bench/" + pkg + "/" + file.Name()
		body, err := os.ReadFile(filepath.Join(benchRoot, pkg, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		text := string(body)
		entry := datedEntry{
			Package:    pkg,
			Date:       match[1],
			Path:       path,
			Conclusion: conclusionOf(text),
			State:      StateStands,
		}
		if state, note := selfDeclaredState(text); state != StateStands {
			entry.State, entry.StateSource, entry.StateNote = state, SelfDeclared, note
		}
		if outside, ok := withdrawals[path]; ok {
			entry.State, entry.StateSource, entry.StateNote = outside.State, WithdrawalsPath, outside.Why
			entry.Fell = outside.Fell
			delete(withdrawals, path)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

const tableRowMark = "|"

func fellIn(report, text string, fell []string) ([]string, error) {
	said, lines := flatten(text), strings.Split(text, "\n")
	struck := make([]string, 0, len(fell))
	for _, quotation := range fell {
		want := flatten(quotation)
		if !strings.Contains(said, want) {
			return nil, fmt.Errorf("%s: %s says %q falls, and that sentence does not appear in the report, so nothing on the page would be struck", WithdrawalsPath, report, quotation)
		}
		if !strings.HasPrefix(want, tableRowMark) {
			struck = append(struck, want)
			continue
		}
		struck = append(struck, tableHolding(lines, want)...)
	}
	return struck, nil
}

func tableHolding(lines []string, row string) []string {
	at := slices.IndexFunc(lines, func(line string) bool { return flatten(line) == row })
	if at < 0 {
		return []string{row}
	}
	first, last := at, at
	for first > 0 && strings.HasPrefix(flatten(lines[first-1]), tableRowMark) {
		first--
	}
	for last+1 < len(lines) && strings.HasPrefix(flatten(lines[last+1]), tableRowMark) {
		last++
	}
	table := make([]string, 0, last-first+1)
	for _, line := range lines[first : last+1] {
		table = append(table, flatten(line))
	}
	return table
}

func readWithdrawals(path string) (map[string]withdrawal, error) {
	var list []withdrawal
	if err := readJSON(path, &list); err != nil {
		return nil, err
	}
	tree := filepath.Join(path, "..", "..", "..")
	byReport := make(map[string]withdrawal, len(list))
	for _, item := range list {
		switch item.State {
		case StateWithdrawnInPart:
			if len(item.Fell) == 0 {
				return nil, fmt.Errorf("%s: %s is withdrawn in part and quotes no sentence under fell, so the renderer cannot tell the half that fell from the half that stands", WithdrawalsPath, item.Report)
			}
		case StateWithdrawn, StateStale:
			if len(item.Fell) > 0 {
				return nil, fmt.Errorf("%s: %s is %q and quotes sentences under fell, which only a partial withdrawal does", WithdrawalsPath, item.Report, item.State)
			}
		case StateStands:
			return nil, fmt.Errorf("%s: %s is declared as %q, which is what a report is without an entry here", WithdrawalsPath, item.Report, item.State)
		default:
			return nil, fmt.Errorf("%s: %s carries the unknown state %q", WithdrawalsPath, item.Report, item.State)
		}
		if item.Why == "" {
			return nil, fmt.Errorf("%s: %s is withdrawn from outside itself and says no reason", WithdrawalsPath, item.Report)
		}
		body, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(item.Report)))
		if err == nil {
			if item.Fell, err = fellIn(item.Report, string(body), item.Fell); err != nil {
				return nil, err
			}
		}
		byReport[item.Report] = item
	}
	return byReport, nil
}

func readPackages(path string) (map[string]Package, error) {
	var list []Package
	if err := readJSON(path, &list); err != nil {
		return nil, err
	}
	byName := make(map[string]Package, len(list))
	for _, item := range list {
		switch item.Kind {
		case KindLibrary, KindRunner, KindUnpublished:
		default:
			return nil, fmt.Errorf("%s: bench/%s carries the unknown kind %q", PackagesPath, item.Package, item.Kind)
		}
		if item.Note == "" {
			return nil, fmt.Errorf("%s: bench/%s says nothing about why it has no dated report", PackagesPath, item.Package)
		}
		byName[item.Package] = item
	}
	return byName, nil
}

func readJSON(path string, into any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.ToSlash(path), err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("parsing %s: %w", filepath.ToSlash(path), err)
	}
	return nil
}
