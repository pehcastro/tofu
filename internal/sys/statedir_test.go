package sys

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestNoSourceRootHoldsARecordedStateDirectory(t *testing.T) {
	root := SourceRoot()
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("this guard reads the source tree and %s is not it: %v", root, err)
	}
	var held []string
	for _, source := range []string{"bench", "cmd", "interface", "library", "internal"} {
		walked := filepath.Join(root, source)
		err := filepath.WalkDir(walked, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			if entry.Name() != StateDirName && entry.Name() != LegacyStateDirName {
				return nil
			}
			for _, name := range MovedStateNames() {
				if _, err := os.Stat(filepath.Join(path, name)); err == nil {
					held = append(held, filepath.Join(path, name))
				}
			}
			return fs.SkipDir
		})
		if err != nil {
			t.Fatalf("walking %s: %v", walked, err)
		}
	}
	if len(held) > 0 {
		t.Fatalf("a source root holds a recorded state directory, which a test wrote by running with its own package as the working directory: %v", held)
	}
}

func TestATestInsideTheSourceTreeGetsAStateDirectoryOutsideIt(t *testing.T) {
	for name, dir := range map[string]func() (string, error){"state": ProjectStateDir, "config": ProjectConfigDir} {
		inside, err := dir()
		if err != nil {
			t.Fatal(err)
		}
		if InsideSourceTree(inside) {
			t.Fatalf("a test working in the source tree was handed the %s directory %s, which is in the source tree", name, inside)
		}
		t.Logf("a test working in %s keeps its %s in %s", mustGetwd(t), name, inside)
	}

	t.Chdir(t.TempDir())
	outside, err := ProjectConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(mustGetwd(t), StateDirName); outside != want {
		t.Fatalf("outside the source tree the project config directory is %s, want %s", outside, want)
	}
}

func TestAProjectsStateLivesUnderTheHomeAndNotInTheProject(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir, err := ProjectStateDirAt(project)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, StateDirName, ProjectsDirName, ProjectKey(project)); dir != want {
		t.Fatalf("the state of %s is %s, want %s", project, dir, want)
	}
	if inside(project, dir) {
		t.Fatalf("the state of %s is inside the project, at %s", project, dir)
	}
}

func TestAProjectKeyIsThePathWithEveryOtherCharacterADash(t *testing.T) {
	cases := map[string]string{
		`Q:\code\ephem-sh\bob`:                  "Q--code-ephem-sh-bob",
		`Q:\code\ephem-sh\bob\.local\sources\x`: "Q--code-ephem-sh-bob--local-sources-x",
		"/home/ada/my_project.v2":               "-home-ada-my-project-v2",
	}
	if OS() == "windows" {
		cases[`q:\code\bob`] = "Q--code-bob"
	}
	for path, want := range cases {
		if got := ProjectKey(path); got != want {
			t.Fatalf("ProjectKey(%q) is %q, want %q", path, got, want)
		}
	}
}

func TestATestWorkingAtTheSourceRootItselfGetsAStateDirectoryOutsideIt(t *testing.T) {
	t.Chdir(SourceRoot())
	for _, dir := range []func() (string, error){ProjectStateDir, ProjectConfigDir} {
		got, err := dir()
		if err != nil {
			t.Fatal(err)
		}
		if InsideSourceTree(got) || got == OwnerProjectStateDir(SourceRoot()) {
			t.Fatalf("a test working at the source root was handed %s, which is the owner's own directory", got)
		}
	}
}

func TestTheRedirectedHomeIsNeitherTheOwnersNorTheProjects(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := HomeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir == StateDir(home) {
		t.Fatalf("a test that set no home was handed %s, where the live credential and the credential database live", dir)
	}
	project, err := ProjectConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir == project {
		t.Fatalf("the redirected home and the redirected project are both %s, and a layered read cannot tell them apart", dir)
	}
}

func TestAHomeTheTestChoseIsTheHomeItGets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir, err := HomeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, StateDirName); dir != want {
		t.Fatalf("a test that chose %s was handed %s, want %s", home, dir, want)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
