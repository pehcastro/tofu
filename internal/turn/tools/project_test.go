package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/turn"
)

func writeTree(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func projectTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, t.TempDir(), map[string]string{
		".gitignore":                  "node_modules/\ndist/\n",
		"README.md":                   "the readme",
		"CHANGELOG.md":                "the changelog",
		"go.mod":                      "module example\n",
		"cmd/example/main.go":         "package main\n\nfunc main() {}\n",
		"internal/engine/run.go":      "package engine\n",
		"internal/engine/run_test.go": "package engine\n",
		"web/index.ts":                "export const x = 1\n",
		"web/style.css":               "body {}\n",
		"docs/deep/design.md":         "a nested note",
		"node_modules/left/pad.js":    "module.exports = 1\n",
		"dist/bundle.js":              "bundled\n",
	})
}

func runProject(t *testing.T, root, args string) string {
	t.Helper()
	tool, err := NewProject(root)
	if err != nil {
		t.Fatalf("building project_report: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("running project_report: %v", err)
	}
	return result.Content
}

func TestProjectReportNamesTheLanguagesTheRootsTheEntryPointsAndTheDocumentation(t *testing.T) {
	content := runProject(t, projectTree(t), `{}`)

	for _, want := range []string{
		"languages", "go 3 files", "typescript 1 file", "markdown 3 files",
		"roots", "internal/ 2 files", "the top level 4 files",
		"entry points", "cmd/example/main.go", "go.mod",
		"documentation", "README.md", "CHANGELOG.md",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("the report never says %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"node_modules", "dist/bundle.js"} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("the report reached an ignored path %q:\n%s", unwanted, content)
		}
	}
	t.Log("\n" + content)
}

func TestTheProjectReportDefinitionTellsTheModelNotToAssembleThisFromFind(t *testing.T) {
	tool, err := NewProject(t.TempDir())
	if err != nil {
		t.Fatalf("building project_report: %v", err)
	}
	definition := tool.Definition()
	if tool.Name() != "project_report" || definition.Name != tool.Name() {
		t.Fatalf("the tool is named %q and declares itself %q", tool.Name(), definition.Name)
	}
	for _, want := range []string{"what is this repository", "never assemble this from find", "ls -R", "glob", "read"} {
		if !strings.Contains(definition.Description, want) {
			t.Fatalf("the definition never says %q:\n%s", want, definition.Description)
		}
	}
}

func TestProjectReportSaysWhenALimitCutASectionRatherThanLookingComplete(t *testing.T) {
	content := runProject(t, projectTree(t), `{"limit":1}`)

	for _, want := range []string{"degraded truncated", "limit is 1", "languages has", "raise limit"} {
		if !strings.Contains(content, want) {
			t.Fatalf("a cut section is presented as a complete answer:\n%s", content)
		}
	}
}

func TestACappedSectionKeepsTheEntryPointsAtTheTopOfTheTreeAndCutsTheFixturesUnderIt(t *testing.T) {
	root := writeTree(t, t.TempDir(), map[string]string{
		"go.mod":                          "module example\n",
		"cmd/example/main.go":             "package main\n",
		"bench/cost/testdata/one/main.go": "package main\n",
		"bench/cost/testdata/two/main.go": "package main\n",
		"bench/seed/src/index.ts":         "export const x = 1\n",
		"bench/seed/package.json":         "{}\n",
	})
	content := runProject(t, root, `{"limit":3}`)
	entries, _, _ := strings.Cut(strings.SplitN(content, "entry points\n", 2)[1], "\ndocumentation")

	for _, want := range []string{"go.mod", "cmd/example/main.go"} {
		if !strings.Contains(entries, want) {
			t.Fatalf("a cap cut %q, the entry point a caller asks about:\n%s", want, content)
		}
	}
	if strings.Contains(entries, "testdata") {
		t.Fatalf("a fixture under testdata took a capped slot:\n%s", content)
	}
	t.Log("\n" + content)
}

func TestProjectReportAgainstTheTwoFindCommandsFromTheRecordedRun(t *testing.T) {
	if os.Getenv("TOFU_MEASURE_FIND") == "" {
		t.Skip("the find arms walk every ignored directory and take minutes, so they run only under TOFU_MEASURE_FIND")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	shell, err := turn.NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	report, err := NewProject(root)
	if err != nil {
		t.Fatalf("building project_report: %v", err)
	}

	for _, arm := range []struct{ name, args string }{
		{"project_report", ""},
		{"find step 2", `{"command":"find bench cmd interface library internal -type d | sort; echo ---; find . -name '*.go' -not -path './.local/*' | wc -l","timeout_ms":600000}`},
		{"find step 7", `{"command":"find . -name '*.go' -not -path './.local/*' -not -name '*_test.go' | xargs wc -l | tail -1","timeout_ms":600000}`},
	} {
		started := time.Now()
		var content string
		if arm.args == "" {
			result, runErr := report.Run(context.Background(), json.RawMessage(`{}`))
			if runErr != nil {
				t.Fatalf("%s: %v", arm.name, runErr)
			}
			content = result.Content
		} else {
			result, runErr := shell.Run(context.Background(), json.RawMessage(arm.args))
			if runErr != nil {
				t.Fatalf("%s: %v", arm.name, runErr)
			}
			content = result.Content
		}
		t.Logf("%s: %v wall clock, %d result bytes, about %d result tokens",
			arm.name, time.Since(started).Round(time.Millisecond), len(content), len(content)/konst.SearchBytesPerToken)
	}
}

func TestProjectReportReadsTheWholeRepositoryInUnderASecond(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("the repository root is not at %s: %v", root, err)
	}
	tool, err := NewProject(root)
	if err != nil {
		t.Fatalf("building project_report: %v", err)
	}

	started := time.Now()
	result, err := tool.Run(context.Background(), json.RawMessage(`{}`))
	took := time.Since(started)
	if err != nil {
		t.Fatalf("running project_report on the repository: %v", err)
	}

	if took > time.Second {
		t.Fatalf("project_report took %v on %s", took, root)
	}
	for _, want := range []string{"go ", "entry points", "documentation", "cmd/tofu/main.go"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the report of the real repository never says %q:\n%s", want, result.Content)
		}
	}
	t.Logf("project_report on %s took %v and returned %d bytes\n%s", root, took, len(result.Content), result.Content)
}
