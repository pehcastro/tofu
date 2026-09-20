package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	"boji/internal/llm"
)

type contextModel struct {
	decisions []llm.Decision
	requests  []llm.Request
	calls     int
}

func (m *contextModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if err := ctx.Err(); err != nil {
		return llm.Decision{}, err
	}
	m.requests = append(m.requests, request)
	if m.calls >= len(m.decisions) {
		return llm.Decision{}, errors.New("contextModel: no more decisions queued")
	}
	decision := m.decisions[m.calls]
	m.calls++
	return decision, nil
}

type cancellingTool struct {
	cancel  context.CancelFunc
	after   int
	running sync.Mutex
	calls   int
}

func (t *cancellingTool) Name() string { return "read" }

func (t *cancellingTool) Definition() llm.Tool {
	return llm.Tool{Name: "read", Description: "a stub tool", Parameters: map[string]any{"type": "object"}}
}

func (t *cancellingTool) Run(context.Context, json.RawMessage) (Result, error) {
	t.running.Lock()
	t.calls++
	last := t.calls >= t.after
	t.running.Unlock()
	if last {
		t.cancel()
	}
	return Result{Content: "file contents", Command: "read a.txt"}, nil
}

func twoCalls() llm.Decision {
	return toolCallDecision(
		llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
		llm.ToolCall{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
	)
}

func rolesOf(messages []llm.Message) []llm.Role {
	roles := make([]llm.Role, len(messages))
	for index, message := range messages {
		roles[index] = message.Role
	}
	return roles
}

func secondSend(t *testing.T, config Config, carried []llm.Message) []llm.Message {
	t.Helper()
	second := &contextModel{decisions: []llm.Decision{messageDecision()}}
	config.Model, config.Task, config.History = second, "please continue", carried
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("the second send: %v", err)
	}
	return second.requests[0].Messages
}

func TestTheConversationARunReturnsIsTheOneTheNextRunSends(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	first := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, first, NewRegistry(tool))
	config.System = "the system prompt"
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the first run: %v", err)
	}

	roles := rolesOf(row.Conversation)
	t.Logf("the conversation the first run returned: %v", roles)
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleAssistant}
	if !slices.Equal(roles, want) {
		t.Fatalf("the conversation holds %v, want %v with no system message", roles, want)
	}
	if row.Conversation[3].Content != "done" {
		t.Fatalf("the conversation ends with %q, want the answer the model gave", row.Conversation[3].Content)
	}

	second := &stubModel{decisions: []llm.Decision{messageDecision()}}
	config.Model, config.Task, config.History = second, "and now the second task", row.Conversation
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("the second run: %v", err)
	}
	sent := second.requests[0].Messages
	if len(sent) != len(row.Conversation)+2 {
		t.Fatalf("the second run sent %d messages, want the system prompt, the %d carried, and the new task",
			len(sent), len(row.Conversation))
	}
	if sent[0].Role != llm.RoleSystem || sent[0].Content != "the system prompt" {
		t.Fatalf("the second run leads with %s %q, want the system prompt", sent[0].Role, sent[0].Content)
	}
	for index, carried := range row.Conversation {
		if sent[index+1].Role != carried.Role || sent[index+1].Content != carried.Content {
			t.Fatalf("message %d of the second request is %s %q, want the carried %s %q",
				index+1, sent[index+1].Role, sent[index+1].Content, carried.Role, carried.Content)
		}
	}
	last := sent[len(sent)-1]
	if last.Role != llm.RoleUser || last.Content != "and now the second task" {
		t.Fatalf("the second request ends with %s %q, want the new task", last.Role, last.Content)
	}
}

