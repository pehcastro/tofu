package turn

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func link(t *testing.T, name, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", name, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink %s: %v %s", name, err, out)
		}
		return
	}
	relative, err := filepath.Rel(filepath.Dir(name), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, name); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRefusesALinkThatLeadsOutsideTheRoot(t *testing.T) {
	base := t.TempDir()
	project, outside := filepath.Join(base, "project"), filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(project, "inner"), outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link(t, filepath.Join(project, "out"), outside)
	link(t, filepath.Join(project, "in"), filepath.Join(project, "inner"))
	link(t, filepath.Join(project, "gone"), filepath.Join(outside, "gone"))
	link(t, filepath.Join(project, "self"), filepath.Join(project, "self"))
	link(t, filepath.Join(project, "ping"), filepath.Join(project, "pong"))
	link(t, filepath.Join(project, "pong"), filepath.Join(project, "ping"))
	link(t, filepath.Join(outside, "back"), filepath.Join(project, "inner"))
	link(t, filepath.Join(base, "via"), project)
	rows := []struct {
		root, path, refusal string
	}{
		{project, "out/x.txt", `link "out"`},
		{project, "out/new/deeper.txt", `link "out"`},
		{project, "gone/new.txt", `link "gone"`},
		{project, "self/a.txt", "is a loop"},
		{project, "ping/a.txt", "is a loop"},
		{project, "in/y.txt", ""},
		{project, "in/new.txt", ""},
		{project, "out/back/y.txt", ""},
		{project, "inner/../in/y.txt", ""},
		{filepath.Join(base, "via"), "in/y.txt", ""},
		{filepath.Join(base, "via"), "out/x.txt", `link "out"`},
	}
	for _, row := range rows {
		got, err := Root(row.root).Resolve(row.path)
		if row.refusal == "" {
			if want := filepath.Join(row.root, row.path); err != nil || got != want {
				t.Errorf("%s under %s: got %q, %v, want %q", row.path, row.root, got, err, want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), row.refusal) {
			t.Errorf("%s under %s: got %q, %v, want a refusal naming %s", row.path, row.root, got, err, row.refusal)
		}
	}
}
