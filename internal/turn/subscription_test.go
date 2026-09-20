package turn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"boji/internal/llm/wire/anthropic"
)

func recordedSubscription(t *testing.T, streams ...string) Subscription {
	t.Helper()
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if served >= len(streams) {
			t.Errorf("the loop asked for %d turns and only %d streams were recorded", served+1, len(streams))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, err := os.ReadFile(filepath.Join("testdata", streams[served]))
		if err != nil {
			t.Errorf("reading the recorded stream: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		served++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	wire, err := anthropic.New(anthropic.Config{
		BaseURL: server.URL,
		Proxy:   true,
		Model:   "claude-sonnet-4-5-20250929",
		Token:   func(context.Context) (string, error) { return "sk-ant-oat01-recorded", nil },
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return Subscription{Wire: wire}
}

func TestTheTurnRowRoundTripsTheStopReasonAndTheTokensOfARecordedSubscriptionStream(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "wrote hello.txt", Command: "write hello.txt"}}
	gate := gateSaying("allow")
	config := Config{
		Model:          recordedSubscription(t, "subscription-tool-use.sse", "subscription-end-turn.sse"),
		Spend:          SpendSubscription,
		Tools:          NewRegistry(tool),
		Gate:           gate,
		Task:           "write hello.txt",
		Caps:           Caps{MaxSteps: 10, MaxDecisions: 10},
		ResultBytesCap: 4096,
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	read := writeAndReadBack(t, row)

	if len(gate.requests) != 1 || len(read.DecisionIDs) != 1 {
		t.Fatalf("the gate must see every tool call on the subscription arm too: %d asked, %d recorded",
			len(gate.requests), len(read.DecisionIDs))
	}

	if read.Spend != SpendSubscription || read.TotalCostUSD != 0 {
		t.Fatalf("a subscription row must carry no money: spend %q total %v", read.Spend, read.TotalCostUSD)
	}
	if read.Model != "claude-sonnet-4-5-20250929" {
		t.Fatalf("the row reports model %q, and the stream reported claude-sonnet-4-5-20250929", read.Model)
	}
	if len(read.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(read.Steps))
	}

	first := read.Steps[0]
	if first.StopReason != "tool_use" {
		t.Fatalf("step 1 stop reason is %q, the stream reported tool_use", first.StopReason)
	}
	if first.PromptTokens != 1204 || first.CompletionTokens != 78 ||
		first.CacheReadTokens != 8192 || first.CacheWriteTokens != 311 {
		t.Fatalf("step 1 tokens are %+v", first)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Tool != "write" {
		t.Fatalf("step 1 tool calls are %+v, and the stream named the tool _write", first.ToolCalls)
	}

	second := read.Steps[1]
	if second.StopReason != "end_turn" || second.AssistantText != "wrote hello.txt" {
		t.Fatalf("step 2 is %+v", second)
	}
	if second.PromptTokens != 96 || second.CompletionTokens != 14 ||
		second.CacheReadTokens != 9503 || second.CacheWriteTokens != 0 {
		t.Fatalf("step 2 tokens are %+v", second)
	}
}
