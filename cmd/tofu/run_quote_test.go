package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	quotedWords    = "the gate reads library/policy/shell.yaml before it asks"
	quoteReference = "#c41099"
)

func sessionCarryingOneTurn(t *testing.T) (*session.Store, string) {
	t.Helper()
	store, id := session.NewStore(t.TempDir()), turn.NewID(time.Now())
	body, err := json.Marshal(session.MessageBody{Role: session.RoleAssistant, Content: quotedWords})
	if err != nil {
		t.Fatalf("encoding the recorded turn: %v", err)
	}
	event := session.Event{ID: "0f2c9b1a-3333-4aaa-8bbb-ccccccc41099", Attempt: session.FirstAttempt, Kind: session.EventMessage, Body: body}
	if err := store.Write(session.Header{ID: id, Root: id, At: time.Now()}, []session.Event{event}); err != nil {
		t.Fatalf("recording %s: %v", id, err)
	}
	return store, id
}

func quotingTurn(t *testing.T, reference string) (turn.Config, turn.Row) {
	t.Helper()
	store, recorded := sessionCarryingOneTurn(t)
	opts := armOpts(t)
	opts.task, opts.turnID = "say what you meant in [quote"+reference+"]", recorded
	built, _, err := buildRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	model := &scriptedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "quote", Arguments: json.RawMessage(`{"id":"` + reference + `"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "answered"},
	}}
	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription, sessions: store})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	return config, row
}

func TestATurnCallsQuoteThroughTheToolsTheRunBuildsAndGetsTheRecordedWordsBack(t *testing.T) {
	config, row := quotingTurn(t, quoteReference)
	if !slices.ContainsFunc(config.Tools.Definitions(), func(one llm.Tool) bool { return one.Name == "quote" }) {
		t.Fatal("the run builds no quote tool, so the rule still tells the model to do something it cannot do")
	}
	var answers []string
	for _, message := range row.Conversation {
		if message.Role == llm.RoleTool {
			answers = append(answers, message.Content)
		}
	}
	if len(answers) != 1 {
		t.Fatalf("the turn came back with %d tool answers, want the one quote: %q", len(answers), answers)
	}
	if !strings.Contains(answers[0], quotedWords) {
		t.Fatalf("the model was handed %q, want the recorded words", answers[0])
	}
	t.Logf("the model was sent: %s", answers[0])
}

func TestAnUnresolvableReferenceComesBackToTheModelAsAnErrorRatherThanATurn(t *testing.T) {
	_, row := quotingTurn(t, "#000000")
	called := row.Steps[0].ToolCalls[0]
	if called.Error == "" {
		t.Fatalf("an id nothing ends with was answered rather than refused: %+v", called)
	}
	for _, want := range []string{"nothing in this session ends with that id", "ask for the reference again"} {
		if !strings.Contains(called.Error, want) {
			t.Fatalf("the refusal does not say %q: %s", want, called.Error)
		}
	}
	t.Logf("the model was sent: error: %s", called.Error)
}

func TestTheThreeToolArmStaysThreeToolsWithNoQuoteInIt(t *testing.T) {
	store, recorded := sessionCarryingOneTurn(t)
	opts := armOpts(t, "--tools", toolSetThree)
	opts.turnID = recorded
	built, _, err := buildRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	config, _ := mustConfig(t, opts, built, runtime{spend: turn.SpendSubscription, sessions: store})
	var named []string
	for _, one := range config.Tools.Definitions() {
		named = append(named, one.Name)
	}
	if len(named) != 3 || slices.Contains(named, "quote") {
		t.Fatalf("the three tool arm offers %v", named)
	}
}
