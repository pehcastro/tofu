package session

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func eventOf(t *testing.T, kind EventKind, body any) Event {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal a %s: %v", kind, err)
	}
	return Event{ID: string(kind) + "-1", Kind: kind, Body: raw}
}

func oneOfEachKind(t *testing.T) []Event {
	t.Helper()
	return []Event{
		eventOf(t, EventStep, StepBody{Index: 1, AssistantText: "looking"}),
		eventOf(t, EventMessage, MessageBody{Role: RoleUser, Content: "go on"}),
		eventOf(t, EventRead, Read{Step: 1, Tool: readTool, Source: "main.go", Bytes: 12}),
		eventOf(t, EventOutcome, map[string]string{"outcome": "stopped"}),
		eventOf(t, EventAttachment, Attachment{File: "shot.png", Bytes: 9, Format: "png"}),
		eventOf(t, EventPrompt, PromptBody{System: "be brief", Tools: []string{"read"}}),
	}
}

func oneOfEachWrittenKind(t *testing.T) []Event {
	t.Helper()
	asked := func(event Event, call string) Event {
		event.Request, event.Call = "r1", call
		return event
	}
	return []Event{
		eventOf(t, EventTurnStart, TurnStart{Task: "read main.go"}),
		eventOf(t, EventPrompt, PromptBody{System: "be brief", Tools: []string{"read"}}),
		eventOf(t, EventMessage, MessageBody{Role: RoleUser, Content: "read main.go"}),
		asked(eventOf(t, EventMessage, MessageBody{Role: RoleAssistant}), ""),
		asked(eventOf(t, EventToolCall, CallBody{Tool: readTool, Args: json.RawMessage(`{"path":"main.go"}`)}), "c1"),
		eventOf(t, EventSpawn, SpawnBody{Agent: "go-dev-1"}),
		eventOf(t, EventAgentEnd, AgentEndBody{Status: "finished"}),
		eventOf(t, EventReport, ReportBody{State: "finished", Text: "sub-agent go-dev-1 is finished"}),
		asked(eventOf(t, EventHook, map[string]string{"event": "PreToolUse"}), "c1"),
		asked(eventOf(t, EventToolResult, ResultBody{Content: "package main", ResultBytes: 12}), "c1"),
		asked(eventOf(t, EventRequest, StepBody{Index: 1, AssistantText: "looking"}), ""),
		asked(eventOf(t, EventCompaction, map[string]any{"compaction": map[string]int{"step": 1}}), ""),
		eventOf(t, EventAttachment, Attachment{File: "shot.png", Bytes: 9, Format: "png"}),
		eventOf(t, EventTurnEnd, map[string]string{"outcome": "stopped"}),
		eventOf(t, EventNotice, NoticeBody{Text: "the gate is off"}),
		eventOf(t, EventListChange, ListChangeBody{Kind: "fork", After: []string{"a1"}}),
	}
}

var tracedOnly = []EventKind{EventTurnStart, EventSpawn, EventAgentEnd, EventNotice, EventListChange, EventReport, EventHook}

func readingOf(t *testing.T, reading Reading, kind EventKind) int {
	t.Helper()
	switch kind {
	case EventStep, EventRequest:
		return len(reading.Steps)
	case EventCompaction:
		return strings.Count(string(reading.Steps[0].Raw), `"compaction"`)
	case EventToolCall:
		return len(reading.Steps[0].ToolCalls)
	case EventToolResult:
		return len(reading.Messages) - 2
	case EventTurnEnd:
		return len(reading.Outcome)
	case EventMessage:
		return len(reading.Messages)
	case EventRead:
		return len(reading.Reads)
	case EventOutcome:
		return len(reading.Outcome)
	case EventAttachment:
		return len(reading.Attachments)
	case EventPrompt:
		if reading.Prompt == nil {
			return 0
		}
		return 1
	}
	t.Fatalf("the kind %q has no reading asserted here: add it to oneOfEachKind and to this switch", kind)
	return 0
}

func TestEveryDeclaredKindHasAReading(t *testing.T) {
	for _, events := range [][]Event{oneOfEachKind(t), oneOfEachWrittenKind(t)} {
		shown, err := DefaultSettings().view(events)
		if err != nil {
			t.Fatalf("view one event of every kind: %v", err)
		}
		reading, err := ReadEvents(shown)
		if err != nil {
			t.Fatalf("read one event of every kind: %v", err)
		}
		if len(reading.Unknown) > 0 {
			t.Fatalf("a declared kind read as unknown: %v", reading.Unknown)
		}
		for _, event := range events {
			if !slices.Contains(tracedOnly, event.Kind) && readingOf(t, reading, event.Kind) == 0 {
				t.Fatalf("the kind %q was read and nothing of it survived into the reading", event.Kind)
			}
		}
	}
	for _, kind := range Kinds() {
		if !slices.ContainsFunc(append(oneOfEachKind(t), oneOfEachWrittenKind(t)...), func(event Event) bool { return event.Kind == kind }) {
			t.Fatalf("the kind %q is declared and no event of it was read", kind)
		}
	}
}

