package sys

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
)

func freshHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func folders(t *testing.T, dirs ...string) []string {
	t.Helper()
	var got []string
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		state, err := ProjectStateDirAt(dir)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(state))
	}
	return got
}

func TestProjectFolderTwoPathsTheOldKeyMergedGetTwoFolders(t *testing.T) {
	freshHome(t)
	root := t.TempDir()
	got := folders(t, filepath.Join(root, "a-b"), filepath.Join(root, "a b"), filepath.Join(root, "a", "b"), filepath.Join(root, "a_b"))
	seen := map[string]bool{}
	for _, folder := range got {
		if seen[folder] {
			t.Fatalf("two different folders share the state folder %s: %v", folder, got)
		}
		seen[folder] = true
	}
}

func TestProjectFolderIsTheFolderNameAndAShortHash(t *testing.T) {
	freshHome(t)
	got := folders(t, filepath.Join(t.TempDir(), "Bob Builder"))[0]
	name, hash, found := strings.Cut(got, "-")
	if !found || name != "Bob" || !strings.HasPrefix(hash, "Builder-") || len(got) != len("Bob-Builder-")+8 {
		t.Fatalf("the state folder of Bob Builder is %q, want Bob-Builder- and eight characters of hash", got)
	}
}

func TestProjectFolderOfADeepPathStaysShort(t *testing.T) {
	freshHome(t)
	deep := filepath.Join(t.TempDir(), strings.Repeat("deep-segment-", 4), "a", "b", "c", "d", "e", "f", "g", strings.Repeat("long-final-folder-name-", 6))
	got := folders(t, deep)[0]
	if len(got) > 49 {
		t.Fatalf("a project nested deep got the folder %q, %d characters, want at most 49", got, len(got))
	}
}

