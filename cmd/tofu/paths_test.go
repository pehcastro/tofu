package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func legacyTree(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	for path, body := range map[string]string{
		"sessions/turn-a/header.json":  `{"id":"turn-a"}`,
		"sessions/turn-a/events.jsonl": "{\"kind\":\"step\"}\n",
		"sessions/turn-b/header.json":  `{"id":"turn-b"}`,
		"log/decisions.jsonl":          "{\"point\":\"tool_gate\"}\n",
		"calibration/tool_gate.lock":   "threshold: 0.8\n",
	} {
		full := filepath.Join(parent, sys.LegacyStateDirName, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return parent
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return files
}

func sameTree(t *testing.T, want, got map[string]string, what string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: %d files, want %d\n%v\n%v", what, len(got), len(want), got, want)
	}
	for name, body := range want {
		if got[name] != body {
			t.Fatalf("%s: %s reads %q, want %q", what, name, got[name], body)
		}
	}
}

func TestTheFirstRunCopiesTheOldDirectoryAndLeavesItByteForByte(t *testing.T) {
	parent := legacyTree(t)
	old := filepath.Join(parent, sys.LegacyStateDirName)
	before := snapshot(t, old)

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	sameTree(t, before, snapshot(t, old), "the old directory changed")
	sameTree(t, before, snapshot(t, filepath.Join(parent, sys.StateDirName)), "the copy does not match the old directory")

	said := out.String()
	for _, want := range []string{".tofu", ".boji", "2 sessions", "5 files", "still there"} {
		if !strings.Contains(said, want) {
			t.Errorf("the sentence does not say %q:\n%s", want, said)
		}
	}
	t.Log(said)
}

func TestTheSecondRunCopiesNothingAndSaysNothing(t *testing.T) {
	parent := legacyTree(t)
	var first bytes.Buffer
	copyLegacyStateDir(&first, parent)
	if first.Len() == 0 {
		t.Fatal("the first run said nothing, so there is no second run to test")
	}
	copied := snapshot(t, filepath.Join(parent, sys.StateDirName))

	var second bytes.Buffer
	copyLegacyStateDir(&second, parent)

	if second.Len() != 0 {
		t.Fatalf("the second run printed:\n%s", second.String())
	}
	sameTree(t, copied, snapshot(t, filepath.Join(parent, sys.StateDirName)), "the second run wrote into the new directory")
}

type refusingFS struct {
	fs.FS
	refuse string
}

func (r refusingFS) Open(name string) (fs.File, error) {
	if name == r.refuse {
		return nil, fs.ErrPermission
	}
	return r.FS.Open(name)
}

func TestACopyInterruptedPartWayNeverBecomesTheNewDirectory(t *testing.T) {
	parent := legacyTree(t)
	source := filepath.Join(parent, sys.LegacyStateDirName)
	target := filepath.Join(parent, sys.StateDirName)
	partial := target + partialSuffix

	if _, _, err := copyTreeInto(refusingFS{FS: os.DirFS(source), refuse: "log/decisions.jsonl"}, ".", partial); err == nil {
		t.Fatal("the copy reported success over a source it could not read whole")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("an interrupted copy created %s", target)
	}
	if half := snapshot(t, partial); len(half) != 1 {
		t.Fatalf("the interrupted copy holds %d files, want the one it managed: %v", len(half), half)
	}
	if dir := sys.StateDir(parent); dir != source {
		t.Fatalf("after an interrupted copy the resolver reads %s, want the old directory", dir)
	}

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("the interrupted copy survived at %s", partial)
	}
	sameTree(t, snapshot(t, source), snapshot(t, target), "the run after an interruption did not copy the whole tree")
}

