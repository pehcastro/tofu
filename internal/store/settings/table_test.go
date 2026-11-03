package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScratchRootsTakesAListWithAnAbsoluteEntryAndRefusesTheRest(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, FileName)
	store, err := Open(global, filepath.Join(dir, "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "disk")
	list := string(os.PathListSeparator)

	for _, refused := range []string{"here", "a" + list + "b", "." + list + ".."} {
		err := store.SetText(Global, "scratchRoots", refused)
		if err == nil {
			t.Errorf("scratchRoots %q has no absolute entry and was accepted", refused)
			continue
		}
		if !strings.Contains(err.Error(), refused) || !strings.Contains(err.Error(), "absolute") {
			t.Errorf("refusing scratchRoots %q said %q, which does not name the value and the absolute entry it lacks", refused, err)
		}
	}
	if err := store.SetText(Project, "scratchRoots", disk); err == nil || !strings.Contains(err.Error(), "global") {
		t.Errorf("a project scratchRoots, which nothing reads, was not refused naming the global scope: %v", err)
	}
	if written, _ := os.ReadFile(global); strings.Contains(string(written), "scratchRoots") {
		t.Errorf("a refused scratchRoots reached the file:\n%s", written)
	}

	for _, accepted := range []string{disk, "relative" + list + disk, ""} {
		if err := store.SetText(Global, "scratchRoots", accepted); err != nil {
			t.Errorf("scratchRoots %q holds an absolute entry or clears it and was refused: %v", accepted, err)
			continue
		}
		if got := store.Text("scratchRoots"); got != accepted {
			t.Errorf("scratchRoots reads %q after a set of %q", got, accepted)
		}
		if scope, fromFile := store.Source("scratchRoots"); scope != Global || !fromFile {
			t.Errorf("scratchRoots %q comes from scope %v, file %v, want the global file", accepted, scope, fromFile)
		}
	}
}