func TestTheDeclaredKindsAreTheOnesTheSourceDeclares(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the package sources: %v", err)
	}
	var declared []string
	for _, source := range sources {
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", source, err)
		}
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if name, ok := value.Type.(*ast.Ident); !ok || name.Name != "EventKind" {
					continue
				}
				for _, literal := range value.Values {
					basic, ok := literal.(*ast.BasicLit)
					if !ok {
						t.Fatalf("an EventKind constant in %s is not a string literal: %v", source, literal)
					}
					text, err := strconv.Unquote(basic.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", basic.Value, err)
					}
					declared = append(declared, text)
				}
			}
		}
	}
	listed := make([]string, len(Kinds()))
	for i, kind := range Kinds() {
		listed[i] = string(kind)
	}
	slices.Sort(declared)
	slices.Sort(listed)
	if !slices.Equal(declared, listed) {
		t.Fatalf("the source declares %v and Kinds lists %v", declared, listed)
	}
}

func TestASessionCarryingAKindThisBuildDoesNotKnowStillReadsForEverythingElse(t *testing.T) {
	events := append(oneOfEachKind(t), eventOf(t, EventKind("weather"), map[string]int{"degrees": 12}))
	reading, err := ReadEvents(events)
	if err != nil {
		t.Fatalf("a session carrying one unknown kind refused to read: %v", err)
	}
	if len(reading.Steps) != 1 || len(reading.Messages) != 1 || len(reading.Reads) != 1 || reading.Prompt == nil {
		t.Fatalf("the known kinds did not survive an unknown one: %+v", reading)
	}
	if !slices.Equal(reading.Unknown, []EventKind{"weather"}) {
		t.Fatalf("the unknown kind was reported as %v", reading.Unknown)
	}
}

func TestAMessageKeepsTheBytesItWasRecordedWithAndNotOnlyTheFieldsThisPackageModels(t *testing.T) {
	recorded := json.RawMessage(`{"role":"assistant","content":"reading it","cache_write_tokens":214}`)
	reading, err := ReadEvents([]Event{{ID: "message-1", Kind: EventMessage, Body: recorded}})
	if err != nil {
		t.Fatalf("read a message carrying a field this package does not model: %v", err)
	}
	if len(reading.Messages) != 1 {
		t.Fatalf("one message event read back as %d", len(reading.Messages))
	}
	message := reading.Messages[0]
	if message.Role != RoleAssistant || message.Content != "reading it" {
		t.Fatalf("the modelled fields read back as %+v", message.MessageBody)
	}
	if string(message.Raw) != string(recorded) {
		t.Fatalf("the message reads back as %s, and a caller that needs a field beyond MessageBody cannot recover it", message.Raw)
	}
}

func TestAMessageRecordedWithThinkingAndReasoningReadsBackWithBoth(t *testing.T) {
	recorded := json.RawMessage(`{"role":"assistant","content":"reading it","thinking":"the file is small",` +
		`"reasoning":{"id":"rs_1","encrypted_content":"opaque"}}`)
	reading, err := ReadEvents([]Event{{ID: "message-1", Kind: EventMessage, Body: recorded}})
	if err != nil {
		t.Fatalf("read a message carrying thinking and reasoning: %v", err)
	}
	body := reading.Messages[0].MessageBody
	if body.Thinking != "the file is small" {
		t.Fatalf("thinking read back as %q", body.Thinking)
	}
	if body.Reasoning == nil {
		t.Fatal("reasoning read back as nothing, so a person reading the session cannot see it")
	}
	if body.Reasoning.ID != "rs_1" || body.Reasoning.EncryptedContent != "opaque" {
		t.Fatalf("reasoning read back as %+v", *body.Reasoning)
	}
}

func TestAKnownKindWithABodyThatDoesNotParseNamesTheEvent(t *testing.T) {
	_, err := ReadEvents([]Event{{Kind: EventStep, Body: json.RawMessage(`"not a step"`)}})
	if err == nil {
		t.Fatal("a step whose body is a string read without complaint")
	}
	if got := err.Error(); got[:len("event 1 is a step")] != "event 1 is a step" {
		t.Fatalf("the failure reads %q", got)
	}
}
