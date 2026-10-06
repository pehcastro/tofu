package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"tofu/internal/hook"
	"tofu/internal/llm"
	"tofu/internal/shell"
	"tofu/internal/sys"
)

func sessionHooks(t *testing.T, project string) string {
	t.Helper()
	if choice, err := shell.Resolve(""); err != nil || strings.Contains(choice.Label, "powershell") {
		t.Skip("no bash resolved on this machine")
	}
	logged := `{"type":"command","command":"cat >> session.log; echo >> session.log"}`
	settings := `{"hooks":{"SessionStart":[{"hooks":[` + logged + `]}],"SessionEnd":[{"hooks":[` + logged + `]}]}}`
	if err := sys.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := hook.Load(project, shell.Choice{})
	if err := engine.Answer(engine.Untrusted(), hook.AnswerAlways); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(project, "session.log")
}

func sessionEvents(t *testing.T, logged string) []string {
	t.Helper()
	body, err := os.ReadFile(logged)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var fired struct {
			Event   string `json:"hook_event_name"`
			Source  string `json:"source"`
			Reason  string `json:"reason"`
			Session string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(line), &fired); err != nil {
			t.Fatalf("%s holds a line that is not JSON: %q", logged, line)
		}
		seen = append(seen, fired.Event+" "+fired.Source+fired.Reason+" "+fired.Session)
	}
	return seen
}

func TestHooksRunFiresSessionStartFirstAndSessionEndWhenItsLeadReturns(t *testing.T) {
	isolateHome(t)
	project := t.TempDir()
	logged := sessionHooks(t, project)
	deck := filepath.Join(t.TempDir(), "deck.jsonl")
	if err := os.WriteFile(deck, []byte(`{"text":"nothing to do"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cassetteVariable, deck)
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", project, "--no-gate", "--sift", "free", "say hi"}, &out, &errOut); code != exitOK {
		t.Fatalf("tofu run exited %d: %s", code, errOut.String())
	}
	seen := sessionEvents(t, logged)
	start := regexp.MustCompile(`^SessionStart startup (\S+)$`)
	if len(seen) != 2 || !start.MatchString(seen[0]) || seen[1] != "SessionEnd other "+start.FindStringSubmatch(seen[0])[1] {
		t.Errorf("tofu run fired %q, want SessionStart startup, then SessionEnd other on the same session", seen)
	}
}

func TestHooksTheAppFiresSessionStartForStartupClearAndResumeAndSessionEndAtExit(t *testing.T) {
	logged := sessionHooks(t, scratchProject(t))
	dir, _ := os.Getwd()
	model := &sendModel{}
	for range 4 {
		model.queued = append(model.queued, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "seen"})
	}
	live := newAppSession(dir, func(runOpts) (appWire, error) { return wireOn(model), nil }, nil, time.Now, sessionResume{})
	var events eventLog
	live.run(t.Context(), onTheSubscription, "first", events.add)
	first := live.id
	live.run(t.Context(), onTheSubscription, "second, same session", events.add)
	live.startFresh()
	live.run(t.Context(), onTheSubscription, "after clear", events.add)
	live.resume(first)
	live.run(t.Context(), onTheSubscription, "after resume", events.add)
	live.end()
	want := []string{"SessionStart startup " + first, "SessionStart clear ", "SessionStart resume " + first, "SessionEnd prompt_input_exit " + first}
	seen := sessionEvents(t, logged)
	if len(seen) != len(want) {
		t.Fatalf("the app fired %q, want %q", seen, want)
	}
	for i := range want {
		if !strings.HasPrefix(seen[i], want[i]) {
			t.Errorf("event %d was %q, want it to start %q", i+1, seen[i], want[i])
		}
	}
}

func TestHooksListsEverySourceWithItsTrustAndTrustTrustsTheProjectsOnce(t *testing.T) {
	isolateHome(t)
	t.Setenv("NO_COLOR", "1")
	home, project := os.Getenv("USERPROFILE"), t.TempDir()
	t.Chdir(project)
	files := map[string]string{
		filepath.Join(home, ".claude", "settings.json"):          `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`,
		filepath.Join(project, ".claude", "settings.json"):       `{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo edited >> edits.log"}]}],"Notification":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`,
		filepath.Join(project, ".codex", "hooks.json"):           `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo stop"}]}]}}`,
		filepath.Join(project, ".claude", "settings.local.json"): `{"hooks":{"PreToolUse":[{"matcher":"Write","hooks":[{"type":"prompt","prompt":"is this ok"}]}]}}`,
	}
	for path, body := range files {
		if err := sys.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errOut := runOutput([]string{"hooks"})
	if code != exitOK {
		t.Fatalf("tofu hooks exited %d: %s", code, errOut)
	}
	for _, want := range []string{"rtk hook claude", "tofu runs the rtk rewrite itself", "echo edited >> edits.log", "not trusted", "echo stop",
		filepath.Join(".codex", "hooks.json"), "tofu does not fire Notification", "prompt hooks are not run by tofu yet", "· user ·"} {
		if !strings.Contains(out, want) {
			t.Errorf("tofu hooks does not say %q:\n%s", want, out)
		}
	}
	if code, out, errOut = runOutput([]string{"hooks", "trust"}); code != exitOK || !strings.Contains(out, "echo edited >> edits.log") || !strings.Contains(out, "echo stop") {
		t.Fatalf("tofu hooks trust exited %d and printed %s %s, want both project hooks trusted", code, out, errOut)
	}
	code, out, _ = runOutput([]string{"hooks", "--json"})
	var listed struct {
		Data struct {
			Hooks []struct {
				Command string `json:"command"`
				Trust   string `json:"trust"`
				Skipped string `json:"skipped"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || code != exitOK {
		t.Fatalf("tofu hooks --json exited %d with %s: %v", code, out, err)
	}
	trusted := 0
	for _, hook := range listed.Data.Hooks {
		if hook.Trust == "trusted" {
			trusted++
		}
		if hook.Trust == "not trusted" && hook.Skipped == "" {
			t.Errorf("after tofu hooks trust, %s still reads not trusted", hook.Command)
		}
	}
	if trusted != 2 {
		t.Errorf("%d hooks read trusted after tofu hooks trust, want the two project hooks that run: %s", trusted, out)
	}
}
