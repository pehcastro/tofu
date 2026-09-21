package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func searchResult(t *testing.T, root, args string) turn.Result {
	t.Helper()
	tool, err := tools.NewSearch(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("search %s: %v", args, err)
	}
	return result
}

func markedFunctions(t *testing.T, root string, files, each int) {
	t.Helper()
	for file := range files {
		var body strings.Builder
		body.WriteString("package marked\n")
		for function := range each {
			fmt.Fprintf(&body, "\nfunc Step%d%d() int {\n\treturn needleMarker%d%d\n}\n", file, function, file, function)
		}
		seed(t, root, fmt.Sprintf("pkg%d/marked.go", file), body.String())
	}
}

func TestFiftyMatchesNeverReachTheModelAsCandidateLines(t *testing.T) {
	root := t.TempDir()
	markedFunctions(t, root, 10, 5)

	result := searchResult(t, root, `{"pattern":"needleMarker","max_tokens":400}`)

	grepShaped := regexp.MustCompile(`(?m)^[^\s:]+\.go:\d+:`)
	if grepShaped.MatchString(result.Content) {
		t.Fatalf("a candidate line reached the result as path:line:text:\n%s", result.Content)
	}
	markers := strings.Count(result.Content, "needleMarker")
	if markers >= 50 {
		t.Fatalf("all 50 candidates were handed over, the budget selected nothing: %d markers", markers)
	}
	if !strings.Contains(result.Content, "50 text candidates") {
		t.Fatalf("the result does not say how many candidates it started from:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "func Step") {
		t.Fatalf("the result returned no whole function:\n%s", result.Content)
	}
	if spent := len(result.Content) / konst.SearchBytesPerToken; spent > 400+konst.SearchResultOverhead {
		t.Fatalf("the result is about %d tokens against a budget of 400", spent)
	}
}

func TestTheSearchToolIsRegistrableAndItsDescriptionSaysWhatItReturns(t *testing.T) {
	built, err := tools.NewSearch(t.TempDir())
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	var tool turn.Tool = built
	if tool.Name() != "search" {
		t.Fatalf("the registry would hold it under %q", tool.Name())
	}
	definition := tool.Definition()
	if definition.Name != tool.Name() {
		t.Fatalf("the model would call %q and the registry would look up %q", definition.Name, tool.Name())
	}
	for _, phrase := range []string{
		"whole declaration", "comment", "string literal", "fallback", "token budget",
		"finds text anywhere", "there is no grep tool",
	} {
		if !strings.Contains(definition.Description, phrase) {
			t.Fatalf("the description does not tell the model about %q: %s", phrase, definition.Description)
		}
	}
}

func TestSearchUnderASubdirectoryIgnoreFileNeverReadsTheIgnoredCopy(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", "vendored/\n")
	seed(t, root, "src/app.go", "package src\n\nconst needleHere = 1\n")
	seed(t, root, "vendored/copy.go", "package vendored\n\nconst needleHere = 2\n")

	found := searchResult(t, root, `{"pattern":"needleHere"}`)
	if strings.Contains(found.Content, "vendored/copy.go") {
		t.Fatalf("search read a file the .gitignore excludes:\n%s", found.Content)
	}
	if !strings.Contains(found.Content, "src/app.go") {
		t.Fatalf("search missed the one file it was supposed to read:\n%s", found.Content)
	}

	unfiltered := searchResult(t, root, `{"pattern":"needleHere","include_ignored":true}`)
	if !strings.Contains(unfiltered.Content, "vendored/copy.go") || !strings.Contains(unfiltered.Content, "degraded unfiltered") {
		t.Fatalf("the override did not reach the ignored file or did not say so:\n%s", unfiltered.Content)
	}
}

func TestAnAgentCanSearchThisRepositoryForItsOwnInstructions(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Skip("CLAUDE.md is itself gitignored here, so a clone does not carry one to find")
	}
	found := searchResult(t, root, `{"pattern":"No ticket, no agent"}`)
	if !strings.Contains(found.Content, "CLAUDE.md:") {
		t.Fatalf("line 11 of the root .gitignore hid the project's own rules from search:\n%s", found.Content)
	}
}

func TestABinaryFileIsNamedAsSkippedBySearchAndRefusedByEdit(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "notes.txt", "needleHere\n")
	seed(t, root, "blob.bin", "needleHere\x00tail\n")

	found := searchResult(t, root, `{"pattern":"needleHere"}`)
	for _, want := range []string{"degraded binary_skipped", "null byte", "not a complete answer"} {
		if !strings.Contains(found.Content, want) {
			t.Fatalf("search never says %q about the file it did not read:\n%s", want, found.Content)
		}
	}

	editTool, err := tools.NewEdit(root)
	if err != nil {
		t.Fatalf("building edit: %v", err)
	}
	_, err = editTool.Run(context.Background(), json.RawMessage(`{"path":"blob.bin","old_string":"needleHere","new_string":"other"}`))
	if err == nil || !strings.Contains(err.Error(), "degraded binary_skipped") {
		t.Fatalf("edit rewrote a binary file or did not name why it refused: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	at, err := os.Getwd()
	if err != nil {
		t.Fatalf("finding the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(at, "go.mod")); err == nil {
			return at
		}
		parent := filepath.Dir(at)
		if parent == at {
			t.Skip("this measurement needs the tofu repository and go.mod is not above the test")
		}
		at = parent
	}
}

func TestSearchStaysUnderItsBudgetOnRecordedQuestions(t *testing.T) {
	root := repositoryRoot(t)
	questions := []struct{ pattern, path string }{
		{`t\.Skip|t\.Skipf`, "internal/judge"},
		{`t\.Skip`, "internal/judge"},
		{`TODO|FIXME|XXX|HACK`, "internal/judge"},
		{`read_worth`, "cmd"},
		{`tool_gate@2|read_worth@1`, "cmd"},
	}
	for _, question := range questions {
		args, err := json.Marshal(map[string]string{"pattern": question.pattern, "path": question.path})
		if err != nil {
			t.Fatalf("encoding the question: %v", err)
		}
		searched := searchResult(t, root, string(args))
		searchTokens := len(searched.Content) / konst.SearchBytesPerToken
		t.Logf("%s under %s: search %d tokens", question.pattern, question.path, searchTokens)
		if searchTokens > konst.SearchTokenBudget {
			t.Fatalf("search returned about %d tokens, over its own budget of %d", searchTokens, konst.SearchTokenBudget)
		}
	}
}
