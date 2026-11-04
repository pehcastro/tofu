package registry

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"tofu/internal/tools/contract"
)

func linkTo(t *testing.T, name, target string) {
	t.Helper()
	info, err := os.Stat(target)
	if runtime.GOOS == "windows" && err == nil && info.IsDir() {
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

func TestWalkNeverFollowsALinkOutsideTheProject(t *testing.T) {
	base := t.TempDir()
	project, outside := filepath.Join(base, "project"), filepath.Join(base, "outside")
	for _, dir := range []string{"inner", "a", "b"} {
		if err := os.MkdirAll(filepath.Join(project, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		filepath.Join(outside, "secret.txt"):     "outside secret",
		filepath.Join(project, "inner", "y.txt"): "inside secret",
		filepath.Join(project, ".gitignore"):     "ghost\n",
	} {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	linkTo(t, filepath.Join(project, "out"), outside)
	linkTo(t, filepath.Join(project, "in"), filepath.Join(project, "inner"))
	linkTo(t, filepath.Join(project, "inner", "loop"), project)
	linkTo(t, filepath.Join(project, "a", "l"), filepath.Join(project, "b"))
	linkTo(t, filepath.Join(project, "b", "l"), filepath.Join(project, "a"))
	linkTo(t, filepath.Join(project, "leak.txt"), filepath.Join(outside, "secret.txt"))
	linkTo(t, filepath.Join(project, "near.txt"), filepath.Join(project, "inner", "y.txt"))
	linkTo(t, filepath.Join(project, "node_modules"), filepath.Join(project, "inner"))
	for _, name := range []string{"dangling", "ghost"} {
		if err := os.Symlink("nowhere", filepath.Join(project, name)); err != nil {
			t.Fatal(err)
		}
	}
	const counted = "links not followed: 2,"
	glob, err := NewGlob(project)
	if err != nil {
		t.Fatal(err)
	}
	search := Search{root: contract.Root(project)}
	rows := []struct {
		tool contract.Tool
		args string
		want []string
	}{
		{glob, `{"pattern":"**/*"}`, []string{"3 of 3 files", ".gitignore", "inner/y.txt", "near.txt", counted}},
		{glob, `{"pattern":"**/*","path":"in"}`, []string{"1 of 1 files", "in/y.txt"}},
		{glob, `{"pattern":"**/*","path":"inner/y.txt"}`, []string{"1 of 1 files", "inner/y.txt"}},
		{glob, `{"pattern":"**/*","path":"node_modules"}`, []string{"no file under node_modules"}},
		{search, `{"pattern":"secret"}`, []string{"inner/y.txt", "near.txt", counted}},
	}
	for _, row := range rows {
		result, err := row.tool.Run(context.Background(), json.RawMessage(row.args))
		if err != nil {
			t.Errorf("%s %s: %v", row.tool.Name(), row.args, err)
			continue
		}
		for _, line := range strings.Split(result.Content, "\n") {
			wanted := slices.ContainsFunc(row.want, func(want string) bool { return strings.HasPrefix(line, want) })
			if !wanted && (strings.Contains(line, "/") || strings.Contains(line, ".txt") || strings.Contains(line, "links")) {
				t.Errorf("%s %s: unexpected line %q", row.tool.Name(), row.args, line)
			}
		}
		for _, want := range row.want {
			if !strings.Contains(result.Content, want) {
				t.Errorf("%s %s: %q missing:\n%s", row.tool.Name(), row.args, want, result.Content)
			}
		}
	}
}
