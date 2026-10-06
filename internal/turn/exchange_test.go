package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/session"
)

func TestEveryRequestIsKeptByteForByteWithTheProviderErrorBody(t *testing.T) {
	const refusal = `{"type":"error","error":{"type":"invalid_request_error","message":"messages.3: tool_use ids were found without tool_result blocks immediately after: toolu_1"},"request_id":"req_011"}`
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		first := len(bodies) == 1
		mu.Unlock()
		if !first {
			w.Header().Set("request-id", "req_011")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, refusal)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"_read","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.txt\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	t.Cleanup(server.Close)
	wire, err := anthropic.New(anthropic.Config{BaseURL: server.URL, Model: "claude-test", Proxy: true,
		Token: func(context.Context, string) (string, error) { return "sk-ant-oat01-test", nil }})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	store, id := session.NewStore(t.TempDir()), session.NewEventID()
	_, err = Run(context.Background(), Config{Model: Subscription{Wire: wire}, Spend: SpendSubscription, Tools: NewRegistry(read), System: "you read files",
		Task: "read a.txt", Caps: Caps{MaxSteps: 3}, ResultBytesCap: 4096, ArtifactDir: t.TempDir(), Sessions: store, Session: id})
	if err == nil || !strings.Contains(err.Error(), "invalid_request_error") {
		t.Fatalf("the turn ended with %v, want the provider's 400", err)
	}

	exchanges, err := store.Exchanges(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(exchanges) != 2 || len(bodies) != 2 {
		t.Fatalf("the session keeps %d requests and the server saw %d, want 2 of each", len(exchanges), len(bodies))
	}
	for i, exchange := range exchanges {
		view, err := store.Request(id, exchange.Request)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Attempts) != 1 {
			t.Fatalf("request %d keeps %d attempts, want the one the server saw", i+1, len(view.Attempts))
		}
		if !bytes.Equal(view.Attempts[0].Body, bodies[i]) {
			t.Fatalf("request %d keeps the body\n%s\nand the server read\n%s", i+1, view.Attempts[0].Body, bodies[i])
		}
	}
	failed, err := store.Request(id, exchanges[1].Request)
	if err != nil {
		t.Fatal(err)
	}
	var attempt llm.Attempt
	if err := json.Unmarshal(failed.Attempts[0].Detail, &attempt); err != nil {
		t.Fatal(err)
	}
	if attempt.Status != http.StatusBadRequest || attempt.RequestID != "req_011" || attempt.ErrorBody != refusal || !strings.Contains(failed.Error, refusal) {
		t.Fatalf("the failed request keeps status %d, id %q, body %q and error %q, want the 400 whole", attempt.Status, attempt.RequestID, attempt.ErrorBody, failed.Error)
	}
	var roles []string
	for _, message := range failed.Messages {
		roles = append(roles, strings.Join(strings.Fields(message.Role+" "+message.ToolCallID+" "+message.Origin), " "))
	}
	if got := strings.Join(roles, ", "); got != "system, user task, assistant, tool toolu_1, user step cap notice" {
		t.Fatalf("the failed request keeps the messages %s", got)
	}
}
