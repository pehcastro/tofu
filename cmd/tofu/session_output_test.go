package main

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/golden"
	"tofu/internal/konst"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

func sessionOutputCases() []outputCase {
	return []outputCase{
		{"session-list", []string{"session", "list"}, exitOK},
		{"session-info", []string{"session", "info", "turn-one"}, exitOK},
		{"session-trace", []string{"session", "trace", "turn-one"}, exitOK},
		{"session-reads", []string{"session", "reads", "turn-one"}, exitOK},
		{"session-resume", []string{"session", "resume", "turn-one"}, exitUsage},
		{"session-rename", []string{"session", "rename", "turn-two", "older work"}, exitOK},
		{"session-missing", []string{"session", "info", "nope"}, exitVerdict},
		{"session-usage", []string{"session", "info"}, exitUsage},
		{"session-family", []string{"session", "store-walk"}, exitOK},
		{"session-family-missing", []string{"session", "tree"}, exitVerdict},
		{"session-find", []string{"session", "find", "store-walk", "--tool", "read"}, exitOK},
		{"session-find-usage", []string{"session", "find", "store-walk", "--colour", "red"}, exitUsage},
		{"continue", []string{"--continue"}, exitUsage},
		{"context", []string{"context"}, exitOK},
		{"context-missing", []string{"context", "nope"}, exitVerdict},
		{"shells-list", []string{"shells", "list"}, exitOK},
		{"shells-log", []string{"shells", "log", "build"}, exitOK},
		{"shells-stop-refused", []string{"shells", "stop", "build"}, exitVerdict},
		{"shells-restart-refused", []string{"shells", "restart", "nope"}, exitVerdict},
		{"shells-usage", []string{"shells", "log"}, exitUsage},
	}
}

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sessionOutputProject(t *testing.T) string {
	t.Helper()
	isolateHome(t)
	home := os.Getenv("USERPROFILE")
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	store, err := session.Open()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	step := turn.StepRow{Index: 1, AssistantText: "looking at the store", Model: "claude-opus-5", PromptTokens: 1200, CompletionTokens: 300, CostUSD: 0.0042,
		Occupancy: &recall.Occupancy{Identity: 3300, Facts: 1200, WorkingSet: 9000, Recent: 4000, Target: 50000}}
	storeWalk, olderTask := "store-walk", "older-task"
	one := session.Header{ID: "turn-one", Name: &storeWalk, At: now.Add(-10 * time.Minute), Task: "explain the session store", Root: "turn-one",
		Wire: wireSubscription, Model: "claude-opus-5", Outcome: "stopped", CostUSD: 0.0054,
		Agents: []session.AgentRun{{Agent: "agent-1", Definition: "planner", Model: "claude-sub/claude-opus-5", SpawnCall: "call-2", SpawnTurn: "turn-one", Status: "done", CostUSD: 0.0012}}}
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes-10) * time.Minute) }
	events := []session.Event{
		{ID: "ev-task", At: at(0), Kind: session.EventMessage, Body: rawJSON(t, turn.MessageRow{Role: "user", Content: "explain the session store"})},
		{ID: "req-1", At: at(1), Turn: "turn-one", Kind: session.EventStep, Body: rawJSON(t, step)},
		{ID: "ev-call-1", At: at(2), Turn: "turn-one", Call: "call-1", Request: "req-1", Kind: session.EventToolCall,
			Body: rawJSON(t, session.CallBody{Tool: "read", Args: json.RawMessage(`{"path":"internal/session/store.go"}`)})},
		{ID: "res-1", At: at(3), Turn: "turn-one", Call: "call-1", Kind: session.EventToolResult, Body: rawJSON(t, session.ResultBody{ToolOutcome: "ran", ResultBytes: 2048})},
		{ID: "ev-call-3", At: at(4), Turn: "turn-one", Agent: "agent-1", Call: "call-3", Kind: session.EventToolCall, Body: rawJSON(t, session.CallBody{Tool: "search"})},
		{ID: "ev-answer", At: at(5), Kind: session.EventMessage, Body: rawJSON(t, turn.MessageRow{Role: "assistant", Content: "it keeps one log per session"})},
	}
	two := session.Header{ID: "turn-two", Name: &olderTask, At: now.Add(-3 * time.Hour), Task: "the older task", Root: "turn-two", Outcome: "done"}
	for _, err := range []error{store.Write(one, events), store.Write(two, nil), store.SetHead("turn-one")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := sys.WriteFile(filepath.Join(store.Dir("turn-broken"), "header.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := sys.ProjectStateDir()
	if err != nil {
		t.Fatal(err)
	}
	exit := 0
	ended := now.Add(-time.Minute)
	for _, entry := range []shell.Shell{
		{Name: "build", Command: "go build ./...", Dir: project, PID: 4242, State: shell.Exited, Started: now.Add(-2 * time.Minute), Ended: &ended, ExitCode: &exit},
		{Name: "web", Command: "npm run dev", Dir: project, Owner: "tofu", PID: 4343, State: shell.Killed, Started: now.Add(-time.Minute), Ended: &ended},
	} {
		if err := sys.WriteFile(filepath.Join(state, "shells", entry.Name+".json"), rawJSON(t, entry), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	built := filepath.Join(state, "shells", "build.log")
	printedAt := now.Add(-90 * time.Second)
	if err := cmp.Or(sys.WriteFile(built, []byte("compiling\ndone\n"), 0o644), os.Chtimes(built, printedAt, printedAt)); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestSessionContextAndShellsMatchTheirTextAndJSONGoldens(t *testing.T) {
	stamp := regexp.MustCompile(`"(at|started_at|ended_at|started|ended|last_output)": "[^"]*"`)
	homeValue := regexp.MustCompile(`HOME[^"]*`)
	clock := regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}\b`)
	printed := map[string]string{}
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			escapedHome, _ := json.Marshal(sessionOutputProject(t))
			for _, c := range sessionOutputCases() {
				asJSON := mode == "json" && !strings.HasSuffix(c.name, "usage")
				args, code := c.args, c.code
				if asJSON {
					args = append(append([]string{}, args...), jsonFlag)
				}
				if asJSON && (c.name == "continue" || c.name == "session-resume") {
					code = exitOK
				}
				got, out, errOut := runOutput(args)
				if got != code {
					t.Errorf("%s exited %d, want %d\nstdout %s\nstderr %s", c.name, got, code, out, errOut)
				}
				if asJSON {
					if !json.Valid([]byte(out)) || errOut != "" {
						t.Errorf("%s --json wrote stdout that is not one document, or wrote stderr %q:\n%s", c.name, errOut, out)
					}
					out = strings.ReplaceAll(out, strings.Trim(string(escapedHome), `"`), "HOME")
					out = homeValue.ReplaceAllStringFunc(out, func(path string) string { return strings.ReplaceAll(path, `\\`, "/") })
					out = strings.ReplaceAll(stamp.ReplaceAllString(out, `"$1": "AT"`), konst.Version, "VERSION")
				}
				out = clock.ReplaceAllString(out, "HH:MM:SS")
				printed["session-output/"+c.name+"."+mode+".golden"] = "exit " + strconv.Itoa(got) + "\n--- stdout\n" + out + "--- stderr\n" + errOut
			}
		})
	}
	for name, text := range printed {
		golden.Assert(t, name, text)
	}
}

func TestSessionContextAndShellsWriteNoEscapeUnderNoColour(t *testing.T) {
	for _, noColour := range []bool{false, true} {
		sessionOutputProject(t)
		t.Setenv("CLICOLOR_FORCE", "1")
		t.Setenv("NO_COLOR", map[bool]string{true: "1"}[noColour])
		escaped := 0
		for _, c := range sessionOutputCases() {
			_, out, errOut := runOutput(c.args)
			if strings.ContainsRune(out+errOut, 0x1b) {
				escaped++
			}
		}
		if noColour && escaped > 0 {
			t.Errorf("with NO_COLOR, %d verbs wrote an ESC byte", escaped)
		}
		if !noColour && escaped != len(sessionOutputCases()) {
			t.Errorf("with colour forced, %d of %d verbs wrote an ESC byte, so the NO_COLOR run proves nothing", escaped, len(sessionOutputCases()))
		}
	}
}
