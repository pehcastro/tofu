package turn

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestACommandPastItsOwnDeadlineIsStoppedAndSaysSoAndHowLongItRan(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}

	started := time.Now()
	_, err = tool.Run(context.Background(), json.RawMessage(`{"command":"sleep 30","timeout_ms":300}`))
	took := time.Since(started)

	if err == nil {
		t.Fatal("a command that outran its deadline came back as an ordinary result")
	}
	if took > 10*time.Second {
		t.Fatalf("nothing stopped the command: it ran %v against a 300 ms deadline", took)
	}
	for _, want := range []string{"degraded stopped", "past the 300 ms deadline", "timeout_ms", "project_report"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the stopped command never says %q: %v", want, err)
		}
	}
	if !regexp.MustCompile(`ran \d+ ms`).MatchString(err.Error()) {
		t.Fatalf("the stopped command never says how long it ran: %v", err)
	}
	t.Logf("stopped after %v with: %v", took, err)
}

func TestACommandThatFinishesInsideItsDeadlineIsUnaffected(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"echo well inside","timeout_ms":30000}`))
	if err != nil {
		t.Fatalf("a quick command was treated as an overrun: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 || !strings.Contains(result.Content, "well inside") {
		t.Fatalf("the quick command returned %+v", result)
	}
}

func TestATimeoutAboveTheCeilingIsTakenAtTheCeilingAndTheResultSaysSo(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"echo over the cap","timeout_ms":900000}`))
	if err != nil {
		t.Fatalf("a timeout_ms above the ceiling ended the call: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 || !strings.Contains(result.Content, "over the cap") {
		t.Fatalf("the command did not run: %+v", result)
	}
	for _, want := range []string{"900000", "600000"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the result never says %q, so the model cannot tell it did not get what it asked for: %q", want, result.Content)
		}
	}
	t.Logf("timeout_ms 900000 returned: %q", result.Content)
}

func TestBashToolRunsTheRecordedQuotedCommandsThatCmdExeTurnedIntoABackslashQuoteAndZeroBytes(t *testing.T) {
	recorded := []struct {
		session string
		command string
		want    string
	}{
		{"turn-18d6a27c7dfb1644", `grep -n "done" src/app.test.ts`, "6:  it('accepts an explicit done flag'"},
		{"turn-18d6a27c7dfb1644", `grep -n "done\|201)\|repeat" src/app.test.ts`, "12:    const res = await postJson('/tasks', { title: 'x'.repeat(201) })"},
		{"turn-18d6a27c7dfb1644", `grep -nE "describe\(" src/app.test.ts`, "5:describe('POST /tasks'"},
		{"turn-18d6a80dc5d90f84", `grep -n "import" src/app.test.ts`, "1:import { describe, it, expect } from 'bun:test'"},
		{"turn-18d6a80dc5d90f84", `grep -n "Hono" src/app.ts`, "2:import { Hono } from 'hono'"},
	}
	tool, err := NewBashTool(seedRecordedFixture(t))
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	for _, recording := range recorded {
		t.Run(recording.command, func(t *testing.T) {
			args, err := json.Marshal(bashArgs{Command: recording.command})
			if err != nil {
				t.Fatalf("encoding the recorded command: %v", err)
			}
			result, err := tool.Run(context.Background(), json.RawMessage(args))
			if err != nil {
				t.Fatalf("running the recorded command from %s: %v", recording.session, err)
			}
			if result.ExitCode == nil || *result.ExitCode != 0 {
				t.Fatalf("recorded command from %s exited %d with %q", recording.session, *result.ExitCode, result.Content)
			}
			if !strings.Contains(result.Content, recording.want) {
				t.Fatalf("recorded command from %s returned %q, wanted a line containing %q", recording.session, result.Content, recording.want)
			}
		})
	}
}

func TestBashToolRunsTheRecordedPipelineWhoseRedirectionAndExitStatusAreShellSyntaxCmdExeDoesNotHave(t *testing.T) {
	tool, err := NewBashTool(seedRecordedFixture(t))
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"grep -a \"import\" src/app.test.ts | head -3; echo \"rc=$?\""}`))
	if err != nil {
		t.Fatalf("running the recorded command from turn-18d6a80dc5d90f84: %v", err)
	}
	if !strings.Contains(result.Content, "rc=0") {
		t.Fatalf("the pipeline reported %q, wanted it to end with rc=0", result.Content)
	}
	if strings.Count(result.Content, "import") != 3 {
		t.Fatalf("the pipeline returned %q, wanted the three import lines", result.Content)
	}
}
