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

	"tofu/internal/konst"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/rule"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	shipped "tofu/library"
)

const (
	sessionEnvironment = "<env>\nworking directory: F:\\localhost\\hono-app-2\nplatform: windows/amd64\n</env>"
	firstTurnTask      = "this is a hono app with bun, i need you to check the nw.sqlite and extract its schema, then build a hono api using sub agents in parallel, start the dev server for me so i can test too"
	secondTurnTask     = "debug why bun is running a very wrong project and not this one, its pointing to my portfolio project"
	thirdTurnTask      = "can you explain what was done to me? can be with lld"
)

type capturedServer struct {
	mu      sync.Mutex
	bodies  [][]byte
	replies []string
}

func (s *capturedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, body)
	reply := "text"
	if len(s.replies) > 0 {
		reply, s.replies = s.replies[0], s.replies[1:]
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	events := []string{`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`}
	if reply == "read" {
		events = append(events,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"_read","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"API_SPEC.md\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`)
	} else {
		events = append(events,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`)
	}
	for _, event := range append(events, `{"type":"message_stop"}`) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
	}
}

func sessionConfig(t *testing.T, replies ...string) (turn.Config, *capturedServer) {
	t.Helper()
	server := &capturedServer{replies: replies}
	listening := httptest.NewServer(server)
	t.Cleanup(listening.Close)
	wire, err := anthropic.New(anthropic.Config{
		BaseURL: listening.URL,
		Model:   "claude-test",
		Proxy:   true,
		Token:   func(context.Context) (string, error) { return "sk-ant-oat01-test", nil },
	})
	if err != nil {
		t.Fatalf("opening the wire: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "API_SPEC.md"), []byte(strings.Repeat("GET /customers returns every customer\n", 200)), 0o600); err != nil {
		t.Fatalf("writing the spec: %v", err)
	}
	read, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatalf("building read: %v", err)
	}
	write, err := turn.NewWriteTool(root)
	if err != nil {
		t.Fatalf("building write: %v", err)
	}
	return turn.Config{
		Model:          turn.Subscription{Wire: wire},
		Spend:          turn.SpendSubscription,
		Tools:          turn.NewRegistry(read, write),
		Environment:    sessionEnvironment,
		Caps:           turn.Caps{MaxSteps: 5},
		ResultBytesCap: 1 << 16,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return "turn-test" },
	}, server
}

func sessionSpec(t *testing.T, task string, role rule.Role) turn.ComposeSpec {
	t.Helper()
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatalf("loading the shipped rules: %v", err)
	}
	return turn.ComposeSpec{
		Task:         task,
		Environment:  sessionEnvironment,
		ToolGuidance: turn.EveryToolIsRelativeToTheWorkingDirectory + turn.SpawnAddendum,
		Rules:        rules,
		Role:         role,
	}
}

type cachedPiece struct {
	where      string
	text       string
	breakpoint bool
}

