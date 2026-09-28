package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/sys"
)

const (
	storedForRedaction = "sk-or-v1-made-up-stored-for-redaction-4Hq8"
	looseForRedaction  = "sk-or-v1-made-up-only-in-a-file-2Wm5"
)

func keyedTurn(t *testing.T, command string) (row Row, seen string, recorded map[string]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv(sys.OpenRouterKeyName, "")
	if err := sys.SaveKey(sys.OpenRouterKeyName, storedForRedaction); err != nil {
		t.Fatalf("storing the key: %v", err)
	}
	tool, root := bashRoot(t)
	for name, body := range map[string]string{"secret.txt": storedForRedaction + "\n", "dotenv.txt": sys.OpenRouterKeyName + "=" + looseForRedaction + "\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sessionDir := t.TempDir()
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: bashToolName, Arguments: json.RawMessage(`{"command":` + strconv.Quote(command) + `}`)}),
		messageDecision(),
	}}
	row, err := Run(context.Background(), Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(tool),
		Task:           "print the files",
		Caps:           Caps{MaxSteps: 4},
		ResultBytesCap: 4096,
		ArtifactDir:    t.TempDir(),
		Sessions:       session.NewStore(sessionDir),
	})
	if err != nil {
		t.Fatalf("the turn returned an error: %v", err)
	}
	if len(model.requests) < 2 {
		t.Fatalf("the model was asked %d times, want a second request carrying the result", len(model.requests))
	}
	for _, message := range model.requests[1].Messages {
		if message.Role == llm.RoleTool {
			seen += message.Content
		}
	}
	recorded = map[string]string{}
	err = filepath.WalkDir(sessionDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		recorded[path] = string(body)
		return err
	})
	if err != nil {
		t.Fatalf("reading the session back: %v", err)
	}
	return row, seen, recorded
}

func assertRedacted(t *testing.T, where, text string) {
	t.Helper()
	if !strings.Contains(text, sys.KeyRedactedMark) {
		t.Errorf("%s carries no %s", where, sys.KeyRedactedMark)
	}
	for _, key := range []string{storedForRedaction, looseForRedaction} {
		if strings.Contains(text, key) {
			t.Errorf("%s carries a key of length %d:\n%s", where, len(key), strings.ReplaceAll(text, key, "<THE KEY>"))
		}
	}
}

func TestABashResultThatPrintsAStoredKeyComesBackRedactedInTheResultAndTheSessionEvent(t *testing.T) {
	_, seen, recorded := keyedTurn(t, "cat secret.txt dotenv.txt")
	assertRedacted(t, "the result the model received", seen)
	var results string
	for _, body := range recorded {
		for line := range strings.SplitSeq(body, "\n") {
			if strings.Contains(line, `"`+string(session.EventToolResult)+`"`) {
				results += line + "\n"
			}
		}
	}
	assertRedacted(t, "the tool_result session event", results)
	t.Logf("the model received %q", seen)
}

func TestAKeyInABashCommandIsRedactedInEveryRecordedEvent(t *testing.T) {
	row, _, recorded := keyedTurn(t, "echo "+storedForRedaction+" && echo "+sys.OpenRouterKeyName+"="+looseForRedaction)
	var called string
	for _, step := range row.Steps {
		for _, call := range step.ToolCalls {
			called += call.Command + " " + string(call.Args) + "\n"
		}
	}
	assertRedacted(t, "the tool calls on the returned row", called)
	for path, body := range recorded {
		if strings.HasSuffix(path, "events.jsonl") {
			assertRedacted(t, path, body)
		}
		for _, key := range []string{storedForRedaction, looseForRedaction} {
			if strings.Contains(body, key) {
				t.Errorf("%s carries a key of length %d", path, len(key))
			}
		}
	}
}