func TestSomethingAlreadyAtTheNewPathIsLeftAlone(t *testing.T) {
	parent := legacyTree(t)
	target := filepath.Join(parent, sys.StateDirName)
	his := "a file of his, not a data directory"
	if err := os.WriteFile(target, []byte(his), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	if out.Len() != 0 {
		t.Fatalf("the run printed:\n%s", out.String())
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != his {
		t.Fatalf("%s was overwritten: %q, %v", target, string(body), err)
	}
}

func TestAHalfCopyLeftBehindIsThrownAwayAndTheNextRunStartsOver(t *testing.T) {
	parent := legacyTree(t)
	partial := filepath.Join(parent, sys.StateDirName+partialSuffix)
	if err := os.MkdirAll(filepath.Join(partial, "sessions", "turn-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "sessions", "turn-a", "header.json"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir := sys.StateDir(parent); dir != filepath.Join(parent, sys.LegacyStateDirName) {
		t.Fatalf("a half copy was read as the data directory: %s", dir)
	}

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("the half copy survived at %s", partial)
	}
	sameTree(t, snapshot(t, filepath.Join(parent, sys.LegacyStateDirName)),
		snapshot(t, filepath.Join(parent, sys.StateDirName)), "the copy that started over is not complete")
}

func TestWithTheNewDirectoryPresentTheOldOneIsNeverRead(t *testing.T) {
	parent := legacyTree(t)
	copyLegacyStateDir(io.Discard, parent)

	old := filepath.Join(parent, sys.LegacyStateDirName)
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("reading this as a directory fails"), 0o000); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	copyLegacyStateDir(&second, parent)
	if second.Len() != 0 {
		t.Fatalf("the old directory was touched:\n%s", second.String())
	}
	if dir := sys.StateDir(parent); dir != filepath.Join(parent, sys.StateDirName) {
		t.Fatalf("the resolver reads %s with the new directory present", dir)
	}
	if _, err := os.ReadDir(old); err == nil {
		t.Fatal("the old path is still readable as a directory, so this test proves nothing")
	}
}

func projectWithState(t *testing.T) (home, project string) {
	t.Helper()
	home, project = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for path, body := range map[string]string{
		"agents/go-dev.md":             "---\nname: go-dev\n---\n",
		"settings.json":                `{"theme":"dark"}`,
		".env":                         "OPENROUTER_KEY=sk-or-v1-thistestwroteit\n",
		"sessions/HEAD":                "turn-a\n",
		"sessions/turn-a/header.json":  `{"id":"turn-a"}`,
		"sessions/turn-a/events.jsonl": "{\"kind\":\"step\"}\n",
		"log/decisions.jsonl":          "{\"point\":\"tool_gate\"}\n",
		"quota/readings.jsonl":         "{\"window\":\"5h\"}\n",
		"promotions.jsonl":             "{\"turn\":\"turn-a\"}\n",
	} {
		full := filepath.Join(project, sys.StateDirName, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home, project
}

func TestTheMoveTakesTheStateAndLeavesWhatThePersonWrote(t *testing.T) {
	home, project := projectWithState(t)
	config := filepath.Join(project, sys.StateDirName)
	before := snapshot(t, config)
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if moved, failed := moveProjectState(&out, project); moved != 4 || failed != 0 {
		t.Fatalf("moved %d and failed %d, want 4 and 0:\n%s", moved, failed, out.String())
	}

	sameTree(t, map[string]string{
		"agents/go-dev.md": before["agents/go-dev.md"],
		"settings.json":    before["settings.json"],
		".env":             before[".env"],
	}, snapshot(t, config), "the project's .tofu after the move")
	sameTree(t, map[string]string{
		"sessions/HEAD":                before["sessions/HEAD"],
		"sessions/turn-a/header.json":  before["sessions/turn-a/header.json"],
		"sessions/turn-a/events.jsonl": before["sessions/turn-a/events.jsonl"],
		"log/decisions.jsonl":          before["log/decisions.jsonl"],
		"promotions.jsonl":             before["promotions.jsonl"],
	}, snapshot(t, state), "the state folder under the home")
	quota := snapshot(t, filepath.Join(home, sys.StateDirName, sys.QuotaDirName))
	if quota["readings.jsonl"] != before["quota/readings.jsonl"] {
		t.Fatalf("the quota readings are not under the home: %v", quota)
	}
	if said := out.String(); strings.Count(said, "\n") != 1 || !strings.Contains(said, state) || !strings.Contains(said, "sessions") {
		t.Fatalf("the notice is not one line naming what moved and where:\n%s", said)
	}

	var second bytes.Buffer
	if moved, failed := moveProjectState(&second, project); moved+failed != 0 || second.Len() != 0 {
		t.Fatalf("the second run moved %d, failed %d and said:\n%s", moved, failed, second.String())
	}
}

func TestAFileAlreadyMovedWithOtherBytesKeepsTheOldCopyAndSaysWhy(t *testing.T) {
	_, project := projectWithState(t)
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"sessions/turn-a/header.json": `{"id":"turn-a"}`,
		"log/decisions.jsonl":         "a row the new build wrote first\n",
	} {
		full := filepath.Join(state, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	moved, failed := moveProjectState(&out, project)
	if moved != 3 || failed != 1 {
		t.Fatalf("moved %d and failed %d, want 3 and 1:\n%s", moved, failed, out.String())
	}
	old := filepath.Join(project, sys.StateDirName, "log", "decisions.jsonl")
	if body, err := os.ReadFile(old); err != nil || string(body) != "{\"point\":\"tool_gate\"}\n" {
		t.Fatalf("the old log was not kept whole: %q, %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(project, sys.StateDirName, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("the sessions held a file already moved with the same bytes and still did not move: %v", err)
	}
	if said := out.String(); !strings.Contains(said, "decisions.jsonl") || !strings.Contains(said, "tofu migrate") {
		t.Fatalf("the notice does not name the conflict and the way out:\n%s", said)
	}
}

func TestAProjectThatIsTheHomeMovesItsStateAndLeavesQuotaWhereItIs(t *testing.T) {
	home, _ := projectWithState(t)
	project := home
	if err := os.MkdirAll(filepath.Join(project, sys.StateDirName, sys.QuotaDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, sys.StateDirName, sys.QuotaDirName, "readings.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if moved, failed := moveProjectState(&out, project); failed != 0 || moved != 0 {
		t.Fatalf("a home holding only quota moved %d and failed %d:\n%s", moved, failed, out.String())
	}
	if _, err := os.Stat(filepath.Join(project, sys.StateDirName, sys.QuotaDirName, "readings.jsonl")); err != nil {
		t.Fatalf("the quota under the home was moved: %v", err)
	}
}

func TestADryRunListsTheMoveAndTouchesNothing(t *testing.T) {
	_, project := projectWithState(t)
	t.Chdir(project)
	config := filepath.Join(project, sys.StateDirName)
	before := snapshot(t, config)

	var out, errOut bytes.Buffer
	if code := run([]string{"migrate", "--dry-run"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	sameTree(t, before, snapshot(t, config), "the dry run changed the project")
	for _, name := range []string{"sessions", "log", "quota", "promotions.jsonl"} {
		if !strings.Contains(out.String(), name) {
			t.Fatalf("the dry run does not list %s:\n%s", name, out.String())
		}
	}
	if strings.Contains(out.String(), "agents") || strings.Contains(out.String(), "settings.json") {
		t.Fatalf("the dry run lists config as state:\n%s", out.String())
	}
	t.Log(out.String())
}