func TestATurnCancelledAfterTwoToolCallsCarriesEveryMessageIntoTheNextSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := &cancellingTool{cancel: cancel, after: 2}
	config := baseConfig(t, &contextModel{decisions: []llm.Decision{twoCalls()}}, NewRegistry(tool))

	row, err := Run(ctx, config)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled run returned %v, want a cancellation", err)
	}
	if tool.calls != 2 {
		t.Fatalf("the tool ran %d times, want the two calls the cancel lands after", tool.calls)
	}
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleTool}
	if roles := rolesOf(row.Conversation); !slices.Equal(roles, want) {
		t.Fatalf("a cancelled turn carries %v, want %v: the task, the calls and both results", roles, want)
	}

	sent := secondSend(t, config, row.Conversation)
	if roles := rolesOf(sent); !slices.Equal(roles, append(slices.Clone(want), llm.RoleUser)) {
		t.Fatalf("the send after the cancel carries %v, want the four from the cancelled turn and the new task", roles)
	}
	if len(sent[1].ToolCalls) != 2 {
		t.Fatalf("the assistant message carried %d tool calls, want the two that ran", len(sent[1].ToolCalls))
	}
	t.Logf("cancelled after %d tool calls, the next request sent %d messages: %v", tool.calls, len(sent), rolesOf(sent))
}

func TestATurnThatHitTheStepCapCarriesEveryMessageIntoTheNextSend(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents"}}
	config := baseConfig(t, &contextModel{decisions: []llm.Decision{twoCalls(), messageDecision()}}, NewRegistry(tool))
	config.Caps = Caps{MaxSteps: 1}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the capped run: %v", err)
	}
	if row.Outcome != OutcomeStepCap {
		t.Fatalf("outcome = %s, want the step cap", row.Outcome)
	}
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleTool, llm.RoleUser, llm.RoleAssistant}
	if roles := rolesOf(row.Conversation); !slices.Equal(roles, want) {
		t.Fatalf("a capped turn carries %v, want %v", roles, want)
	}
	if roles := rolesOf(secondSend(t, config, row.Conversation)); len(roles) != len(want)+1 {
		t.Fatalf("the send after the cap carries %v, want the six from the capped turn and the new task", roles)
	}
}

func TestATurnThatEndedInAModelErrorCarriesEveryMessageIntoTheNextSend(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents"}}
	model := &contextModel{decisions: []llm.Decision{twoCalls()}}
	config := baseConfig(t, model, NewRegistry(tool))

	row, err := Run(context.Background(), config)
	if err == nil {
		t.Fatal("the run was expected to fail when the model ran out of answers")
	}
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleTool}
	if roles := rolesOf(row.Conversation); !slices.Equal(roles, want) {
		t.Fatalf("an errored turn carries %v, want %v", roles, want)
	}
	if roles := rolesOf(secondSend(t, config, row.Conversation)); len(roles) != len(want)+1 {
		t.Fatalf("the send after the error carries %v, want the four from the errored turn and the new task", roles)
	}
	t.Logf("the turn failed with %v and still carried %v", err, rolesOf(row.Conversation))
}

func TestThePartialTextOfATruncatedAnswerReachesTheNextRequest(t *testing.T) {
	config := baseConfig(t, &contextModel{decisions: []llm.Decision{
		{Build: "m1", Outcome: llm.OutcomeTruncated, Content: "i was part way through saying"},
	}}, NewRegistry())
	config.NoLastWord = true

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the truncated run: %v", err)
	}
	if row.Outcome != OutcomeTruncated {
		t.Fatalf("outcome = %s, want truncated", row.Outcome)
	}
	sent := secondSend(t, config, row.Conversation)
	if sent[1].Content != "i was part way through saying" {
		t.Fatalf("the next request carries %q where the partial answer should be", sent[1].Content)
	}
}

func TestAnAssistantMessageWhoseCallsWereNeverAnsweredKeepsItsTextAndLosesTheCalls(t *testing.T) {
	recorded := []llm.Message{
		{Role: llm.RoleUser, Content: "the task"},
		{Role: llm.RoleAssistant, Content: "reading both", ToolCalls: []llm.ToolCall{{ID: "call-1"}, {ID: "call-2"}}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Content: "the first result"},
	}
	sendable := Sendable(recorded)
	if len(sendable) != 3 {
		t.Fatalf("a record cut between two results reads back as %d messages, want all three", len(sendable))
	}
	if sendable[1].Content != "reading both" {
		t.Fatalf("the partial answer reads %q, want its text kept", sendable[1].Content)
	}
	if len(sendable[1].ToolCalls) != 1 || sendable[1].ToolCalls[0].ID != "call-1" {
		t.Fatalf("the assistant message offers %v, want only the call that was answered", sendable[1].ToolCalls)
	}
}
