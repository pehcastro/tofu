package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var onDisk = regexp.MustCompile(`^report-\d{4}-\d{2}-\d{2}.*\.(md|txt)$`)

func built(t *testing.T) Data {
	t.Helper()
	data, err := Build("..")
	if err != nil {
		t.Fatalf("building the index: %v", err)
	}
	return data
}

func TestEveryDatedReportOnDiskIsInTheIndex(t *testing.T) {
	var found []string
	benches, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, bench := range benches {
		if !bench.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join("..", bench.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if !file.IsDir() && onDisk.MatchString(file.Name()) {
				found = append(found, "bench/"+bench.Name()+"/"+file.Name())
			}
		}
	}
	listed := map[string]bool{}
	for _, report := range built(t).Reports {
		listed[report.Source] = true
	}
	for _, path := range found {
		if !listed[path] {
			t.Errorf("%s is on disk and not in the index", path)
		}
	}
	if len(listed) != len(found) {
		t.Errorf("the index carries %d reports and the walk found %d", len(listed), len(found))
	}
	t.Logf("%d dated reports on disk, %d in the index", len(found), len(listed))
}

func TestTheCostReportSaysItsOwnWithdrawal(t *testing.T) {
	report := find(t, "bench/cost/report-2026-09-19.md")
	if report.State != StateWithdrawnInPart {
		t.Errorf("state is %q, and the file's own first line says PART OF THIS REPORT IS WITHDRAWN", report.State)
	}
	if report.StateSource != SelfDeclared {
		t.Errorf("the withdrawal is credited to %q rather than %q", report.StateSource, SelfDeclared)
	}
	if strings.Contains(report.Conclusion, "$0.000063") {
		t.Errorf("the index quotes a figure this report withdrew: %q", report.Conclusion)
	}
}

func TestAReportIsWithdrawnFromOutsideItself(t *testing.T) {
	report := find(t, "bench/forkcache/report-2026-09-22.txt")
	body, err := os.ReadFile(filepath.Join("..", "forkcache", "report-2026-09-22.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "WITHDRAWN") {
		t.Fatal("this file declares its own withdrawal, so it no longer tests the outside case")
	}
	if report.State != StateWithdrawnInPart {
		t.Errorf("state is %q and %s withdraws part of it", report.State, WithdrawalsPath)
	}
	if report.StateSource != WithdrawalsPath {
		t.Errorf("the withdrawal is credited to %q rather than %q", report.StateSource, WithdrawalsPath)
	}
	if !strings.Contains(report.StateNote, "BOARD.md") {
		t.Errorf("the note does not say who withdrew it: %q", report.StateNote)
	}
}

func TestEveryBenchWithNoDatedReportIsNamedAndClassified(t *testing.T) {
	data := built(t)
	named := map[string]PackageKind{}
	for _, pkg := range data.NoReport {
		named[pkg.Package] = pkg.Kind
	}
	for _, want := range []string{"cmd", "corpus", "prompts", "report", "stat", "tui"} {
		if _, ok := named[want]; !ok {
			t.Errorf("bench/%s has no dated report and is not named in the index", want)
		}
	}
	for _, pkg := range data.NoReport {
		if pkg.Kind == "" || pkg.Note == "" {
			t.Errorf("bench/%s is named with no kind and no reason", pkg.Package)
		}
	}
	t.Logf("%d benches with no dated report, by kind: %v", len(named), named)
}

func TestAConclusionThatCannotBeParsedIsListedAsUnparsed(t *testing.T) {
	data := built(t)
	var unparsed []string
	for _, report := range data.Reports {
		if report.Unparsed {
			unparsed = append(unparsed, report.Source)
		}
	}
	if len(unparsed) != data.Counts.Unparsed {
		t.Errorf("%d reports are listed unparsed and the count says %d", len(unparsed), data.Counts.Unparsed)
	}
	if len(unparsed) == 0 {
		t.Fatal("no report is unparsed, so nothing proves an unguessed conclusion is reported as one")
	}
	t.Logf("%d of %d reports name no conclusion a reader can find: %s", len(unparsed), len(data.Reports), strings.Join(unparsed, ", "))
}

func TestEveryReportOpensWithAFigureTakenFromTheReportItself(t *testing.T) {
	for _, report := range built(t).Reports {
		body, err := os.ReadFile(filepath.Join("..", "..", report.Source))
		if err != nil {
			t.Fatal(err)
		}
		if report.Figure == "" || report.Sample == "" {
			t.Errorf("%s opens with figure %q and sample %q", report.Source, report.Figure, report.Sample)
		}
		if !strings.Contains(plain(string(body)), report.Figure) {
			t.Errorf("%s: the headline %q is not in the report", report.Source, report.Figure)
		}
	}
}

func TestABenchWithSeveralDatedRunsKeepsThemNewestFirst(t *testing.T) {
	var runs []string
	for _, report := range built(t).Reports {
		if report.Package == "stopcheck" {
			runs = append(runs, report.Date+" "+report.Figure+" ("+string(report.State)+")")
		}
	}
	if len(runs) != 3 {
		t.Fatalf("stopcheck has 3 dated runs and the index carries %d", len(runs))
	}
	for i := 1; i < len(runs); i++ {
		if runs[i-1] < runs[i] {
			t.Errorf("the runs are not newest first: %v", runs)
		}
	}
	t.Logf("stopcheck as a series: %s", strings.Join(runs, " | "))
}

func find(t *testing.T, path string) Report {
	t.Helper()
	for _, report := range built(t).Reports {
		if report.Source == path {
			return report
		}
	}
	t.Fatalf("%s is not in the index", path)
	return Report{}
}

func TestTheFilesOnDiskAreWhatTheGeneratorWouldWriteNow(t *testing.T) {
	data := built(t)
	machine, err := data.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{
		ViewerFile: []byte(Page(data)),
		DataFile:   machine,
		"INDEX.md": []byte(data.Markdown()),
	} {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%v: run %s", err, RegenerateWith)
		}
		if string(got) != string(want) {
			t.Errorf("bench/report/%s is not what the code writes today, %d bytes against %d: run %s", name, len(got), len(want), RegenerateWith)
		}
	}
}

