package turn

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"tofu/internal/judge/method"
	"tofu/internal/sift"
)

const recordedSessionFloor = 20

func recordedSessionsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		sessions := filepath.Join(dir, ".tofu", "sessions")
		if info, err := os.Stat(sessions); err == nil && info.IsDir() {
			return sessions
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skipf("no .tofu/sessions above %s, so no recorded turn can be measured", dir)
		}
		dir = parent
	}
}

type recordedShell struct {
	session string
	task    string
	shell   sift.Shell
}

func recordedShellResults(t *testing.T, sessions string) []recordedShell {
	t.Helper()
	entries, err := os.ReadDir(sessions)
	if err != nil {
		t.Fatal(err)
	}
	var out []recordedShell
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var header struct {
			ID   string `json:"id"`
			Task string `json:"task"`
		}
		raw, err := os.ReadFile(filepath.Join(sessions, entry.Name(), "header.json"))
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		out = append(out, shellResultsOf(t, filepath.Join(sessions, entry.Name(), "body.jsonl"), header.ID, header.Task)...)
	}
	return out
}

func shellResultsOf(t *testing.T, body, session, task string) []recordedShell {
	t.Helper()
	file, err := os.Open(body)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	calls := map[string]string{}
	var out []recordedShell
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event struct {
			Kind string     `json:"kind"`
			Body MessageRow `json:"body"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.Kind != "message" {
			continue
		}
		for _, call := range event.Body.ToolCalls {
			calls[call.ID] = call.Name
		}
		if event.Body.Role != "tool" || calls[event.Body.ToolCallID] != bashToolName {
			continue
		}
		out = append(out, recordedShell{
			session: session,
			task:    task,
			shell:   sift.Shell{Command: bashToolName, Stdout: event.Body.Content},
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return out
}

func TestTheBytesTheCheapMethodSavesOverEveryRecordedTurn(t *testing.T) {
	results := recordedShellResults(t, recordedSessionsDir(t))
	type bytes struct{ raw, sent int }
	perSession := map[string]bytes{}
	sifter := ShellSift{Methods: methodTable(t, string(method.Cheap)), KeepAt: 0.5}
	whole := bytes{}
	for _, one := range results {
		cut, err := sifter.Cut(context.Background(), one.shell, one.task)
		if err != nil {
			t.Fatalf("%s: %v", one.session, err)
		}
		whole.raw += len(one.shell.Stdout)
		whole.sent += len(cut.Text)
		session := perSession[one.session]
		perSession[one.session] = bytes{session.raw + len(one.shell.Stdout), session.sent + len(cut.Text)}
	}
	if len(perSession) < recordedSessionFloor {
		t.Skipf("%d recorded sessions carry a shell result and the measurement asks for %d", len(perSession), recordedSessionFloor)
	}
	if whole.raw == 0 {
		t.Fatal("the recorded sessions carry no shell output, so nothing was measured")
	}

	shares := make([]float64, 0, len(perSession))
	for _, session := range perSession {
		if session.raw == 0 {
			continue
		}
		shares = append(shares, float64(session.raw-session.sent)/float64(session.raw)*100)
	}
	sort.Float64s(shares)
	median := shares[len(shares)/2]

	t.Logf("%d bash results over %d recorded sessions: %d bytes became %d, %d saved, %.1f percent, median session %.1f percent",
		len(results), len(perSession), whole.raw, whole.sent, whole.raw-whole.sent,
		float64(whole.raw-whole.sent)/float64(whole.raw)*100, median)
	t.Logf("the largest saving in one session is %.1f percent and the smallest is %.1f percent",
		slices.Max(shares), slices.Min(shares))
}
