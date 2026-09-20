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

	"boji/internal/konst"
	"boji/internal/turn"
	"boji/internal/turn/tools"
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
	for _, phrase := range []string{"whole declaration", "comment", "string literal", "fallback", "token budget"} {
		if !strings.Contains(definition.Description, phrase) {
			t.Fatalf("the description does not tell the model about %q: %s", phrase, definition.Description)
		}
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
			t.Skip("this measurement needs the boji repository and go.mod is not above the test")
		}
		at = parent
	}
}

func TestSearchAgainstGrepOnRecordedQuestions(t *testing.T) {
	root := repositoryRoot(t)
	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}

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
		grepped, err := grepTool.Run(context.Background(), args)
		if err != nil {
			t.Fatalf("grep %s: %v", question.pattern, err)
		}
		searched := searchResult(t, root, string(args))
		grepTokens := len(grepped.Content) / konst.SearchBytesPerToken
		searchTokens := len(searched.Content) / konst.SearchBytesPerToken
		t.Logf("%s under %s: grep %d tokens, search %d tokens", question.pattern, question.path, grepTokens, searchTokens)
		if searchTokens > konst.SearchTokenBudget {
			t.Fatalf("search returned about %d tokens, over its own budget of %d", searchTokens, konst.SearchTokenBudget)
		}
	}
}
