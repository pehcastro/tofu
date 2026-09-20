package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/turn/tools"
)

const serviceFile = `package auth

func authenticate(user string) bool {
	return verifyPassword(user) && createSession(user)
}

func verifyPassword(string) bool { return true }

func createSession(string) bool { return true }
`

const loginFile = `package auth

func login(user string) bool {
	return authenticate(user)
}
`

func TestSymbolsAnswersWhereAThingIsDeclaredWhatItCallsAndWhoCallsIt(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "auth/service.go", serviceFile)
	seed(t, root, "auth/login.go", loginFile)
	seed(t, root, "auth/broken.go", "package auth\n\nfunc (\n")

	row := runCalls(t, root, konst.TurnResultBytesCap, false,
		llm.ToolCall{ID: "c1", Name: "symbols", Arguments: json.RawMessage(`{"name":"authenticate"}`)})
	called := loggedRows(t, row)[0]
	if called.Error != "" {
		t.Fatalf("symbols through the registry: %s", called.Error)
	}

	tool, err := tools.NewSymbols(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"name":"authenticate"}`))
	if err != nil {
		t.Fatalf("symbols: %v", err)
	}
	t.Logf("symbols authenticate:\n%s", result.Content)
	for _, fact := range []string{
		"defined auth/service.go:3-5 func authenticate",
		"calls createSession, verifyPassword",
		"auth/login.go:4 in login",
		"degraded name_matched",
		"degraded fallback",
	} {
		if !strings.Contains(result.Content, fact) {
			t.Fatalf("the answer does not carry %q:\n%s", fact, result.Content)
		}
	}
}

func TestSymbolsSaysNothingDeclaresItRatherThanFailing(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "auth/service.go", serviceFile)
	tool, _ := tools.NewSymbols(root)

	result, err := tool.Run(context.Background(), json.RawMessage(`{"name":"absent"}`))
	if err != nil {
		t.Fatalf("an identifier nothing declares is an answer, not an error: %v", err)
	}
	if !strings.Contains(result.Content, "not a failure") {
		t.Fatalf("the empty answer does not say it is an answer:\n%s", result.Content)
	}

	if _, err := tool.Run(context.Background(), json.RawMessage(`{"name":"func main("}`)); err == nil {
		t.Fatal("a name that is not an identifier must be refused rather than searched for")
	}
}
