package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tofu/internal/sys"
)

var builtTofu struct {
	once sync.Once
	path string
	err  error
}

func tofuBinary(t *testing.T) string {
	t.Helper()
	builtTofu.once.Do(func() {
		dir := filepath.Join(os.TempDir(), "tofu-e2e")
		if builtTofu.err = os.MkdirAll(dir, 0o755); builtTofu.err != nil {
			return
		}
		path := filepath.Join(dir, "tofu")
		if sys.OS() == "windows" {
			path += ".exe"
		}
		said, err := exec.Command("go", "build", "-o", path, ".").CombinedOutput()
		if err != nil {
			builtTofu.err = fmt.Errorf("go build -o %s .: %w: %s", path, err, said)
			return
		}
		builtTofu.path = path
		t.Logf("built the tofu binary once, at %s", path)
	})
	if builtTofu.err != nil {
		t.Fatal(builtTofu.err)
	}
	return builtTofu.path
}

type project struct {
	dir  string
	home string
}

func newProject(t *testing.T, name string) project {
	t.Helper()
	root := t.TempDir()
	made := project{dir: filepath.Join(root, name), home: filepath.Join(root, name+"-home")}
	for _, dir := range []string{made.dir, made.home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return made
}

func writeFile(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p project) run(t *testing.T, wantCode int, args ...string) string {
	t.Helper()
	command := exec.Command(tofuBinary(t), args...)
	command.Dir = p.dir
	command.Env = []string{
		"USERPROFILE=" + p.home,
		"HOME=" + p.home,
		"TEMP=" + p.home,
		"TMP=" + p.home,
		"SystemRoot=" + os.Getenv("SystemRoot"),
	}
	var said bytes.Buffer
	command.Stdout, command.Stderr = &said, &said
	var stopped *exec.ExitError
	if err := command.Run(); err != nil && !errors.As(err, &stopped) {
		t.Fatalf("tofu %s: %v\n%s", strings.Join(args, " "), err, said.String())
	}
	if code := command.ProcessState.ExitCode(); code != wantCode {
		t.Fatalf("tofu %s exited %d rather than %d, and a script reads the code rather than the words\n%s",
			strings.Join(args, " "), code, wantCode, said.String())
	}
	return said.String()
}

func (p project) root(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func sameText(t *testing.T, what, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	t.Fatalf("%s\n--- tofu printed ---\n%s\n--- the state says it should print ---\n%s", what, got, want)
}
