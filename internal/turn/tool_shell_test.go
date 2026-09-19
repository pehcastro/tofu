package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const recordedFixture = `import { describe, it, expect } from 'bun:test'
import { Hono } from 'hono'
import { app } from './app'

describe('POST /tasks', () => {
  it('accepts an explicit done flag', async () => {
    const res = await postJson('/tasks', { title: 'already done', done: true })
    expect(res.status).toBe(201)
  })

  it('rejects an over-long title with 400', async () => {
    const res = await postJson('/tasks', { title: 'x'.repeat(201) })
    expect(res.status).toBe(400)
  })
})
`

func seedRecordedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("seeding the fixture directory: %v", err)
	}
	for _, name := range []string{"app.ts", "app.test.ts"} {
		if err := os.WriteFile(filepath.Join(root, "src", name), []byte(recordedFixture), 0o644); err != nil {
			t.Fatalf("seeding the fixture: %v", err)
		}
	}
	return root
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

func TestReadToolReturnsANamedLineRangeSoTheModelNeedNotShellOutToSed(t *testing.T) {
	tool, err := NewReadTool(seedRecordedFixture(t))
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"path":"src/app.test.ts","start_line":11,"end_line":14}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	want := "src/app.test.ts lines 11-14 of 15\n" +
		"  it('rejects an over-long title with 400', async () => {\n" +
		"    const res = await postJson('/tasks', { title: 'x'.repeat(201) })\n" +
		"    expect(res.status).toBe(400)\n" +
		"  })"
	if result.Content != want {
		t.Fatalf("read returned\n%q\nwanted\n%q", result.Content, want)
	}
	if result.Command != "read src/app.test.ts lines 11-14 of 15" {
		t.Fatalf("the step row recorded %q", result.Command)
	}
}

func TestReadToolClampsAnEndLinePastTheLastLineAndRefusesAStartLinePastIt(t *testing.T) {
	tool, err := NewReadTool(seedRecordedFixture(t))
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"path":"src/app.test.ts","start_line":14,"end_line":900}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	if !strings.HasPrefix(result.Content, "src/app.test.ts lines 14-15 of 15\n") {
		t.Fatalf("read returned %q, wanted the clamped range named in the first line", result.Content)
	}

	_, err = tool.Run(context.Background(), json.RawMessage(`{"path":"src/app.test.ts","start_line":900}`))
	if err == nil {
		t.Fatal("expected a start_line past the end of the file to fail rather than return nothing")
	}
	if !strings.Contains(err.Error(), "has 15 lines") {
		t.Fatalf("the error did not say how long the file is: %v", err)
	}
}
