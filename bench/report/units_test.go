package report

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
)

var (
	datedReportName = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}\.md$`)
	dollarFigure    = regexp.MustCompile(`\$[0-9]`)
)

func TestEveryDatedReportCarriesTheUnitOfItsCostFigures(t *testing.T) {
	awaiting := map[string]string{
		"turn/report-2026-09-18.md":    "money, from usage.cost on an OpenRouter key; BOJI-064 could not write the line, bench/turn is outside its owns",
		"wording/report-2026-09-18.md": "money, from usage.cost on an OpenRouter key; BOJI-064 could not write the line, bench/wording is outside its owns",
		"cost/rescore-2026-09-19.md":   "money, the same unit its sibling cost/report-2026-09-19.md declares and reads from the same ledger; BOJI-098 could not write the line, bench/cost is outside its owns",
		"cost/sweep-2026-09-19.md":     "money, the same unit its sibling cost/report-2026-09-19.md declares and reads from the same ledger; BOJI-098 could not write the line, bench/cost is outside its owns",
	}
	declared, pending, noFigures := 0, 0, 0
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !datedReportName.MatchString(entry.Name()) {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(strings.TrimPrefix(path, ".."+string(filepath.Separator)))
		if !dollarFigure.Match(body) {
			noFigures++
			t.Logf("%s: prints no cost figure", name)
			return nil
		}
		if line := unitLine(string(body)); line != "" {
			declared++
			t.Logf("%s: %s", name, line)
			return nil
		}
		reason, known := awaiting[name]
		if !known {
			t.Errorf("%s prints a cost figure, declares no %sline, and is not named as awaiting one", name, CostUnitPrefix)
			return nil
		}
		pending++
		delete(awaiting, name)
		t.Logf("%s: no unit line yet. Determined from the file: %s", name, reason)
		return nil
	})
	if err != nil {
		t.Fatalf("walking bench: %v", err)
	}
	for name := range awaiting {
		t.Errorf("%s is named as awaiting a unit line but was not found under bench/", name)
	}
	t.Logf("dated reports with cost figures: %d declaring their unit, %d awaiting the line, 0 undetermined. %d dated reports print no cost figure.",
		declared, pending, noFigures)
}

func unitLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, CostUnitPrefix) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func TestCostUnitLineNamesEveryUnitPresentOnce(t *testing.T) {
	line := CostUnitLine([]ledger.Unit{ledger.UnitMoney, ledger.UnitUnpriced, ledger.UnitMoney})
	if !strings.HasPrefix(line, "Cost unit: money, unpriced.") {
		t.Fatalf("got %q", line)
	}
	if !strings.Contains(line, ledger.UnitsDoNotAdd) {
		t.Fatal("a report in two units must say that they do not add")
	}
}

func TestCostUnitLineOnASingleUnitDoesNotLectureAboutAdding(t *testing.T) {
	line := CostUnitLine([]ledger.Unit{ledger.UnitMoney})
	if !strings.HasPrefix(line, "Cost unit: money.") {
		t.Fatalf("got %q", line)
	}
	if strings.Contains(line, ledger.UnitsDoNotAdd) {
		t.Fatalf("nothing to keep apart in %q", line)
	}
}
