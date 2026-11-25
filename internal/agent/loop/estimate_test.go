package loop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettledRunsSkipATornLastLineAndReadNewestFirst(t *testing.T) {
	state := t.TempDir()
	written := `{"project":"here","model":"m","chars":10,"took_ms":1000}
{"project":"there","model":"m","chars":20,"took_ms":2000}
{"model":"m","chars":30,"took_ms":3000,"test_ms":500,"test_calls":2}
{"project":"here","model":"m","chars":40,"took_`
	if err := os.WriteFile(filepath.Join(state, settledRunsName), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	here := func(project string) bool { return project == "" || project == "here" }
	runs, _, err := settledRuns(state, here)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].TookMS != 3000 || runs[0].TestMS != 500 || runs[0].CheckCalls != 2 || runs[1].TookMS != 1000 {
		t.Fatalf("want the split run then the first one, got %+v", runs)
	}
	elsewhere, _, err := settledRuns(state, func(project string) bool { return !here(project) })
	if err != nil {
		t.Fatal(err)
	}
	if len(elsewhere) != 1 || elsewhere[0].TookMS != 2000 {
		t.Fatalf("want only the run from the other project, got %+v", elsewhere)
	}
}
