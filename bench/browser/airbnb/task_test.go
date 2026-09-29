package airbnb

import (
	"slices"
	"strings"
	"testing"
)

func TestTheRecordedRunsScoreTwelveAndTheRightLowerCount(t *testing.T) {
	task, err := LoadTask("task.json")
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

func TestTheReportRefusesRowsFromTwoCredentialKinds(t *testing.T) {
	task, err := LoadTask("task.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []Row
	for _, dir := range []string{"testdata/pass", "testdata/fail"} {
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
	if !strings.Contains(table, "| steps | 12 of 12 |") || !strings.Contains(table, "| goal | 5 of 12 |") {
		t.Errorf("the table does not carry both rows")
	}
	rows[1].Conditions.Credential = "key"
	if _, err := Render(rows); err == nil {
		t.Error("a subscription row and a key row were printed side by side")
	}
}
