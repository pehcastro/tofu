package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const stubCheckerNDJSON = `{"item":1,"status":"pass"}
{"item":2,"status":"fail","reason":"POST /tasks returned 404"}
{"item":11,"status":"recorded","reason":"whitespace title returned 400"}
{"item":16,"status":"could_not_evaluate","reason":"none of the cases returned 400 or 404"}
{"item":"shape:id","status":"recorded","reason":"id looks like a counter"}
`

const stubCheckerScript = `const lines = ` + "`" + stubCheckerNDJSON + "`" + `.trim().split("\n")
for (const line of lines) console.log(line)
`

func writeStubChecker(t *testing.T) (bunBin, scriptPath string) {
	t.Helper()
	bunBin, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not on PATH, skipping checklist subprocess test")
	}
	dir := t.TempDir()
	scriptPath = filepath.Join(dir, "stub.ts")
	if err := os.WriteFile(scriptPath, []byte(stubCheckerScript), 0o644); err != nil {
		t.Fatalf("write stub checker: %v", err)
	}
	return bunBin, scriptPath
}

func TestRunChecklist_ParsesRealSubprocessOutput(t *testing.T) {
	bunBin, scriptPath := writeStubChecker(t)
	checks, err := RunChecklist(bunBin, scriptPath, t.TempDir())
	if err != nil {
		t.Fatalf("RunChecklist: %v", err)
	}
	want := []ChecklistCheck{
		{Item: 1, Status: ChecklistCheckPassed},
		{Item: 2, Status: ChecklistCheckFailed, Reason: "POST /tasks returned 404"},
		{Item: 11, Status: ChecklistCheckRecorded, Reason: "whitespace title returned 400"},
		{Item: 16, Status: ChecklistCheckCouldNotEvaluate, Reason: "none of the cases returned 400 or 404"},
	}
	if !reflect.DeepEqual(checks, want) {
		t.Fatalf("got %+v, want %+v", checks, want)
	}
}

func TestParseChecklistOutput_SkipsNonNumericItems(t *testing.T) {
	checks, err := parseChecklistOutput([]byte(stubCheckerNDJSON))
	if err != nil {
		t.Fatalf("parseChecklistOutput: %v", err)
	}
	if len(checks) != 4 {
		t.Fatalf("got %d checks, want 4 (the shape:id line must be skipped)", len(checks))
	}
}

func TestParseChecklistOutput_SkipsBlankLines(t *testing.T) {
	checks, err := parseChecklistOutput([]byte("\n" + `{"item":1,"status":"pass"}` + "\n\n"))
	if err != nil {
		t.Fatalf("parseChecklistOutput: %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1", len(checks))
	}
}

func TestParseChecklistOutput_MalformedLineIsAnError(t *testing.T) {
	_, err := parseChecklistOutput([]byte("not json\n"))
	if err == nil {
		t.Fatal("expected an error for a malformed line, got nil")
	}
}

func TestChecklistCheck_ToResult(t *testing.T) {
	cases := []struct {
		status ChecklistCheckStatus
		passed bool
	}{
		{ChecklistCheckPassed, true},
		{ChecklistCheckFailed, false},
		{ChecklistCheckCouldNotEvaluate, false},
		{ChecklistCheckRecorded, false},
	}
	for _, c := range cases {
		check := ChecklistCheck{Item: 9, Status: c.status, Reason: "why"}
		result := check.ToResult()
		if result.Item != "item 9" {
			t.Errorf("status %q: got item %q, want %q", c.status, result.Item, "item 9")
		}
		if result.Passed != c.passed {
			t.Errorf("status %q: got passed %v, want %v", c.status, result.Passed, c.passed)
		}
	}
}

func TestChecklistResults_TurnsChecksIntoRowChecklist(t *testing.T) {
	checks := []ChecklistCheck{
		{Item: 1, Status: ChecklistCheckPassed},
		{Item: 2, Status: ChecklistCheckFailed, Reason: "boom"},
		{Item: 11, Status: ChecklistCheckRecorded, Reason: "recorded"},
	}
	want := []ChecklistResult{
		{Item: "item 1", Passed: true},
		{Item: "item 2", Passed: false},
		{Item: "item 11", Passed: false},
	}
	got := ChecklistResults(checks)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	row := Row{Checklist: got}
	if len(row.Checklist) != 3 {
		t.Fatalf("row did not carry all three checklist results: %+v", row.Checklist)
	}
}

func TestRunChecklist_MissingBinaryIsAnError(t *testing.T) {
	_, err := RunChecklist("tofu-bench-checklist-missing-binary-xyz", "checker.ts", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a missing bun binary, got nil")
	}
}
