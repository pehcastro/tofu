package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/turn"
)

type sentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type sentMessage struct {
	Role    string      `json:"role"`
	Content []sentBlock `json:"content"`
}

func sentMessages(t *testing.T, body []byte) []sentMessage {
	t.Helper()
	var decoded struct {
		Messages []sentMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	return decoded.Messages
}

func assertNoBlankTextBlock(t *testing.T, messages []sentMessage) {
	t.Helper()
	for index, message := range messages {
		for part, block := range message.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) == "" {
				t.Fatalf("message %d (%s) block %d is a whitespace-only text block %q", index, message.Role, part, block.Text)
			}
		}
	}
}

func encodedMessages(t *testing.T, messages []llm.Message) []sentMessage {
	t.Helper()
	body, err := anthropic.Request{Model: "claude-test", Messages: messages}.Encode(false)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return sentMessages(t, body)
}

func TestTheRequestAfterATruncatedBlankReplyBesideAToolCallCarriesNoBlankTextBlock(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	listening := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		first := len(bodies) == 1
		mu.Unlock()
		events := []string{`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`}
		if first {
			events = append(events,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"\n\n"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"_read","input":{}}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"note.txt\"}"}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":2}}`)
		} else {
			events = append(events,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"the note says hello"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range append(events, `{"type":"message_stop"}`) {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	t.Cleanup(listening.Close)
	wire, err := anthropic.New(anthropic.Config{BaseURL: listening.URL, Model: "claude-test", Proxy: true,
		Token: func(context.Context, string) (string, error) { return "sk-ant-oat01-test", nil }})
	if err != nil {
		t.Fatalf("opening the wire: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("writing the note: %v", err)
	}
	read, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatalf("building read: %v", err)
	}
	row, err := turn.Run(context.Background(), turn.Config{
		Model: turn.Subscription{Wire: wire}, Spend: turn.SpendSubscription, Tools: turn.NewRegistry(read),
		System: "you read files", Task: "what does note.txt say", Caps: turn.Caps{MaxSteps: 3},
		ResultBytesCap: 1 << 16, ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-test" },
	})
	if err != nil {
		t.Fatalf("running the turn: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("the turn sent %d requests, want 2", len(bodies))
	}
	second := sentMessages(t, bodies[1])
	for index, message := range second {
		blocks, _ := json.Marshal(message.Content)
		t.Logf("second request message %d %s: %s", index, message.Role, blocks)
	}
	assertNoBlankTextBlock(t, second)
	if answer := row.Conversation[len(row.Conversation)-1].Content; answer != "the note says hello" {
		t.Fatalf("the turn ended on %q, want the answer", answer)
	}
}

func TestAStoredBlankAssistantMessageIsNotSent(t *testing.T) {
	stored := []llm.Message{
		{Role: llm.RoleUser, Content: "read it"},
		{Role: llm.RoleAssistant, Content: "\n\n", ToolCalls: []llm.ToolCall{{ID: "toolu_lost", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: llm.RoleUser, Content: "again"},
		{Role: llm.RoleAssistant, Content: "  　\t"},
		{Role: llm.RoleUser, Content: "and again"},
	}
	sendable := turn.Sendable(stored)
	for _, message := range sendable {
		if message.Role == llm.RoleAssistant {
			t.Fatalf("Sendable kept the blank assistant message %q", message.Content)
		}
	}
	sent := encodedMessages(t, stored)
	assertNoBlankTextBlock(t, sent)
	var roles []string
	for _, message := range sent {
		roles = append(roles, message.Role)
	}
	if got := strings.Join(roles, " "); got != "user assistant user user" {
		t.Fatalf("the encoder sent %s, want the blank assistant message left out", got)
	}
}

func TestBlankTextGoesAndEverythingBesideItStaysInOrder(t *testing.T) {
	sent := encodedMessages(t, []llm.Message{
		{Role: llm.RoleUser, Content: " \n", Images: []llm.Image{{MediaType: "image/png", Data: []byte("png")}}},
		{Role: llm.RoleAssistant, Content: "\n\n", Thinking: llm.Thinking{Text: "read it first", Signature: "sig"},
			ToolCalls: []llm.ToolCall{{ID: "toolu_1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "a"},
		{Role: llm.RoleAssistant, Content: "  it says a\n"},
	})
	var shape []string
	for _, message := range sent {
		for _, block := range message.Content {
			shape = append(shape, message.Role+":"+block.Type)
		}
	}
	if got, want := strings.Join(shape, " "), "user:image assistant:thinking assistant:tool_use user:tool_result assistant:text"; got != want {
		t.Fatalf("the blocks went out as %s, want %s", got, want)
	}
	if text := sent[len(sent)-1].Content[0].Text; text != "  it says a\n" {
		t.Fatalf("visible text went out as %q, want it byte for byte", text)
	}
}

func TestAUserMessageOfWhitespaceAloneIsRefused(t *testing.T) {
	_, err := anthropic.Request{Model: "claude-test", Messages: []llm.Message{{Role: llm.RoleUser, Content: " \n\t"}}}.Encode(false)
	if err == nil {
		t.Fatal("a whitespace-only user message encoded")
	}
}
