package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const recordedPRDiff = `diff --git a/internal/foo.go b/internal/foo.go
index 83db48f..bf269c4 100644
--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,3 +1,4 @@
 package foo

+// added line
 func Foo() {}
`

func plantGH(t *testing.T, exitCode int, stdout, stderr string) {
	t.Helper()
	running, err := os.Executable()
	if err != nil {
		t.Fatalf("finding the running test binary: %v", err)
	}
	body, err := os.ReadFile(running)
	if err != nil {
		t.Fatalf("reading the running test binary: %v", err)
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name = "gh.exe"
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o700); err != nil {
		t.Fatalf("planting a stand-in gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(standInExitEnvar, strconv.Itoa(exitCode))
	t.Setenv(standInStdoutEnvar, stdout)
	t.Setenv(standInMessageEnvar, stderr)
}

func TestGitHubPRDiffRefusesWhenGhIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tool, err := tools.NewGitHubPRDiff(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected a refusal when gh is not on PATH")
	}
	t.Logf("refusal: %v", err)
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("the refusal does not name the missing binary as the cause: %v", err)
	}
}

func TestGitHubPRDiffRefusesWhenGhIsNotAuthenticated(t *testing.T) {
	plantGH(t, 1, "", "gh: To authenticate, run gh auth login\n")
	tool, err := tools.NewGitHubPRDiff(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Run(context.Background(), json.RawMessage(`{"pr":"42"}`))
	if err == nil {
		t.Fatal("expected a refusal when gh is not authenticated")
	}
	t.Logf("refusal: %v", err)
	if !strings.Contains(err.Error(), "not authenticated") || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("the refusal does not name authentication as the cause: %v", err)
	}
}

func TestGitHubPRDiffReturnsTheDiffOfAPullRequestNamedByNumber(t *testing.T) {
	plantGH(t, 0, recordedPRDiff, "")
	tool, err := tools.NewGitHubPRDiff(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"pr":"42"}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	t.Logf("result: %s", result.Content)
	if !strings.Contains(result.Content, recordedPRDiff) {
		t.Fatalf("the recorded diff did not reach the model: %q", result.Content)
	}
	if result.Command != "gh pr diff --color never 42" {
		t.Fatalf("the command did not carry the pull request's number: %q", result.Command)
	}
}

func TestGitHubPRDiffResolvesAPullRequestNamedByURL(t *testing.T) {
	plantGH(t, 0, recordedPRDiff, "")
	tool, err := tools.NewGitHubPRDiff(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	const url = "https://github.com/owner/repo/pull/42"
	result, err := tool.Run(context.Background(), json.RawMessage(`{"pr":"`+url+`"}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	if result.Command != "gh pr diff --color never "+url {
		t.Fatalf("the command did not carry the pull request's url: %q", result.Command)
	}
}

func TestGitHubPRDiffResolvesToTheCurrentBranchWhenNoPullRequestIsNamed(t *testing.T) {
	plantGH(t, 0, recordedPRDiff, "")
	tool, err := tools.NewGitHubPRDiff(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	if result.Command != "gh pr diff --color never" {
		t.Fatalf("naming no pull request must fall back to the current branch, got %q", result.Command)
	}
}

func TestADiffOverTheResultCapGoesToTheArtifactStoreAndTheModelIsTold(t *testing.T) {
	big := recordedPRDiff + strings.Repeat("+extra line that pads the diff out past the cap\n", 400)
	plantGH(t, 0, big, "")
	tool, err := tools.NewGitHubPRDiff(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	model := &watchingModel{scripted: scriptedModel{calls: []llm.ToolCall{
		{ID: "c1", Name: "github_pr_diff", Arguments: json.RawMessage(`{"pr":"42"}`)},
	}}}
	called := loggedRows(t, runWeb(t, model, []turn.Tool{tool}, 4096, nil))
	if len(called) != 1 || called[0].Error != "" {
		t.Fatalf("the tool call did not run: %+v", called)
	}
	t.Logf("result %d bytes rendered as %d with handle %q",
		called[0].ResultBytes, called[0].RenderedBytes, called[0].ResultHandle)
	if called[0].ResultHandle == "" {
		t.Fatal("a diff over the result cap was cut rather than stored whole")
	}
	if !strings.Contains(model.toolResult(t), "artifact "+called[0].ResultHandle) {
		t.Fatal("the model was not told the handle that holds the diff")
	}
}