func TestProjectFolderOfAVolumeRootIsNamed(t *testing.T) {
	root := string(filepath.Separator)
	if OS() == "windows" {
		root = filepath.VolumeName(t.TempDir()) + `\`
	}
	got := projectKey(root)
	if strings.HasPrefix(got, "-") || len(got) < 9 {
		t.Fatalf("the volume root %s got the folder %q", root, got)
	}
}

func TestProjectFolderFoldsCaseOnlyWhereTheFileSystemDoes(t *testing.T) {
	freshHome(t)
	dir := filepath.Join(t.TempDir(), "Mixed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	upper, lower := projectKey(strings.ToUpper(dir)), projectKey(strings.ToLower(dir))
	folds := OS() == "windows" || OS() == "darwin"
	if folds && !strings.EqualFold(upper, lower) {
		t.Fatalf("on %s two spellings of one folder got %s and %s", OS(), upper, lower)
	}
	if !folds && upper == lower {
		t.Fatalf("on %s two different folders that differ only in case share %s", OS(), upper)
	}
}

func TestProjectFolderOfALinkIsTheFolderOfItsTarget(t *testing.T) {
	freshHome(t)
	root := t.TempDir()
	target, link := filepath.Join(root, "real"), filepath.Join(root, "link")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if OS() == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v %s", err, out)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got, want := projectKey(link), projectKey(target); got != want {
		t.Fatalf("the link %s got %s and its target %s got %s", link, got, target, want)
	}
}

func TestProjectFolderClaimedByAnotherPathIsNotShared(t *testing.T) {
	home := freshHome(t)
	first, second := filepath.Join(t.TempDir(), "one"), filepath.Join(t.TempDir(), "two")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := `{"projects":[{"folder":` + quoteJSON(projectKey(second)) + `,"paths":[` + quoteJSON(first) + `]}]}`
	if err := WriteFile(filepath.Join(home, StateDirName, ProjectRegistryName), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := ProjectStateDirAt(second)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(state) == projectKey(second) {
		t.Fatalf("%s was handed %s, which the registry gives to %s", second, state, first)
	}
}

func TestProjectFolderACorruptRegistryIsAnError(t *testing.T) {
	home := freshHome(t)
	if err := WriteFile(filepath.Join(home, StateDirName, ProjectRegistryName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := ProjectStateDirAt(t.TempDir()); err == nil {
		t.Fatalf("a corrupt registry handed out %s", state)
	}
}

func TestProjectOpenOffersTheOldKeyFolderOnceAndRegisters(t *testing.T) {
	home := freshHome(t)
	project := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(project)
	legacy := filepath.Join(home, StateDirName, ProjectsDirName, legacyProjectKey(abs))
	if err := WriteFile(filepath.Join(legacy, "sessions", "HEAD"), []byte("s1"), 0o644); err != nil {
		t.Fatal(err)
	}
	opening, err := OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if opening.Legacy != legacy {
		t.Fatalf("the old-key folder %s was not offered for the copy: %+v", legacy, opening)
	}
	if err := os.MkdirAll(opening.State, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := opening.Register(); err != nil {
		t.Fatal(err)
	}
	toml, err := os.ReadFile(filepath.Join(opening.State, ProjectFileName))
	if err != nil || !strings.Contains(string(toml), "proj") {
		t.Fatalf("the project file says %q, %v", toml, err)
	}
	again, err := OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if again.Legacy != "" || again.State != opening.State || !again.Registered {
		t.Fatalf("the second open is %+v, want the same registered folder and no copy", again)
	}
}

func gitRepository(t *testing.T, dir, remote string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "root"},
		{"remote", "add", "origin", remote},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestProjectOpenOffersTheRelinkOfAMovedRepositoryAndKeepsItsFolder(t *testing.T) {
	home := freshHome(t)
	root := t.TempDir()
	before, after := filepath.Join(root, "before"), filepath.Join(root, "after")
	if err := os.MkdirAll(before, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRepository(t, before, "https://ada:secret-token@example.com/ada/repo.git")
	opening, err := OpenProject(before)
	if err != nil {
		t.Fatal(err)
	}
	if opening.Moved != nil {
		t.Fatalf("a repository opened for the first time was offered a relink to %+v", opening.Moved)
	}
	if err := opening.Register(); err != nil {
		t.Fatal(err)
	}
	registry, _ := os.ReadFile(filepath.Join(home, StateDirName, ProjectRegistryName))
	if strings.Contains(string(registry), "secret-token") {
		t.Fatalf("the registry holds the credential in the remote: %s", registry)
	}
	if err := os.Rename(before, after); err != nil {
		t.Fatal(err)
	}
	moved, err := OpenProject(after)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Moved == nil || moved.Moved.Folder != filepath.Base(opening.State) {
		t.Fatalf("the repository moved from %s to %s and was not offered its folder %s: %+v", before, after, opening.State, moved)
	}
	if _, err := moved.Relink(); err != nil {
		t.Fatal(err)
	}
	if state, err := ProjectStateDirAt(after); err != nil || state != opening.State {
		t.Fatalf("after the relink %s resolves to %s, %v, want %s", after, state, err, opening.State)
	}
}

func TestProjectOpenOffersNoRelinkWhileTheOldPathStillExists(t *testing.T) {
	freshHome(t)
	root := t.TempDir()
	first, clone := filepath.Join(root, "first"), filepath.Join(root, "clone")
	for _, dir := range []string{first, clone} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitRepository(t, dir, "https://example.com/ada/repo.git")
	}
	opening, err := OpenProject(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := opening.Register(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenProject(clone)
	if err != nil {
		t.Fatal(err)
	}
	if second.Moved != nil {
		t.Fatalf("a second clone beside a live first one was offered the first one's folder: %+v", second.Moved)
	}
}

func TestProjectStateReadsTheOldKeyFolderUntilTheOpenerCopiesIt(t *testing.T) {
	home := freshHome(t)
	project := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(project)
	legacy := filepath.Join(home, StateDirName, ProjectsDirName, legacyProjectKey(abs))
	if err := WriteFile(filepath.Join(legacy, "sessions", "HEAD"), []byte("s1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := ProjectStateDirAt(project); err != nil || state != legacy {
		t.Fatalf("before the copy the state of %s is %s, %v, want the old-key folder %s", project, state, err, legacy)
	}
	opening, err := OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := opening.Register(); err != nil {
		t.Fatal(err)
	}
	if state, err := ProjectStateDirAt(project); err != nil || state != opening.State {
		t.Fatalf("after the copy the state of %s is %s, %v, want %s", project, state, err, opening.State)
	}
	folders, err := ProjectGlob(filepath.Join(home, StateDirName, ProjectsDirName), "*")
	if err != nil || len(folders) != 1 || folders[0] != opening.State {
		t.Fatalf("the walk over projects found %v, %v, want only %s and not the old-key folder it was copied from", folders, err, opening.State)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("the old-key folder is gone after the copy: %v", err)
	}
}

func TestProjectMoveTakesTheOldKeyFolderOverAHalfCopy(t *testing.T) {
	home := freshHome(t)
	project := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(project)
	legacy := filepath.Join(home, StateDirName, ProjectsDirName, legacyProjectKey(abs))
	if err := WriteFile(filepath.Join(legacy, "sessions", "a", "session.json"), []byte("whole"), 0o644); err != nil {
		t.Fatal(err)
	}
	opening, err := OpenProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filepath.Join(opening.State, "sessions", "a", "session.json"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filepath.Join(opening.State, "sessions", "b", "session.json"), []byte("stray"), 0o644); err != nil {
		t.Fatal(err)
	}
	if replaced, err := opening.MoveLegacy(); err != nil || !replaced {
		t.Fatalf("the move over a half copy says replaced %v, %v", replaced, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("the old-key folder %s is still there after the move: %v", legacy, err)
	}
	if body, err := os.ReadFile(filepath.Join(opening.State, "sessions", "a", "session.json")); err != nil || string(body) != "whole" {
		t.Fatalf("the moved session reads %q, %v, want the old-key bytes and not the half copy", body, err)
	}
	if _, err := os.Stat(filepath.Join(opening.State, "sessions", "b")); !os.IsNotExist(err) {
		t.Fatalf("the half copy survived the move: %v", err)
	}
	if state, err := ProjectStateDirAt(project); err != nil || state != opening.State {
		t.Fatalf("killed before the registry, the state of %s is %s, %v, want %s", project, state, err, opening.State)
	}
	again, err := OpenProject(project)
	if err != nil || again.Legacy != "" || again.State != opening.State {
		t.Fatalf("the open after a killed move is %+v, %v, want the moved folder and nothing to move", again, err)
	}
	if err := again.Register(); err != nil {
		t.Fatal(err)
	}
	if state, err := ProjectStateDirAt(project); err != nil || state != opening.State {
		t.Fatalf("after the registry the state of %s is %s, %v, want %s", project, state, err, opening.State)
	}
}

func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

func openedAs(t *testing.T, temporary bool, dir string) Project {
	t.Helper()
	if err := WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	project.Temporary = temporary
	if project.Registered {
		err = project.Reopen()
	} else {
		err = project.RegisterIdentityLater()
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filepath.Join(project.State, "sessions", "s1", "session.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func registered(t *testing.T, home string) map[string]ProjectEntry {
	t.Helper()
	registry, err := readProjectRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	byFolder := map[string]ProjectEntry{}
	for _, entry := range registry.Projects {
		byFolder[entry.Folder] = entry
	}
	return byFolder
}

func stateHome(t *testing.T) string {
	t.Helper()
	freshHome(t)
	home, err := HomeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestProjectReapRemovesOnlyTemporaryStateIdlePastTheLimit(t *testing.T) {
	home := stateHome(t)
	root := t.TempDir()
	idle, used, kept := openedAs(t, true, filepath.Join(root, "idle")), openedAs(t, true, filepath.Join(root, "used")), openedAs(t, false, filepath.Join(root, "kept"))
	if entry := registered(t, home)[filepath.Base(idle.State)]; !entry.Temporary || entry.Used.IsZero() {
		t.Fatalf("the project opened temporary is registered as %+v", entry)
	}
	limit := konst.TemporaryProjectIdleHours * time.Hour
	later := time.Now().Add(limit + time.Minute)
	if err := os.Chtimes(filepath.Join(used.State, "sessions", "s1", "session.json"), later, later.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	removed, err := ReapTemporaryProjects(home, later)
	if err != nil || len(removed) != 1 || removed[0] != filepath.Base(idle.State) {
		t.Fatalf("the reap removed %v, %v, want only %s", removed, err, filepath.Base(idle.State))
	}
	byFolder := registered(t, home)
	if _, found := byFolder[filepath.Base(idle.State)]; found {
		t.Fatalf("the idle temporary project is still registered: %+v", byFolder)
	}
	if _, err := os.Stat(idle.State); !os.IsNotExist(err) {
		t.Fatalf("the idle temporary state %s survived: %v", idle.State, err)
	}
	if _, err := os.Stat(filepath.Join(idle.Path, "main.go")); err != nil {
		t.Fatalf("the reap touched the project itself: %v", err)
	}
	for _, survivor := range []Project{used, kept} {
		if _, found := byFolder[filepath.Base(survivor.State)]; !found {
			t.Fatalf("%s left the registry", survivor.State)
		}
		if _, err := os.Stat(filepath.Join(survivor.State, "sessions", "s1", "session.json")); err != nil {
			t.Fatalf("%s lost its session: %v", survivor.State, err)
		}
	}
	if removed, err := ReapTemporaryProjects(home, later.Add(1000*limit)); err != nil || len(removed) != 1 || removed[0] != filepath.Base(used.State) {
		t.Fatalf("far later the reap removed %v, %v, want only %s and never the permanent project", removed, err, filepath.Base(used.State))
	}
}

func TestProjectReapKeepsAnIdleTemporaryProjectWhileATofuHoldsItsSession(t *testing.T) {
	home := stateHome(t)
	open := openedAs(t, true, filepath.Join(t.TempDir(), "open"))
	held, busy, err := tryLock(filepath.Join(open.State, "sessions", "s1", "lock"))
	if err != nil || busy {
		t.Fatalf("the session lock was not taken: busy %v, %v", busy, err)
	}
	t.Cleanup(func() { _ = held.Close() })
	far := time.Now().Add(1000 * time.Hour)
	if removed, err := ReapTemporaryProjects(home, far); err != nil || len(removed) != 0 {
		t.Fatalf("the reap removed %v, %v while a session lock was held", removed, err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if removed, err := ReapTemporaryProjects(home, far); err != nil || len(removed) != 1 {
		t.Fatalf("the reap removed %v, %v once the lock was let go, want the project", removed, err)
	}
}

func TestProjectReapKeepsAnIdleTemporaryProjectWhileATofuHasItOpen(t *testing.T) {
	home := stateHome(t)
	open := openedAs(t, true, filepath.Join(t.TempDir(), "open"))
	release := HoldProjectOpen(open.Path)
	far := time.Now().Add(1000 * time.Hour)
	if removed, err := ReapTemporaryProjects(home, far); err != nil || len(removed) != 0 {
		t.Fatalf("the reap removed %v, %v while a tofu had the project open", removed, err)
	}
	release()
	if removed, err := ReapTemporaryProjects(home, far); err != nil || len(removed) != 1 {
		t.Fatalf("the reap removed %v, %v once the tofu let go, want the project", removed, err)
	}
}

func TestProjectReapDeletesLeftoverReapedFolders(t *testing.T) {
	home := stateHome(t)
	openedAs(t, false, filepath.Join(t.TempDir(), "kept"))
	leftover := filepath.Join(home, ProjectsDirName, reapedDirName, "gone-12345678")
	if err := WriteFile(filepath.Join(leftover, "sessions", "s1", "session.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := ReapTemporaryProjects(home, time.Now()); err != nil || len(removed) != 0 {
		t.Fatalf("the reap removed %v, %v", removed, err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("the leftover %s survived the reap: %v", leftover, err)
	}
}

func TestProjectReapNeverRemovesOutsideTheProjectsFolder(t *testing.T) {
	home := stateHome(t)
	outside := t.TempDir()
	sentinels := []string{filepath.Join(home, "keep"), outside, filepath.Join(home, ProjectsDirName)}
	for _, dir := range sentinels {
		if err := WriteFile(filepath.Join(dir, "sentinel"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var entries []string
	for _, folder := range []string{"..", "", ".", "../keep", outside, "a/../../keep"} {
		entries = append(entries, `{"folder": `+quoteJSON(folder)+`, "paths": [], "temporary": true, "created": "2000-01-01T00:00:00Z", "used": "2000-01-01T00:00:00Z"}`)
	}
	if err := WriteFile(filepath.Join(home, ProjectRegistryName), []byte(`{"projects": [`+strings.Join(entries, ",")+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := ReapTemporaryProjects(home, time.Now().Add(1000*time.Hour)); len(removed) != 0 || err != nil {
		t.Fatalf("the reap removed %v from a registry naming folders outside %s, %v", removed, ProjectsDirName, err)
	}
	for _, dir := range sentinels {
		if _, err := os.Stat(filepath.Join(dir, "sentinel")); err != nil {
			t.Fatalf("the reap reached %s: %v", dir, err)
		}
	}
	if got := len(registered(t, home)); got != 6 {
		t.Fatalf("the registry holds %d folders after the reap, want all 6 kept", got)
	}
}

func TestProjectOpenedWithoutTheFlagStopsBeingTemporaryAndTheFlagNeverMarksAKeptOne(t *testing.T) {
	home := stateHome(t)
	root := t.TempDir()
	scratch := openedAs(t, true, filepath.Join(root, "scratch"))
	openedAs(t, false, scratch.Path)
	kept := openedAs(t, false, filepath.Join(root, "kept"))
	openedAs(t, true, kept.Path)
	for folder, entry := range registered(t, home) {
		if entry.Temporary {
			t.Fatalf("%s is still temporary: %+v", folder, entry)
		}
	}
	if removed, err := ReapTemporaryProjects(home, time.Now().Add(1000*time.Hour)); err != nil || len(removed) != 0 {
		t.Fatalf("the reap removed %v, %v from projects opened without the flag", removed, err)
	}
}

func TestProjectReapWithNothingIdleLeavesTheRegistryUnwritten(t *testing.T) {
	home := stateHome(t)
	openedAs(t, false, filepath.Join(t.TempDir(), "kept"))
	name := filepath.Join(home, ProjectRegistryName)
	for _, step := range []struct {
		open func()
		at   time.Time
	}{
		{func() {}, time.Now().Add(1000 * time.Hour)},
		{func() { openedAs(t, true, filepath.Join(t.TempDir(), "fresh")) }, time.Now()},
	} {
		step.open()
		past := time.Now().Add(-time.Hour).Truncate(time.Second)
		if err := os.Chtimes(name, past, past); err != nil {
			t.Fatal(err)
		}
		if removed, err := ReapTemporaryProjects(home, step.at); err != nil || len(removed) != 0 {
			t.Fatalf("the reap removed %v, %v", removed, err)
		}
		if info, err := os.Stat(name); err != nil || !info.ModTime().Equal(past) {
			t.Fatalf("the registry was written by a reap with nothing to do: %v", err)
		}
	}
}
