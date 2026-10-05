package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"tofu/internal/turn"
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
	glob, search := Glob{root: turn.Root(project)}, Search{root: turn.Root(project)}
	rows := []struct {
		tool turn.Tool
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

var globTree = []string{
	".github/w.ts", "[x].txt", "a,b.txt", "a.ts", "abts", "lib/f.ts", "src/b.ts", "src/d.go",
	"src/q.js", "src/q.min.js", "src/x/y/c.tsx", "src/x/y/e.ts", "{a}.txt", "é.md",
}

func globTool(t *testing.T) Glob {
	dir := t.TempDir()
	for _, rel := range globTree {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tool, err := NewGlob(dir)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func globbed(t *testing.T, tool Glob, pattern string) ([]string, error) {
	raw, err := json.Marshal(globArgs{Pattern: pattern})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Run(context.Background(), raw)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(result.Content, "no file under") {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")[1:]
	slices.Sort(lines)
	return lines, nil
}

func TestGlobBracesAndDoubleStar(t *testing.T) {
	tool := globTool(t)
	cases := map[string][]string{
		"src/**/*.ts":               {"src/b.ts", "src/x/y/e.ts"},
		"**/*.{ts,tsx}":             {".github/w.ts", "a.ts", "lib/f.ts", "src/b.ts", "src/x/y/c.tsx", "src/x/y/e.ts"},
		"*.{ts,tsx}":                {".github/w.ts", "a.ts", "lib/f.ts", "src/b.ts", "src/x/y/c.tsx", "src/x/y/e.ts"},
		"*.ts":                      {".github/w.ts", "a.ts", "lib/f.ts", "src/b.ts", "src/x/y/e.ts"},
		"**/w.ts":                   {".github/w.ts"},
		"src/**.ts":                 {"src/b.ts"},
		"src/**":                    {"src/b.ts", "src/d.go", "src/q.js", "src/q.min.js", "src/x/y/c.tsx", "src/x/y/e.ts"},
		"**":                        globTree,
		"src/x/**/c.tsx":            {"src/x/y/c.tsx"},
		"src/{x/y,none}/*.tsx":      {"src/x/y/c.tsx"},
		"{src/**,lib}/*.ts":         {"lib/f.ts", "src/x/y/e.ts"},
		"{src,lib}/*.ts":            {"lib/f.ts", "src/b.ts"},
		"src/q{,.min}.js":           {"src/q.js", "src/q.min.js"},
		"{a,{b,zz}}.ts":             {"a.ts", "src/b.ts"},
		"[{]a}.txt":                 {"{a}.txt"},
		"{a[,]b,zz}.txt":            {"a,b.txt"},
		"[[]x].txt":                 {"[x].txt"},
		"a,b.txt":                   {"a,b.txt"},
		"a}b":                       {},
		"a.ts":                      {"a.ts"},
		"./src/*.ts":                {"src/b.ts"},
		"././src/*.ts":              {"src/b.ts"},
		`src\**\*.ts`:               {"src/b.ts", "src/x/y/e.ts"},
		`src\x\y\c.tsx`:             {"src/x/y/c.tsx"},
		`src/\*.ts`:                 {},
		`src/\{b,c}.ts`:             {},
		"?.md":                      {"é.md"},
		"src/*":                     {"src/b.ts", "src/d.go", "src/q.js", "src/q.min.js"},
		"src/?/*/*":                 {"src/x/y/c.tsx", "src/x/y/e.ts"},
		"[a-b].ts":                  {"a.ts", "src/b.ts"},
		"[^a].ts":                   {".github/w.ts", "lib/f.ts", "src/b.ts", "src/x/y/e.ts"},
		"[!a].ts":                   {"a.ts"},
		"a{}.ts":                    {"a.ts"},
		strings.Repeat("{a,b}", 30): {},
	}
	for pattern, want := range cases {
		got, err := globbed(t, tool, pattern)
		if err != nil {
			t.Errorf("%q: refused: %v", pattern, err)
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q:\n got %q\nwant %q", pattern, got, want)
		}
	}
}

func TestGlobRefusesAMalformedPattern(t *testing.T) {
	tool := globTool(t)
	cases := map[string]string{
		"*.{ts":    "brace at byte 2 is never closed",
		"{a,{b}":   "brace at byte 0 is never closed",
		"src/[a":   "class at byte 4 is never closed",
		"[]":       "class at byte 0 is empty",
		`src/a\`:   "backslash at byte 5 escapes nothing",
		"src/[a-]": "class at byte 4",
		"   ":      "pattern is required",
	}
	for pattern, want := range cases {
		_, err := globbed(t, tool, pattern)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want a refusal holding %q, got %v", pattern, want, err)
		}
	}
}

func TestGlobKeepsWhatPathMatchMatched(t *testing.T) {
	tool := globTool(t)
	for _, pattern := range []string{
		"*", "*.ts", "src/*", "s?c/*.go", "[a-c]*", "[^a]*", "*/*/*", "src/x/*/c.tsx", "a,b.txt",
		"*.?s", "[!a]*", "a}b", "é*", "*.t[s-x]", "src/[d-e].*", "*,*", "*]*",
	} {
		var want []string
		for _, rel := range globTree {
			full, err := path.Match(pattern, rel)
			if err != nil {
				t.Fatalf("%q is not a path.Match pattern: %v", pattern, err)
			}
			base, _ := path.Match(pattern, path.Base(rel))
			if full || base {
				want = append(want, rel)
			}
		}
		got, err := globbed(t, tool, pattern)
		if err != nil {
			t.Errorf("%q: refused: %v", pattern, err)
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q:\n got %q\nwant %q", pattern, got, want)
		}
	}
}
