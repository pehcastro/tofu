package report

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/internal/platform/fs"
)

var onDisk = regexp.MustCompile(`^report-\d{4}-\d{2}-\d{2}.*\.(md|txt)$`)

const maxViewerBytes = 400 * 1024

func benchTree() string {
	return filepath.Join(fs.SourceRoot(), "bench")
}

func built(t *testing.T) Data {
	t.Helper()
	needReports(t)
	data, err := Build(benchTree())
	if err != nil {
		t.Fatalf("building the index: %v", err)
	}
	return data
}

func TestEveryDatedReportOnDiskIsInTheIndex(t *testing.T) {
	var found []string
	root := benchTree()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !onDisk.MatchString(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found = append(found, "bench/"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
	report := find(t, "bench/suites/judge/cost/report-2026-09-19.md")
	if report.State != StateWithdrawnInPart {
		t.Errorf("state is %q, and the file's own first line says PART OF THIS REPORT IS WITHDRAWN", report.State)
	}
	if report.StateSource != SelfDeclared {
		t.Errorf("the withdrawal is credited to %q rather than %q", report.StateSource, SelfDeclared)
	}
	if strings.Contains(report.Quoted(), "$0.000063") {
		t.Errorf("the index quotes a figure this report withdrew: %q", report.Quoted())
	}
}

func TestAReportIsWithdrawnFromOutsideItself(t *testing.T) {
	report := find(t, "bench/suites/tools/websift/report-2026-09-21.md")
	body, err := os.ReadFile(filepath.Join(benchTree(), "suites", "tools", "websift", "report-2026-09-21.md"))
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
	if !strings.Contains(report.StateNote, "TOFU-440") {
		t.Errorf("the note does not say who withdrew it: %q", report.StateNote)
	}
}

func TestEveryBenchWithNoDatedReportIsNamedAndClassified(t *testing.T) {
	data := built(t)
	named := map[string]PackageKind{}
	for _, pkg := range data.NoReport {
		named[pkg.Package] = pkg.Kind
	}
	for _, want := range []string{"cmd", "kit/corpus", reportDir, "suites/agent/phase"} {
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
	want := []string{"bench/suites/judge/promote/report-2026-09-21.md", "bench/suites/agent/turn/report-2026-09-18.md"}
	if strings.Join(unparsed, ",") != strings.Join(want, ",") {
		t.Errorf("the reports naming no conclusion are %v, want %v", unparsed, want)
	}
	for _, path := range want {
		if got := find(t, path).Quoted(); got != Unparsed {
			t.Errorf("%s: this report genuinely names no answer and must keep saying so, got %q", path, got)
		}
	}
	t.Logf("%d of %d reports name no conclusion a reader can find: %s", len(unparsed), len(data.Reports), strings.Join(unparsed, ", "))
}

func TestAnUnparsedHeadingIsFilledFromTheHandReadEntryInstead(t *testing.T) {
	report := find(t, "bench/suites/judge/calibration/report-2026-09-23.md")
	if report.Unparsed {
		t.Fatal("this report has a handread.json entry and must stop reading unparsed")
	}
	if !strings.HasPrefix(report.Quoted(), "Hand read from "+HandReadPath+": ") {
		t.Errorf("the quoted conclusion does not say it was hand read: %q", report.Quoted())
	}
	if !strings.Contains(report.Quoted(), "hand labelling cannot close the gap") {
		t.Errorf("the quoted conclusion does not carry the hand-read sentence: %q", report.Quoted())
	}
}

func TestEveryReportOpensWithAFigureTakenFromTheReportItself(t *testing.T) {
	for _, report := range built(t).Reports {
		body, err := os.ReadFile(filepath.Join(fs.SourceRoot(), report.Source))
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

func TestANestedBenchIsItsOwnBenchInTheIndex(t *testing.T) {
	data := built(t)
	found := map[string]string{}
	for _, report := range data.Reports {
		if strings.HasPrefix(report.Source, "bench/suites/judge/ask/") {
			found[report.Source] = report.Package
		}
	}
	want := map[string]string{
		"bench/suites/judge/ask/report-2026-09-21.md":        "suites/judge/ask",
		"bench/suites/judge/ask/server/report-2026-09-23.md": "suites/judge/ask/server",
		"bench/suites/judge/ask/server/report-2026-09-24.md": "suites/judge/ask/server",
	}
	for path, pkg := range want {
		if found[path] != pkg {
			t.Errorf("%s: index carries package %q, want %q", path, found[path], pkg)
		}
	}
	if len(found) != len(want) {
		t.Errorf("bench/suites/judge/ask/* carries %d reports in the index, want %d: %v", len(found), len(want), found)
	}
}

func TestAWithdrawalNamingANestedReportIsAccepted(t *testing.T) {
	fixture := "fixture_withdrawn_nested.json"
	body := `[{"report":"bench/suites/judge/ask/server/report-2026-09-23.md","state":"stale","why":"proving TOFU-568's fix accepts a withdrawal naming a nested report"}]`
	if err := os.WriteFile(fixture, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(fixture) })
	withdrawals, err := readWithdrawals(fixture)
	if err != nil {
		t.Fatalf("a withdrawal naming a nested report was refused: %v", err)
	}
	if _, named := withdrawals["bench/suites/judge/ask/server/report-2026-09-23.md"]; !named {
		t.Fatal("the nested report did not come back out of readWithdrawals")
	}
}

func TestABenchWithSeveralDatedRunsKeepsThemNewestFirst(t *testing.T) {
	var runs []string
	for _, report := range built(t).Reports {
		if report.Package == "suites/judge/stopcheck" {
			runs = append(runs, report.Date+" "+report.Figure+" ("+string(report.State)+")")
		}
	}
	if len(runs) != 4 {
		t.Fatalf("stopcheck has 4 dated runs, the fourth bench/suites/judge/stopcheck/report-2026-10-24.md, and the index carries %d", len(runs))
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

func TestTheViewerFetchesNothingWhenItOpens(t *testing.T) {
	page := Page(built(t))
	for _, refused := range []string{"http://", "https://", "<link", "@import", "fetch(", "XMLHttpRequest", "import(", "src="} {
		if strings.Contains(page, refused) {
			t.Errorf("%s reaches for %q when it opens", ViewerFile, refused)
		}
	}
	for _, link := range regexp.MustCompile(`url\(([^)]*)\)`).FindAllStringSubmatch(page, -1) {
		if !strings.HasPrefix(link[1], "data:") {
			t.Errorf("%s loads %q, and only a data: URL is carried inside the page", ViewerFile, link[1][:min(60, len(link[1]))])
		}
	}
	if len(page) > maxViewerBytes {
		t.Errorf("%s is %d bytes, over the %d the embedded font leaves room for", ViewerFile, len(page), maxViewerBytes)
	}
	if !strings.Contains(page, "src:url(data:font/woff2;base64,") {
		t.Errorf("%s does not carry Inter inside it, so it renders in whatever the machine has", ViewerFile)
	}
	for _, specified := range []string{"--font:Inter, system-ui", "font-feature-settings:\"tnum\"", "max-width:960px", "--bg:#fafafa", "prefers-color-scheme: dark", "--ahead-text:#15803d", "--behind-bg:#fee2e2"} {
		if !strings.Contains(page, specified) {
			t.Errorf("%s does not carry %q from the design TOFU-1330 specifies", ViewerFile, specified)
		}
	}
}
