package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
	"tofu/library"
)

const (
	recordedID      = "turn-18d7474e7fae090c"
	quoteFrameBytes = 1024
)

func spoke(t *testing.T, id, role, text string, calls ...session.MessageToolCall) session.Event {
	t.Helper()
	body, err := json.Marshal(session.MessageBody{Role: role, Content: text, ToolCalls: calls})
	if err != nil {
		t.Fatalf("encoding the recorded message: %v", err)
	}
	return session.Event{ID: id, Attempt: session.FirstAttempt, Kind: session.EventMessage, Body: body}
}

func recordedQuote(t *testing.T, events ...session.Event) tools.Quote {
	t.Helper()
	store := session.NewStore(t.TempDir())
	if err := store.Write(session.Header{ID: recordedID, Root: recordedID, At: time.Now()}, events); err != nil {
		t.Fatalf("recording %s: %v", recordedID, err)
	}
	return tools.NewQuote(store, recordedID)
}

func quoted(t *testing.T, tool tools.Quote, id string) (string, error) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		t.Fatalf("encoding the arguments: %v", err)
	}
	result, err := tool.Run(context.Background(), args)
	return result.Content, err
}

func shortOf(id string) string { return "#" + id[len(id)-6:] }

func threeTurns(t *testing.T) tools.Quote {
	t.Helper()
	return recordedQuote(t,
		spoke(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2233", session.RoleUser, "read the policy before the wire\nthen say so"),
		spoke(t, "0f2c9b1a-2222-4aaa-8bbb-bbbbbbc41099", session.RoleAssistant, "",
			session.MessageToolCall{Name: "bash", Arguments: json.RawMessage(`{"command":"curl -H 'authorization: Bearer sk-live-000111'"}`)}),
		spoke(t, "0f2c9b1a-3333-4aaa-8bbb-ccccccc41099", session.RoleAssistant, "the gate reads library/policy/shell.yaml"),
	)
}

func TestQuoteResolvesAShortIDToThatRecordedTurnsOwnWords(t *testing.T) {
	content, err := quoted(t, threeTurns(t), "[quote#de2233]")
	if err != nil {
		t.Fatalf("the reference the picker writes does not resolve: %v", err)
	}
	for _, want := range []string{"#de2233", "you", recordedID, "read the policy before the wire", "then say so"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the quote does not carry %q:\n%s", want, content)
		}
	}
}

func TestAnIDMatchingTwoTurnsAndAnIDMatchingNoneEachComeBackWithTheirOwnMessage(t *testing.T) {
	tool := threeTurns(t)
	ambiguous, twice := quoted(t, tool, "#c41099")
	if !errors.Is(twice, session.ErrEventHashAmbiguous) {
		t.Fatalf("an id two turns end with came back as %v", twice)
	}
	nearest, missing := quoted(t, tool, "#000000")
	if !errors.Is(missing, session.ErrEventHashNotFound) {
		t.Fatalf("an id nothing carries came back as %v", missing)
	}
	if ambiguous != "" || nearest != "" {
		t.Fatalf("a reference that resolves to neither still handed back %q and %q", ambiguous, nearest)
	}
	if twice.Error() == missing.Error() {
		t.Fatalf("the two failures read alike: %v", missing)
	}
	for _, said := range []error{twice, missing} {
		if !strings.Contains(said.Error(), "ask for the reference again") {
			t.Fatalf("the failure does not ask for the reference again: %v", said)
		}
	}
}

