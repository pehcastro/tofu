package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"tofu/internal/llm"
	sessionstore "tofu/internal/session"
	"tofu/internal/turn"
)

type scriptedModel struct {
	decisions []llm.Decision
	asked     int
}

func (m *scriptedModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	m.asked++
	return m.decisions[m.asked-1], nil
}

var drawnHash = regexp.MustCompile(`#[0-9a-f]{6}`)

func (d *appDriver) hashesDrawn() []string {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	var hashes []string
	for _, frame := range d.frames {
		hashes = append(hashes, drawnHash.FindAllString(frame, -1)...)
	}
	return hashes
}

func TestTheHashChatDrawsIsTheIDOfTheCallInTheRecord(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("a note"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := armOpts(t)
	opts.dir, opts.task, opts.turnID = dir, "read the note", turn.NewID(time.Now())
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	driver := driveApp(t)
	model := &scriptedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "toolu_01thecall", Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it"},
	}}
	watch := &appWatcher{inner: model, emit: driver.emit, now: time.Now, turnID: opts.turnID, seen: map[string]bool{}}
	config, _ := mustConfig(t, opts, built, runtime{model: watch, spend: turn.SpendSubscription, sessions: sessions})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}

	hashes := driver.hashesDrawn()
	if len(hashes) == 0 {
		t.Fatal("no frame drew a hash for the call")
	}

	events, err := sessions.Body(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	recorded := ""
	for _, event := range events {
		if event.Kind != sessionstore.EventStep {
			continue
		}
		var step turn.StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatal(err)
		}
		for _, call := range step.ToolCalls {
			recorded = call.ID
		}
	}
	if recorded == "" {
		t.Fatal("the record carries no tool call to find")
	}
	for _, hash := range hashes {
		if sessionstore.DrawnAs(recorded, hash) {
			t.Logf("chat drew %s and the record carries %s for the call", hash, recorded)
			return
		}
	}
	t.Fatalf("the record carries the call as %s and chat drew %v, none of which reaches it", recorded, hashes)
}