func inCacheOrder(t *testing.T, body []byte) []cachedPiece {
	t.Helper()
	var decoded struct {
		Tools    []map[string]any `json:"tools"`
		System   []map[string]any `json:"system"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	var pieces []cachedPiece
	add := func(where string, block map[string]any) {
		marked, breakpoint := block["cache_control"].(map[string]any)
		if breakpoint && marked["ttl"] != konst.SubscriptionCacheTTL {
			t.Fatalf("the breakpoint on %s carries ttl %v, not %s", where, marked["ttl"], konst.SubscriptionCacheTTL)
		}
		delete(block, "cache_control")
		text, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("encoding %s: %v", where, err)
		}
		pieces = append(pieces, cachedPiece{where: where, text: string(text), breakpoint: breakpoint})
	}
	for _, tool := range decoded.Tools {
		add(fmt.Sprintf("the tool %v", tool["name"]), tool)
	}
	for index, block := range decoded.System {
		if text, _ := block["text"].(string); !strings.HasPrefix(text, anthropic.BillingHeaderPrefix) {
			add(fmt.Sprintf("system block %d", index), block)
		}
	}
	for index, message := range decoded.Messages {
		for part, block := range message.Content {
			add(fmt.Sprintf("message %d block %d", index, part), block)
		}
	}
	return pieces
}

func assertCachedPrefixCarriesOver(t *testing.T, earlier, later []byte) {
	t.Helper()
	cached, next := inCacheOrder(t, earlier), inCacheOrder(t, later)
	last := -1
	for index, piece := range cached {
		if piece.breakpoint {
			last = index
		}
	}
	if last < 0 {
		t.Fatal("the earlier request placed no breakpoint")
	}
	for index, piece := range cached[:last+1] {
		if index >= len(next) {
			t.Fatalf("the later request ends before %s, piece %d of the %d cached", piece.where, index+1, last+1)
		}
		if later := next[index].text; later != piece.text {
			at := 0
			for at < min(len(later), len(piece.text)) && later[at] == piece.text[at] {
				at++
			}
			t.Fatalf("the cached prefix ends at %s and breaks at %s, piece %d of %d, byte %d: %q against %q",
				cached[last].where, piece.where, index+1, last+1, at,
				piece.text[at:min(len(piece.text), at+160)], later[at:min(len(later), at+160)])
		}
	}
	t.Logf("%d pieces up to %s carry over byte for byte", last+1, cached[last].where)
}

func TestSiblingSubAgentsFromOneDefinitionShareTheCachedPrefix(t *testing.T) {
	base, server := sessionConfig(t)
	definition := subagent.Definition{Name: "ts-dev", Description: "typescript", Origin: "library", Runs: subagent.RunsModel,
		Instructions: "you write typescript for bun and hono"}
	spawn := turn.NewSpawnTool("turn-test", base, &subagent.Roster{})
	spawn.SubAgents = turn.SubAgents{Defined: []subagent.Definition{definition}, Prompt: sessionSpec(t, "", rule.RoleAny)}
	for _, brief := range []struct{ task, owns string }{
		{"write the customers routes against API_SPEC.md", "src/routes/customers.ts"},
		{"debug the orders routes against API_SPEC.md", "src/routes/orders.ts"},
	} {
		args, _ := json.Marshal(map[string]any{"task": brief.task, "owns": []string{brief.owns}, "agent": definition.Name})
		if _, err := spawn.Run(context.Background(), args); err != nil {
			t.Fatalf("spawning for %s: %v", brief.owns, err)
		}
	}
	if len(server.bodies) != 2 {
		t.Fatalf("two sub-agents sent %d requests, want one each", len(server.bodies))
	}
	assertCachedPrefixCarriesOver(t, server.bodies[0], server.bodies[1])
	assertFirstMessageCarries(t, server.bodies[0], "the paths you hold, and the only ones write, edit and bash may change: src/routes/customers.ts")
	assertFirstMessageCarries(t, server.bodies[1], "from the rule debug_loop]", "may change: src/routes/orders.ts")
}

func TestTheNextTurnsFirstRequestReadsTheLastTurnsCachedPrefix(t *testing.T) {
	config, server := sessionConfig(t, "read", "text", "text", "text")
	for _, task := range []string{firstTurnTask, secondTurnTask, thirdTurnTask} {
		composed, err := turn.Compose(sessionSpec(t, task, rule.RoleOrchestrator))
		if err != nil {
			t.Fatalf("composing: %v", err)
		}
		config.System, config.Environment, config.Task = composed.Head(), composed.WithTaskRules(sessionEnvironment), task
		row, err := turn.Run(context.Background(), config)
		if err != nil {
			t.Fatalf("running %q: %v", task, err)
		}
		config.History = turn.Sendable(row.Conversation)
	}
	if len(server.bodies) != 4 {
		t.Fatalf("three turns sent %d requests, want 2, 1 and 1", len(server.bodies))
	}
	assertCachedPrefixCarriesOver(t, server.bodies[1], server.bodies[2])
	assertFirstMessageCarries(t, server.bodies[0], "[code_rules, from the rule e2e_first]")
	assertFirstMessageCarries(t, server.bodies[2], "[process_discipline, from the rule debug_loop]\nbefore you form a theory")
	assertCachedPrefixCarriesOver(t, server.bodies[2], server.bodies[3])
	assertFirstMessageCarries(t, server.bodies[3], "[task_shaping, from the rule design_docs]")
}

func assertFirstMessageCarries(t *testing.T, body []byte, wanted ...string) {
	t.Helper()
	var decoded struct {
		Messages []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	last := decoded.Messages[len(decoded.Messages)-1].Content[0].Text
	for _, text := range wanted {
		if !strings.Contains(last, text) {
			t.Fatalf("the turn's first user message does not carry %q:\n%s", text, last)
		}
	}
}
