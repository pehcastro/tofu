package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/turn"
)

func TestThinkingDeltasReachOnThinkingInOrderAndTheDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"first the file, "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"then the test"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(server.Close)
	wire, err := anthropic.New(anthropic.Config{
		BaseURL: server.URL,
		Model:   "claude-opus-5",
		Proxy:   true,
		Token:   func(context.Context) (string, error) { return "sk-ant-api-test", nil },
	})
	if err != nil {
		t.Fatalf("new wire: %v", err)
	}

	var streamed []string
	decision, err := turn.Subscription{Wire: wire, Effort: llm.EffortMedium}.Ask(context.Background(), llm.Request{
		Messages:   []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		OnThinking: func(text string) { streamed = append(streamed, text) },
	})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if want := []string{"first the file, ", "then the test"}; !reflect.DeepEqual(streamed, want) {
		t.Fatalf("OnThinking received %q, not %q", streamed, want)
	}
	if decision.Thinking.Text != "first the file, then the test" || decision.Content != "done" {
		t.Fatalf("the decision holds thinking %q and content %q", decision.Thinking.Text, decision.Content)
	}
}

func TestOpusFiveAsksForSummarizedThinkingOnlyWhenItThinks(t *testing.T) {
	for effort, want := range map[llm.Effort]string{
		llm.EffortMedium: `{"type":"adaptive","display":"summarized"}`,
		llm.EffortNone:   "",
	} {
		body, err := anthropic.Request{
			Model:    "claude-opus-5",
			Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
			Effort:   effort,
		}.Encode(true)
		if err != nil {
			t.Fatalf("encoding at %s: %v", effort, err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("decoding at %s: %v", effort, err)
		}
		if got := string(decoded["thinking"]); got != want {
			t.Fatalf("at effort %s the thinking field is %q, not %q", effort, got, want)
		}
	}
}

func TestThinkingSummarySettingDecidesTheDisplayField(t *testing.T) {
	for omit, want := range map[bool]string{
		false: `{"type":"adaptive","display":"summarized"}`,
		true:  "",
	} {
		var sent map[string]json.RawMessage
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&sent)
			w.Header().Set("Content-Type", "text/event-stream")
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":3}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				`{"type":"message_stop"}`,
			} {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			}
		}))
		wire, err := anthropic.New(anthropic.Config{
			BaseURL: server.URL,
			Model:   "claude-opus-5",
			Proxy:   true,
			Token:   func(context.Context) (string, error) { return "sk-ant-api-test", nil },
		})
		if err != nil {
			t.Fatalf("new wire: %v", err)
		}
		_, err = turn.Subscription{Wire: wire, Effort: llm.EffortMedium}.Ask(context.Background(), llm.Request{
			Messages:            []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
			OmitThinkingSummary: omit,
		})
		server.Close()
		if err != nil {
			t.Fatalf("ask with omit %v: %v", omit, err)
		}
		if got := string(sent["thinking"]); got != want {
			t.Fatalf("with omit %v the thinking field is %q, not %q", omit, got, want)
		}
	}
}
