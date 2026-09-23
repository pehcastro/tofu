package search

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tool "tofu/internal/search"
)

const costRuns = 5

type costRun struct {
	read    time.Duration
	total   time.Duration
	byForm  map[tool.Candidate]time.Duration
	notRun  map[tool.Candidate]string
	files   int
	answers tool.Candidate
}

func measure(t *testing.T, pattern string) []costRun {
	t.Helper()
	files, err := TrackedFiles(treeRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	compiled := regexp.MustCompile(pattern)
	runs := make([]costRun, 0, costRuns)
	for range costRuns {
		started := time.Now()
		result, err := tool.Find(tool.Request{Root: treeRoot, Files: files, Pattern: compiled})
		if err != nil {
			t.Fatal(err)
		}
		run := costRun{
			read:    result.Stats.Read,
			total:   time.Since(started),
			byForm:  map[tool.Candidate]time.Duration{},
			notRun:  map[tool.Candidate]string{},
			files:   len(files),
			answers: result.Answered,
		}
		for _, attempt := range result.Tried {
			run.byForm[attempt.Candidate] = attempt.Spent
			if attempt.NotRun != "" {
				run.notRun[attempt.Candidate] = attempt.NotRun
			}
		}
		runs = append(runs, run)
	}
	return runs
}

func median(runs []costRun, of func(costRun) time.Duration) time.Duration {
	spent := make([]time.Duration, 0, len(runs))
	for _, run := range runs {
		spent = append(spent, of(run))
	}
	slices.Sort(spent)
	return spent[len(spent)/2]
}

func wholeTreePatterns(t *testing.T, want int) []string {
	t.Helper()
	seen := map[string]bool{}
	var patterns []string
	for _, row := range loadOrSkip(t).Rows {
		if row.Path != "" || seen[row.Pattern] {
			continue
		}
		seen[row.Pattern] = true
		patterns = append(patterns, row.Pattern)
		if len(patterns) == want {
			break
		}
	}
	if len(patterns) == 0 {
		t.Skip("skipped: no recorded search ran over the whole tree, so there is nothing to time against one")
	}
	return patterns
}

func TestWhatEachCandidateCostsOverTheWholeTree(t *testing.T) {
	forms := []tool.Candidate{tool.Literal, tool.WordBoundary, tool.CaseInsensitive, tool.GoSymbol}
	plainWord := strings.ToUpper("f") + "allback"
	for _, pattern := range append(wholeTreePatterns(t, 3), plainWord) {
		runs := measure(t, pattern)
		read := median(runs, func(run costRun) time.Duration { return run.read })
		total := median(runs, func(run costRun) time.Duration { return run.total })
		t.Logf("%q over %d files, %d runs: whole call %v, of which reading the files %v, answered by %s",
			pattern, runs[0].files, len(runs), total, read, runs[0].answers)
		if runs[0].answers != tool.Literal {
			t.Logf("  the literal found nothing, so this one is priced by the absence measurement instead")
			continue
		}
		var extra time.Duration
		for _, form := range forms {
			if why, skipped := runs[0].notRun[form]; skipped {
				t.Logf("  %-16s not run, %s", form, why)
				continue
			}
			spent := median(runs, func(run costRun) time.Duration { return run.byForm[form] })
			t.Logf("  %-16s %v", form, spent)
			if form != tool.Literal {
				extra += spent
			}
		}
		if extra > read {
			t.Fatalf("%q: the candidate forms cost %v against %v spent reading the files, so they are not free", pattern, extra, read)
		}
	}
}

func TestWhatItCostsToTellAWrongPatternFromAnAbsence(t *testing.T) {
	rescued := strings.ToLower("Binary") + strings.ToLower("Skipped")
	absent := "xyzzy" + "plughquux"
	for _, pattern := range []string{rescued, absent} {
		runs := measure(t, pattern)
		if why, skipped := runs[0].notRun[tool.CaseInsensitive]; skipped {
			t.Fatalf("%q found nothing on the literal and the case insensitive form still did not run: %s", pattern, why)
		}
		t.Logf("%q over %d files, %d runs: whole call %v, of which reading the files twice %v, literal %v, case insensitive %v, answered by %s",
			pattern, runs[0].files, len(runs),
			median(runs, func(run costRun) time.Duration { return run.total }),
			median(runs, func(run costRun) time.Duration { return run.read }),
			median(runs, func(run costRun) time.Duration { return run.byForm[tool.Literal] }),
			median(runs, func(run costRun) time.Duration { return run.byForm[tool.CaseInsensitive] }),
			runs[0].answers)
	}
}
