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
		ldflags := "-X tofu/internal/sys.builtGuarded=1"
		said, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", path, ".").CombinedOutput()
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

func runBinary(t *testing.T, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	command := exec.Command(tofuBinary(t), args...)
	command.Dir = dir
	command.Env = env
	var said bytes.Buffer
	command.Stdout, command.Stderr = &said, &said
	var stopped *exec.ExitError
	if err := command.Run(); err != nil && !errors.As(err, &stopped) {
		t.Fatalf("tofu %s: %v\n%s", strings.Join(args, " "), err, said.String())
	}
	return said.String(), command.ProcessState.ExitCode()
}

func (p project) run(t *testing.T, wantCode int, args ...string) string {
	t.Helper()
	env := []string{
		"USERPROFILE=" + p.home,
		"HOME=" + p.home,
		"TEMP=" + p.home,
		"TMP=" + p.home,
		"SystemRoot=" + os.Getenv("SystemRoot"),
	}
	said, code := runBinary(t, p.dir, env, args...)
	if code != wantCode {
		t.Fatalf("tofu %s exited %d rather than %d, and a script reads the code rather than the words\n%s",
			strings.Join(args, " "), code, wantCode, said)
	}
	return said
}

func (p project) root(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestE2ETheCredentialGuardRefusesAPlantedFileWithNoUSERPROFILE(t *testing.T) {
	plant := filepath.Join(sys.SourceRoot(), "cmd", "tofu", "credential-guard-plant")
	if err := os.MkdirAll(plant, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(plant) })
	writeFile(t, plant, ".env", "OPENROUTER_KEY=planted-not-real\n")
	plantedEnv, err := filepath.Abs(filepath.Join(plant, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	env := []string{"SystemRoot=" + os.Getenv("SystemRoot")}
	printed, code := runBinary(t, plant, env, "check", "--quiet", "true")
	if code != exitUsage {
		t.Fatalf("tofu check exited %d rather than %d\n%s", code, exitUsage, printed)
	}
	if !strings.Contains(printed, plantedEnv) {
		t.Fatalf("the refusal does not name the planted path %s:\n%s", plantedEnv, printed)
	}
	if strings.Contains(printed, "planted-not-real") {
		t.Fatalf("the refusal printed the planted value rather than only the path:\n%s", printed)
	}
}

func TestModelsLoadAndBindAClassifierWithNoHome(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	classifier, err := boundClassifier()
	if err != nil {
		t.Fatalf("with no home the model library did not load: %v", err)
	}
	if classifier.Provider.KeyName() == "" {
		t.Fatalf("with no home the classifier %q names no key", classifier.ID)
	}
}

func sameText(t *testing.T, what, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	t.Fatalf("%s\n--- tofu printed ---\n%s\n--- the state says it should print ---\n%s", what, got, want)
}
