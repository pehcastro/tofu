package settings

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global", FileName)
	projectPath := filepath.Join(dir, "project", FileName)
	store, err := Open(globalPath, projectPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store, globalPath, projectPath
}

func TestASettingChangedInTheViewIsOnDiskBeforeTheNextKeystroke(t *testing.T) {
	store, globalPath, _ := openTemp(t)
	if err := store.Set(Global, ChatShowsTools, 1); err != nil {
		t.Fatalf("Set: %v", err)
	}
	reopened, err := Open(globalPath, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !reopened.Bool(ChatShowsTools) {
		t.Fatal("the value written by Set was not on disk when reopened")
	}
}

func TestASettingSurvivesARestart(t *testing.T) {
	store, globalPath, projectPath := openTemp(t)
	if err := store.Set(Project, DecisionCap, 7); err != nil {
		t.Fatalf("Set: %v", err)
	}
	reopened, err := Open(globalPath, projectPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Int(DecisionCap); got != 7 {
		t.Fatalf("decisionCap after restart = %d, want 7", got)
	}
}

func TestAProjectValueOverridesAGlobalOne(t *testing.T) {
	store, _, _ := openTemp(t)
	if err := store.Set(Global, DecisionCap, 3); err != nil {
		t.Fatalf("Set global: %v", err)
	}
	if err := store.Set(Project, DecisionCap, 9); err != nil {
		t.Fatalf("Set project: %v", err)
	}
	if got := store.Int(DecisionCap); got != 9 {
		t.Fatalf("effective decisionCap = %d, want the project value 9", got)
	}
	scope, fromFile := store.Source(DecisionCap)
	if scope != Project || !fromFile {
		t.Fatalf("Source = %v %v, want Project true", scope, fromFile)
	}
}

func TestTheDecisionCapDefaultsToNoCap(t *testing.T) {
	store, _, _ := openTemp(t)
	if got := store.Int(DecisionCap); got != 0 {
		t.Fatalf("decisionCap default = %d, want 0, meaning no cap", got)
	}
}

func TestTheReadBeforeEditSettingDefaultsToOn(t *testing.T) {
	store, _, _ := openTemp(t)
	if !store.Bool(ReadBeforeEdit) {
		t.Fatal("readBeforeEdit default is off, want on: TOFU-510 measured the cost of on at 1.8 percent of edits")
	}
}

func TestARestartRequiredSettingIsPendingOnlyAfterItChanges(t *testing.T) {
	store, _, _ := openTemp(t)
	if pending := store.RestartPending(); len(pending) != 0 {
		t.Fatalf("RestartPending before any change = %v, want none", pending)
	}
	if err := store.Set(Global, DecisionCap, 4); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pending := store.RestartPending()
	if len(pending) != 1 || pending[0] != DecisionCap {
		t.Fatalf("RestartPending after changing decisionCap = %v, want [decisionCap]", pending)
	}
	if pending := (&Store{}).RestartPending(); pending != nil {
		t.Fatalf("a store with no snapshot must report nothing pending, got %v", pending)
	}
}

func TestChatShowsToolsNeedsNoRestart(t *testing.T) {
	store, _, _ := openTemp(t)
	if err := store.Set(Global, ChatShowsTools, 1); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if pending := store.RestartPending(); len(pending) != 0 {
		t.Fatalf("RestartPending after toggling chatShowsTools = %v, want none", pending)
	}
}

func TestSetRejectsAnUndeclaredKey(t *testing.T) {
	store, _, _ := openTemp(t)
	if err := store.Set(Global, "notARealSetting", 1); err == nil {
		t.Fatal("Set accepted a key that is not in the table")
	}
}

func TestANewSettingIsOneRowInTheTable(t *testing.T) {
	table := append(Default(), Spec{Key: "wallClockBudget", Label: "wall clock budget in minutes", Group: "turn", Kind: Int, Default: 30, Restart: false})
	dir := t.TempDir()
	store, err := OpenWith(table, filepath.Join(dir, "global", FileName), filepath.Join(dir, "project", FileName))
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	if got := store.Int("wallClockBudget"); got != 30 {
		t.Fatalf("new setting default = %d, want 30", got)
	}
	if err := store.Set(Global, "wallClockBudget", 45); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := store.Int("wallClockBudget"); got != 45 {
		t.Fatalf("new setting after Set = %d, want 45", got)
	}
}
