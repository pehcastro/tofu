package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/llm"
)

func TestAFileReadInTurnOneIsEditedInTurnTwoWithoutARefusal(t *testing.T) {
	dir := scratchProject(t)
	target := filepath.Join(dir, "package.json")
	if err := os.WriteFile(target, []byte("{\n  \"name\": \"moth\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := func(id, name, args string) llm.Decision {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}}
	}
	model := &sendModel{queued: []llm.Decision{
		call("call-1", "read", `{"path":"package.json"}`),
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it"},
		call("call-2", "edit", `{"path":"package.json","old_string":"moth","new_string":"inky"}`),
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "renamed it"},
	}}
	live := newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})

	var events eventLog
	live.run(t.Context(), onTheSubscription, "read package.json", events.add)
	live.run(t.Context(), onTheSubscription, "rename the package to inky", events.add)

	if got, _ := os.ReadFile(target); string(got) != "{\n  \"name\": \"inky\"\n}\n" {
		t.Fatalf("turn 2's edit did not reach package.json, which is %q", got)
	}
	if len(model.requests) != 4 {
		t.Fatalf("the two turns asked the model %d times, want 4: a refused edit costs another request", len(model.requests))
	}
}
