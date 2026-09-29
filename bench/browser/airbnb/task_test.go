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
		{"testdata/norating", 11, []int{11}},
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
