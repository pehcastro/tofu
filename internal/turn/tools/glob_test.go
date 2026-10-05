package tools

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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
