package airbnb

import (
	"slices"
	"strings"
	"testing"
)

func TestTheRecordedRunsScoreTwelveAndTheRightLowerCount(t *testing.T) {
	task, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range []struct {
		dir    string
		passed int
		failed []int
	}{
		{"testdata/pass", 12, nil},
		{"testdata/fail", 5, []int{1, 3, 5, 8, 9, 10, 12}},
		{"testdata/first", 11, []int{12}},
		{"testdata/norating", 10, []int{6, 11}},
		{"testdata/outside", 11, []int{2}},
		{"testdata/table", 12, nil},
		{"testdata/skipped", 12, nil},
		{"testdata/wording", 12, nil},
		{"testdata/poolheader", 11, []int{11}},
	} {
		run, err := LoadRun(recorded.dir)
		if err != nil {
			t.Fatal(err)
		}
		row := Score(task, run)
		var failed []int
		for _, result := range row.Steps {
			if !result.Passed {
				failed = append(failed, result.Step)
			}
		}
		if row.Passed != recorded.passed || !slices.Equal(failed, recorded.failed) {
			t.Errorf("%s: %d of %d passed, failing %v, want %d failing %v", recorded.dir, row.Passed, len(task.Steps), failed, recorded.passed, recorded.failed)
		}
		t.Logf("%s: %d of %d", recorded.dir, row.Passed, len(task.Steps))
	}
}

func TestTheTableAddsMainAndBrowserTokensAndSortsByStepsThenTotal(t *testing.T) {
	browser, heavier := 900, 5000
	table, err := Render([]Row{
		{Arm: ArmC, Passed: 9, Steps: make([]Result, 12), MainTokens: 7000},
		{Arm: ArmB2, Passed: 12, Steps: make([]Result, 12), MainTokens: 100, BrowserTokens: &heavier},
		{Arm: ArmB3, Passed: 12, Steps: make([]Result, 12), MainTokens: 100, BrowserTokens: &browser},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table)
	b3, b2, c := strings.Index(table, "| B3 |"), strings.Index(table, "| B2 |"), strings.Index(table, "| C |")
	if !strings.Contains(table, "| B3 | as set | 12 of 12 | 0 s | 1000 | - |") || !(b3 < b2 && b2 < c) {
		t.Errorf("want B3 with total 1000 first, then B2, then C")
	}
	if !strings.Contains(table, "| C | as set | 9 of 12 | 0 s | not recorded | - |") {
		t.Errorf("a row with no browser tokens printed a total")
	}
}

func TestEachArmIsStampedWithTheCredentialOfTheModelThatRanItsBrowserTurns(t *testing.T) {
	for arm, want := range map[Arm]string{ArmA: "subscription", ArmB1: "key", ArmB2: "subscription", ArmC: "subscription"} {
		run := Run{Conditions: Conditions{BrowserModel: map[Arm]string{ArmB1: "meta/muse-spark-1.3-contributor", ArmB2: "claude-sub/claude-sonnet-5"}[arm]}}
		if err := arm.Stamp(&run, "2026-09-29", "fixture-machine"); err != nil {
			t.Fatal(err)
		}
		if run.Conditions.Credential != "subscription" || run.Conditions.BrowserCredential != want {
			t.Errorf("arm %s is stamped main %q and browser %q, want subscription and %s", arm, run.Conditions.Credential, run.Conditions.BrowserCredential, want)
		}
	}
}

func TestTheReportComparesBrowserCredentialsAndRefusesTwoMainOnes(t *testing.T) {
	task, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var rows []Row
	for _, dir := range []string{"testdata/pass", "testdata/fail", "testdata/first"} {
		run, err := LoadRun(dir)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, Score(task, run))
	}
	table, err := Render(rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table)
	for _, want := range []string{"browser credential |", "| A | as set | 12 of 12 |", "| C | as set | 5 of 12 |", "| B1 | as set | 11 of 12 |", "| subscription | key |"} {
		if !strings.Contains(table, want) {
			t.Errorf("the table lacks %q", want)
		}
	}
	rows[1].Conditions.Credential = "key"
	if _, err := Render(rows); err == nil {
		t.Error("two main models on different credential kinds were printed side by side")
	}
}
