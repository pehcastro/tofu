package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm/wire/codex"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const codexStubStreamReasoningToolCall = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}

data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"thinking"}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"}}

data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"write"}}

data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":\"hello.txt\",\"content\":\"hi\"}"}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"write","arguments":"{\"path\":\"hello.txt\",\"content\":\"hi\"}"}}

data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":11,"output_tokens":4}}}

`

const codexStubStreamFinalText = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","output_index":0,"delta":"wrote hello.txt"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.completed","response":{"id":"resp_2","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":20,"output_tokens":6}}}

`

func TestAReasoningItemFromStepOneReachesTheStepTwoCodexRequestAndTheStoredRow(t *testing.T) {
	var requests [][]byte
	streams := []string{codexStubStreamReasoningToolCall, codexStubStreamFinalText}
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the sent body: %v", err)
		}
		requests = append(requests, sent)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, streams[served])
		served++
	}))
	t.Cleanup(server.Close)

	wire, err := codex.New(codex.Config{
		Model:   "gpt-5.6-sol",
		BaseURL: server.URL,
		Token:   func(context.Context) (string, error) { return "stub-token-not-a-real-credential", nil },
	})
	if err != nil {
		t.Fatalf("building the codex wire: %v", err)
	}
	model := codexTurn{wire: wire}

	dir := t.TempDir()
	opts := armOpts(t, "--tools", toolSetThree)
	opts.dir, opts.task = dir, "write hello.txt"
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	store := session.NewStore(t.TempDir())
	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription, sessions: store})

	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("the wire saw %d requests, want 2", len(requests))
	}

	var body struct {
		Input []struct {
			Type string `json:"type"`
			Role string `json:"role"`
		} `json:"input"`
	}
	if err := json.Unmarshal(requests[1], &body); err != nil {
		t.Fatalf("the second request is not json: %v", err)
	}
	types := make([]string, len(body.Input))
	for index, item := range body.Input {
		kind := item.Type
		if kind == "" {
			kind = item.Role
		}
		types[index] = kind
	}
	t.Logf("step 2 encoded input item types, in order: %v", types)
	reasoningAt, functionCallAt := -1, -1
	for index, kind := range types {
		switch kind {
		case "reasoning":
			reasoningAt = index
		case "function_call":
			functionCallAt = index
		}
	}
	if reasoningAt < 0 || functionCallAt < 0 || reasoningAt != functionCallAt-1 {
		t.Fatalf("the reasoning item is not immediately ahead of the function call: %v", types)
	}

	sessionFile := filepath.Join(store.Dir(row.Session), "events.jsonl")
	onDisk, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("reading the session file: %v", err)
	}
	t.Logf("session file at %s", sessionFile)
	if !strings.Contains(string(onDisk), `"reasoning":{"id":"rs_1","encrypted_content":"opaque"}`) {
		t.Fatal("the session file does not carry the reasoning item as a named id and payload field")
	}
}