func TestTheMachineFormCarriesEveryJudgmentTheViewerShows(t *testing.T) {
	machine, err := os.ReadFile(DataFile)
	if err != nil {
		t.Fatal(err)
	}
	var fromDisk Data
	if err := json.Unmarshal(machine, &fromDisk); err != nil {
		t.Fatal(err)
	}
	page := Page(built(t))
	if len(fromDisk.Judgments) == 0 {
		t.Fatalf("%s carries no judgment row", DataFile)
	}
	for _, row := range fromDisk.Judgments {
		if !strings.Contains(page, row.Point) {
			t.Errorf("%s carries %s and the page does not show it", DataFile, row.Point)
		}
	}
}

func TestTheHumanIndexCarriesTheSameRowsAsTheMachineOne(t *testing.T) {
	doc, err := os.ReadFile("INDEX.md")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	inReports := false
	for _, line := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(line, "## Every dated report") {
			inReports = true
		}
		if !inReports || !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "|---") || strings.HasPrefix(line, "| Bench ") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), " | ")
		for i, cell := range cells {
			cells[i] = strings.TrimSpace(strings.ReplaceAll(cell, "\\|", "|"))
		}
		rows = append(rows, cells)
	}
	data := built(t)
	if len(rows) != len(data.Reports) {
		t.Fatalf("INDEX.md carries %d rows and the machine form carries %d", len(rows), len(data.Reports))
	}
	for i, report := range data.Reports {
		want := []string{report.Package, report.Date, "`" + report.Source + "`", report.Conclusion, report.Sample, string(report.State)}
		for j, cell := range want {
			if cell == "" {
				cell = "not stated"
			}
			if rows[i][j] != cell {
				t.Errorf("row %d column %d: INDEX.md says %q and the data says %q", i, j, rows[i][j], cell)
			}
		}
	}
}

func TestTheViewerFetchesNothingWhenItOpens(t *testing.T) {
	body, err := os.ReadFile(ViewerFile)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	for _, refused := range []string{"http://", "https://", "<link", "@import", "fetch(", "XMLHttpRequest", "import(", "src="} {
		if strings.Contains(page, refused) {
			t.Errorf("%s reaches for %q when it opens", ViewerFile, refused)
		}
	}
	for _, copied := range []string{"oklch(0.5770 0.2450 27.3250)", "--radius-xl", ".table-container", ".tab-trigger", ".card-header", ".badge"} {
		if !strings.Contains(page, copied) {
			t.Errorf("%s does not carry the shadcn component %q that was copied into it", ViewerFile, copied)
		}
	}
}
