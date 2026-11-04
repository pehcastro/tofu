package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func tsProject(t *testing.T, runner, lockfile string, files map[string]string) string {
	if _, err := exec.LookPath(runner); err != nil {
		t.Skip(runner + " is not on PATH")
	}
	dir := filepath.Clean(t.TempDir())
	for name, body := range map[string]string{"tsconfig.json": `{"compilerOptions":{"strict":true}}`, "package.json": `{"name":"fallback"}`, lockfile: "{}"} {
		if _, given := files[name]; !given {
			files[name] = body
		}
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
