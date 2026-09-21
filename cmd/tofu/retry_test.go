package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const anthropicEndTurnStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_01retry","model":"claude-sonnet-4-5-20250929","usage":{"input_tokens":12,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answered"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`

func TestARetriedSubscriptionStepIsRecordedAsASecondAttempt(t *testing.T) {
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		if served == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicEndTurnStream)
	}))
	defer server.Close()

	wire, err := anthropic.New(anthropic.Config{
		BaseURL:   server.URL,
		Proxy:     true,
		Model:     "claude-sonnet-4-5-20250929",
		Token:     func(context.Context) (string, error) { return "sk-ant-oat01-stub", nil },
		Transport: turnTransportConfig(),
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}

	row, err := turn.Run(context.Background(), turn.Config{
		Model:          turn.Subscription{Wire: wire},
		Spend:          turn.SpendSubscription,
		Task:           "answer once",
		Caps:           turn.Caps{MaxSteps: 2},
		ResultBytesCap: 1024,
		ArtifactDir:    t.TempDir(),
		NoLastWord:     true,
	})
	if err != nil {
		t.Fatalf("one 503 ended the turn: %v", err)
	}
	if served != 2 {
		t.Fatalf("the stub saw %d requests, want the failure and one retry", served)
	}
	_, events, err := row.Record()
	if err != nil {
		t.Fatal(err)
	}
	var attempts []int
	for _, event := range events {
		if event.Kind == session.EventStep {
			attempts = append(attempts, event.Attempt)
		}
	}
	if len(attempts) != 1 || attempts[0] != 2 {
		t.Fatalf("the recorded steps carry attempts %v, want one step recorded as the second attempt", attempts)
	}
}