func TestQuoteReadsATurnFromEitherSideOfAFork(t *testing.T) {
	store := session.NewStore(t.TempDir())
	parent, child := "turn-18d7474e7fae0900", "turn-18d7474e7fae0901"
	if err := store.Write(session.Header{ID: parent, Root: parent, At: time.Now(), ForkedInto: child},
		[]session.Event{spoke(t, "0f2c9b1a-4444-4aaa-8bbb-ddddddaa0001", session.RoleUser, "never create .bak copies")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(session.Header{ID: child, Root: parent, At: time.Now(), CarriedFrom: &session.Carried{Session: parent}},
		[]session.Event{spoke(t, "0f2c9b1a-5555-4aaa-8bbb-eeeeeebb0002", session.RoleUser, "change the port to 9090")}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ held, id, want, in string }{
		{child, "#aa0001", "never create .bak copies", parent},
		{parent, "#bb0002", "change the port to 9090", child},
	} {
		content, err := quoted(t, tools.NewQuote(store, c.held), c.id)
		if err != nil || !strings.Contains(content, c.want) || !strings.Contains(content, c.in) {
			t.Fatalf("quoting %s from %s gave %v:\n%s", c.id, c.held, err, content)
		}
	}
	if _, err := quoted(t, tools.NewQuote(store, child), "#000000"); !errors.Is(err, session.ErrEventHashNotFound) {
		t.Fatalf("an id no session in the chain carries came back as %v", err)
	}
}

func TestATurnRecordedBeforeEventIDsExistedIsQuotableByItsPlaceInTheSession(t *testing.T) {
	tool := recordedQuote(t,
		spoke(t, "", session.RoleUser, "what did the gate decide"),
		spoke(t, "", session.RoleAssistant, "it asked, and the policy is shadow"),
	)
	content, err := quoted(t, tool, shortOf(session.EventIDFor(recordedID, "step:1")))
	if err != nil {
		t.Fatalf("a turn recorded before event ids does not resolve: %v", err)
	}
	if !strings.Contains(content, "it asked, and the policy is shadow") {
		t.Fatalf("the derived id resolved to the wrong turn:\n%s", content)
	}
}

func TestEveryPreEventIDTurnRecordedUnderThisRepositoryResolves(t *testing.T) {
	store := session.NewStore(sys.RecordedStateDir("sessions"))
	listing, err := store.Listing()
	if err != nil {
		t.Skipf("this checkout has no session store to read: %v", err)
	}
	sessions, turns := 0, 0
	for _, header := range listing.Sessions {
		talk, err := store.Conversation(header.ID)
		if err != nil {
			continue
		}
		tool := tools.NewQuote(store, header.ID)
		counted := 0
		for index, one := range talk.Said {
			if one.Event != "" {
				continue
			}
			counted++
			if _, err := quoted(t, tool, shortOf(session.EventIDFor(header.ID, "step:"+strconv.Itoa(index)))); err != nil {
				t.Fatalf("%s: turn %d carries no recorded id and does not resolve: %v", header.ID, index, err)
			}
		}
		if counted > 0 {
			sessions++
			turns += counted
		}
	}
	if turns == 0 {
		t.Skipf("none of the %d recorded sessions carries a turn from before event ids existed", len(listing.Sessions))
	}
	t.Logf("%d turns across %d of %d recorded sessions carry no recorded id, and every one of them resolves", turns, sessions, len(listing.Sessions))
}

func TestATurnTooLargeToReturnWholeIsCutAndSaysHowMuchWent(t *testing.T) {
	long := strings.Repeat("the gate reads the policy before the wire. ", konst.TurnResultBytesCap/8)
	tool := recordedQuote(t, spoke(t, "0f2c9b1a-4444-4aaa-8bbb-dddddd9f0e11", session.RoleAssistant, long))
	content, err := quoted(t, tool, "#9f0e11")
	if err != nil {
		t.Fatalf("a large turn does not resolve: %v", err)
	}
	for _, want := range []string{
		"degraded truncated",
		"the turn is " + strconv.Itoa(len(strings.TrimSpace(long))) + " bytes and the first " + strconv.Itoa(konst.TurnResultBytesCap),
		"this result is not a complete answer and must not be read as one",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("the cut is not visible in what the model sees, %q is missing:\n%s", want, content[max(len(content)-400, 0):])
		}
	}
	if len(content) > konst.TurnResultBytesCap+quoteFrameBytes {
		t.Fatalf("the cut result is %d bytes against a %d byte cap", len(content), konst.TurnResultBytesCap)
	}
}

func TestQuoteNamesTheToolsThatRanAndNeverTheirArguments(t *testing.T) {
	content, err := quoted(t, threeTurns(t), "#bbc41099")
	if err != nil {
		t.Fatalf("the turn that only called a tool does not resolve: %v", err)
	}
	if !strings.Contains(content, "bash") {
		t.Fatalf("the quote does not name the tool that ran:\n%s", content)
	}
	if strings.Contains(content, "sk-live-000111") {
		t.Fatalf("the quote handed back a credential recorded in a tool call's arguments:\n%s", content)
	}
}

func shippedQuoteRule(t *testing.T) rule.Rule {
	t.Helper()
	rules, err := rule.LoadFS(library.Files(), "library")
	if err != nil {
		t.Fatalf("loading the shipped rules: %v", err)
	}
	for _, one := range rules {
		if one.ID == "quote" {
			return one
		}
	}
	t.Fatal("the shipped rules carry no quote rule")
	return rule.Rule{}
}

func TestTheShippedQuoteRuleNamesTheToolThatResolvesTheReference(t *testing.T) {
	var registered turn.Tool = tools.NewQuote(nil, "")
	defined := registered.Definition()
	if defined.Name != registered.Name() {
		t.Fatalf("the registry keys the tool as %q and the model is told %q", registered.Name(), defined.Name)
	}
	if text := shippedQuoteRule(t).Text; !strings.Contains(text, "the "+defined.Name+" tool") {
		t.Fatalf("the rule tells the model to read a turn without naming the %s tool: %q", defined.Name, text)
	}
	shape, ok := defined.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("the tool describes its arguments as %T", defined.Parameters)
	}
	if required, ok := shape["required"].([]string); !ok || len(required) != 1 || required[0] != "id" {
		t.Fatalf("the tool the rule names takes %v, and the rule tells the model to call it with an id", shape["required"])
	}
}

