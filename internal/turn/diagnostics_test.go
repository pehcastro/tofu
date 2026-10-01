package turn_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/turn"
)

func TestTypecheckedFallsBackWhenTheProjectHasNoTypeScript(t *testing.T) {
	for _, toolchain := range []struct{ runner, lockfile string }{{"bun", "bun.lock"}, {"npx", "package-lock.json"}} {
		t.Run(toolchain.runner, func(t *testing.T) {
			if _, err := exec.LookPath(toolchain.runner); err != nil {
				t.Skip(toolchain.runner + " is not on PATH")
			}
			dir := t.TempDir()
			files := map[string]string{
				"tsconfig.json":    `{"compilerOptions":{"strict":true}}`,
				"package.json":     `{"name":"fallback"}`,
				toolchain.lockfile: "{}",
				"broken.ts":        "const count: number = \"many\";\n",
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := turn.Typechecked(context.Background(), filepath.Join(dir, "broken.ts"), "wrote broken.ts")
			if !strings.Contains(got, "broken.ts(1,7): error TS2322") {
				t.Fatalf("the write result carries no tsc error:\n%s", got)
			}
		})
	}
}
