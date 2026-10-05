package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASetFromTheAppKeepsWhatWasWrittenOnDiskSinceItOpened(t *testing.T) {
	dir := t.TempDir()
	global, project := filepath.Join(dir, "global.json"), filepath.Join(dir, "project.json")
	if err := os.WriteFile(project, []byte(`{"diffContext":5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(global, project)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(global, []byte(`{"chatShowsTools":1,"theme":"light","hyperlinks":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(Global, Hyperlinks, 1); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(global)
	for _, kept := range []string{`"chatShowsTools": 1`, `"theme": "light"`, `"hyperlinks": 1`} {
		if !strings.Contains(string(written), kept) {
			t.Errorf("the global file lost %s after a Set from the app:\n%s", kept, written)
		}
	}
	if untouched, _ := os.ReadFile(project); string(untouched) != `{"diffContext":5}` {
		t.Errorf("a Set on the global scope rewrote the project file:\n%s", untouched)
	}

	if err := os.Remove(global); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(Global, ChatShowsTools, 0); err != nil {
		t.Fatalf("a Set after the file was deleted failed: %v", err)
	}
	if written, _ := os.ReadFile(global); strings.Contains(string(written), Theme) {
		t.Errorf("a Set after the file was deleted brought back a value from memory:\n%s", written)
	}

	broken := []byte(`{"chatShowsTools":`)
	if err := os.WriteFile(global, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(Global, Hyperlinks, 0); err == nil {
		t.Error("a Set over a broken file succeeded")
	}
	if kept, _ := os.ReadFile(global); string(kept) != string(broken) {
		t.Errorf("a Set over a broken file rewrote it:\n%s", kept)
	}

	if err := os.WriteFile(global, []byte(`{"hyperlinks":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"diffContext":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reread(); err != nil {
		t.Fatal(err)
	}
	if store.Int(DiffContext) != 9 || !store.Bool(Hyperlinks) {
		t.Errorf("Reread shows diffContext %d and hyperlinks %v, want 9 and on", store.Int(DiffContext), store.Bool(Hyperlinks))
	}
}

func TestARetiredCompactionKeyLoadsIsIgnoredAndSurvivesASet(t *testing.T) {
	global := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(global, []byte(`{"compaction":"off","theme":"Nord"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(global, "")
	if err != nil {
		t.Fatalf("a file holding the retired compaction key did not load: %v", err)
	}
	for _, spec := range store.Table() {
		if spec.Key == "compaction" {
			t.Fatalf("the settings table still declares compaction: %+v", spec)
		}
	}
	if store.Text(Theme) != "Nord" {
		t.Errorf("theme beside the retired key reads %q, want Nord", store.Text(Theme))
	}
	if err := store.SetText(Global, "compaction", "off"); err == nil {
		t.Error("a set of the retired compaction key was accepted")
	}
	if err := store.SetText(Global, Density, "compact"); err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(global); !strings.Contains(string(written), `"compaction": "off"`) {
		t.Errorf("a set on another key dropped the person's retired key from the file:\n%s", written)
	}
}