func TestTheQuoteRuleReachesTheModelWhileItsModeSaysShadow(t *testing.T) {
	quote := shippedQuoteRule(t)
	if quote.Mode != rule.ModeShadow {
		t.Fatalf("the shipped rule is %s, and every shipped rule is %s until a ticket promotes the whole library", quote.Mode, rule.ModeShadow)
	}
	composed, err := turn.Compose(turn.ComposeSpec{
		Task:         "say what you meant in [quote#de2233]",
		Environment:  "windows, one repository",
		ToolGuidance: "read before you write",
		Rules:        []rule.Rule{quote},
	})
	if err != nil {
		t.Fatalf("composing a task that carries a reference: %v", err)
	}
	if !strings.Contains(composed.System(), quote.Text) {
		t.Fatalf("a shadow rule whose condition matched was held back, so mode does gate what is sent:\n%s", composed.System())
	}
}

func called(t *testing.T, scope, agent, call, tool, args string) session.Event {
	t.Helper()
	body, err := json.Marshal(session.CallBody{Tool: tool, Args: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("encoding the recorded call: %v", err)
	}
	return session.Event{ID: session.EventIDFor(scope, call), Turn: scope, Agent: agent, Call: call, Attempt: session.FirstAttempt, Kind: session.EventToolCall, Body: body}
}

func answeredCall(t *testing.T, scope, agent, call, content string) session.Event {
	t.Helper()
	body, err := json.Marshal(session.ResultBody{Content: content, ToolOutcome: session.ToolOutcomeRan, ResultBytes: len(content)})
	if err != nil {
		t.Fatalf("encoding the recorded result: %v", err)
	}
	return session.Event{Turn: scope, Agent: agent, Call: call, Attempt: session.FirstAttempt, Kind: session.EventToolResult, Body: body}
}

func TestQuoteResolvesTheCallBehindAnEditAShellAnAskAndASubAgentRow(t *testing.T) {
	const lead, later, helper = "turn-18d7474e7fae090c", "turn-18d7474e7fae0b22", "turn-18d7474e7fae0a11"
	tool := recordedQuote(t,
		spoke(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2233", session.RoleUser, "fix the port"),
		called(t, lead, "", "toolu_edit", "edit", `{"path":"internal/serve/port.go","old":"8080","new":"9090"}`),
		answeredCall(t, lead, "", "toolu_edit", "edited internal/serve/port.go"),
		called(t, lead, "", "toolu_shell", "bash", `{"command":"go test ./internal/serve/"}`),
		answeredCall(t, lead, "", "toolu_shell", "ok tofu/internal/serve"),
		called(t, lead, "", "toolu_ask", "ask_person", `{"question":"which port"}`),
		answeredCall(t, lead, "", "toolu_ask", "9090"),
		called(t, lead, "", "toolu_spawn", "spawn", `{"task":"read the routes"}`),
		answeredCall(t, lead, "", "toolu_spawn", "the routes are in routes.go"),
		called(t, helper, helper, "toolu_shell", "bash", `{"command":"go vet ./internal/routes/"}`),
		answeredCall(t, helper, helper, "toolu_shell", "vet found nothing"),
		called(t, later, "", "toolu_edit", "edit", `{"path":"internal/serve/host.go"}`),
		answeredCall(t, later, "", "toolu_edit", "edited internal/serve/host.go"),
		called(t, later, "", "toolu_run", "bash", `{"command":"go run ./cmd/serve"}`),
	)
	for _, c := range []struct{ scope, call, want, result string }{
		{lead, "toolu_edit", "internal/serve/port.go", "edited internal/serve/port.go"},
		{lead, "toolu_shell", "go test ./internal/serve/", "ok tofu/internal/serve"},
		{lead, "toolu_ask", "which port", "9090"},
		{lead, "toolu_spawn", "read the routes", "the routes are in routes.go"},
		{helper, "toolu_shell", "go vet ./internal/routes/", "vet found nothing"},
		{later, "toolu_edit", "internal/serve/host.go", "edited internal/serve/host.go"},
		{later, "toolu_run", "go run ./cmd/serve", "no result"},
	} {
		ref := "[quote" + shortOf(session.EventIDFor(c.scope, c.call)) + "]"
		content, err := quoted(t, tool, ref)
		if err != nil {
			t.Fatalf("%s %s does not resolve: %v", c.call, ref, err)
		}
		for _, want := range []string{c.want, c.result} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s %s does not carry %q:\n%s", c.call, ref, want, content)
			}
		}
	}
}

func TestATurnAndACallEndingAlikeAreAmbiguousAndAMessageIsCountedOnce(t *testing.T) {
	const lead = "turn-18d7474e7fae090c"
	call := called(t, lead, "", "toolu_edit", "edit", `{"path":"a.go"}`)
	tail := call.ID[len(call.ID)-6:]
	tool := recordedQuote(t,
		spoke(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaa"+tail, session.RoleUser, "fix it"),
		call,
		spoke(t, "0f2c9b1a-2222-4aaa-8bbb-bbbbbbde2299", session.RoleAssistant, "done"),
	)
	if _, err := quoted(t, tool, "#"+tail); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("a turn and a call sharing a tail came back as %v", err)
	}
	if content, err := quoted(t, tool, "  [quote#DE2299] "); err != nil || !strings.Contains(content, "done") {
		t.Fatalf("a message read as an utterance and as an event came back as %v:\n%s", err, content)
	}
}

func TestQuoteWithNoRecordedSessionSaysThereIsNothingToQuote(t *testing.T) {
	if _, err := quoted(t, tools.NewQuote(nil, ""), "#de2233"); err == nil {
		t.Fatal("a turn recording no session still answered a quote")
	}
}
