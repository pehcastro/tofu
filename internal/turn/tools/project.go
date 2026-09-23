package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/turn"
)

type Project struct {
	root turn.Root
}

func NewProject(dir string) (Project, error) {
	root, err := turn.NewRoot(dir)
	return Project{root: root}, err
}

func (p Project) Name() string { return "project_report" }

func (p Project) Definition() llm.Tool {
	return llm.Tool{
		Name: p.Name(),
		Description: "answers what is this repository in one call, and it is the first thing to reach for when you do not know the project yet. " +
			"it returns how many files there are and how many bytes they hold, the languages by file count, the top level directories by file count, " +
			"the entry points and manifests the project is built and started from, and its documentation files. " +
			ignoredWalkDescription + ". " +
			"it costs one walk: never assemble this from find, ls -R or wc, which descend into every ignored directory and take minutes on a tree this one walks in milliseconds. " +
			"after it, glob lists files by name, search finds text and returns the whole declaration around a match, and read reads one file whole",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":           map[string]any{"type": "integer", "description": "how many lines each section lists, " + strconv.Itoa(konst.ProjectSectionCap) + " by default, and it scales every section at once"},
				"include_ignored": map[string]any{"type": "boolean"},
			},
		},
	}
}

type projectArgs struct {
	Limit          int  `json:"limit"`
	IncludeIgnored bool `json:"include_ignored"`
}

func (p Project) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args projectArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return turn.Result{}, fmt.Errorf("project_report: arguments are not the expected shape: %w", err)
		}
	}
	if args.Limit < 0 {
		return turn.Result{}, fmt.Errorf("project_report: limit is %d and a section lists at least one line", args.Limit)
	}
	limit := cmp.Or(args.Limit, konst.ProjectSectionCap)
	listed, err := filesUnder(p.root, ".", args.IncludeIgnored)
	if err != nil {
		return turn.Result{}, fmt.Errorf("project_report: %w", err)
	}

	languages, roots := map[string]int{}, map[string]int{}
	var entries, docs []string
	for _, rel := range listed.files {
		languages[languageOf(rel)]++
		roots[rootOf(rel)]++
		if isEntryPoint(rel) {
			entries = append(entries, rel)
		}
		if isDocumentation(rel) {
			docs = append(docs, rel)
		}
	}

	slices.SortFunc(entries, shallowestFirst)
	slices.SortFunc(docs, shallowestFirst)

	var out strings.Builder
	fmt.Fprintf(&out, "project report for %s\n%d files holding %s, from one ignore-aware walk\n",
		string(p.root), len(listed.files), sizeText(listed.bytes))
	var cut []string
	for _, part := range []struct {
		title string
		lines []string
	}{
		{"languages", counted(languages)},
		{"roots", counted(roots)},
		{"entry points", entries},
		{"documentation", docs},
	} {
		if len(part.lines) > limit {
			cut = append(cut, fmt.Sprintf("%s has %d more", part.title, len(part.lines)-limit))
			part.lines = part.lines[:limit]
		}
		if len(part.lines) == 0 {
			part.lines = []string{"none"}
		}
		fmt.Fprintf(&out, "\n%s\n  %s\n", part.title, strings.Join(part.lines, "\n  "))
	}
	out.WriteString("\nnext: glob lists files by name, search finds text and returns the declaration around a match, read reads one file whole\n")

	note := listed.note
	if len(cut) > 0 {
		note = strings.TrimSpace(note + " " + search.Note(search.Truncated,
			"limit is "+strconv.Itoa(limit)+" and "+strings.Join(cut, ", ")+": raise limit, or glob the one you want in full"))
	}
	return turn.Result{Content: withNote(out.String(), note), Command: "project_report"}, nil
}

func shallowestFirst(left, right string) int {
	fixture := func(rel string) int {
		if slices.Contains(strings.Split(rel, "/"), "testdata") {
			return 1
		}
		return 0
	}
	return cmp.Or(
		cmp.Compare(fixture(left), fixture(right)),
		cmp.Compare(strings.Count(left, "/"), strings.Count(right, "/")),
		cmp.Compare(left, right))
}

func counted(counts map[string]int) []string {
	names := slices.Collect(maps.Keys(counts))
	slices.SortFunc(names, func(left, right string) int {
		return cmp.Or(cmp.Compare(counts[right], counts[left]), cmp.Compare(left, right))
	})
	lines := make([]string, len(names))
	for i, name := range names {
		files := " files"
		if counts[name] == 1 {
			files = " file"
		}
		lines[i] = name + " " + strconv.Itoa(counts[name]) + files
	}
	return lines
}

func languageOf(rel string) string {
	name := path.Base(rel)
	extension := strings.ToLower(path.Ext(name))
	if extension == "" || extension == name {
		return "no extension"
	}
	switch extension {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java", ".kt":
		return "jvm"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".hpp":
		return "c++"
	case ".cs":
		return "c sharp"
	case ".sh", ".bash":
		return "shell"
	case ".ps1", ".psm1":
		return "powershell"
	case ".sql":
		return "sql"
	case ".html", ".css":
		return "web"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	}
	return strings.TrimPrefix(extension, ".")
}

func rootOf(rel string) string {
	if top, _, nested := strings.Cut(rel, "/"); nested {
		return top + "/"
	}
	return "the top level"
}

func isEntryPoint(rel string) bool {
	switch path.Base(rel) {
	case "main.go", "main.rs", "main.py", "__main__.py", "index.ts", "index.js",
		"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "setup.py",
		"Makefile", "Dockerfile", "CMakeLists.txt", "Gemfile", "pom.xml", "build.gradle":
		return true
	}
	return false
}

func isDocumentation(rel string) bool {
	name := path.Base(rel)
	switch name {
	case "README.md", "AGENTS.md", "CLAUDE.md", "CHANGELOG.md", "LICENSE":
		return true
	}
	return strings.HasSuffix(name, ".md") && !strings.Contains(rel, "/")
}

func sizeText(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", size)
}
