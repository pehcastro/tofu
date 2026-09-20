package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
)

func recordingConfig(t *testing.T, model Model, tools Registry) (Config, *session.Store) {
	t.Helper()
	store := session.NewStore(t.TempDir())
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          tools,
		Task:           "say pong",
		Wire:           "anthropic",
		Caps:           Caps{MaxSteps: 10},
		ResultBytesCap: 4096,
		Sessions:       store,
	}, store
}

func kinds(events []session.Event) []string {
	seen := make([]string, len(events))
	for index, event := range events {
		seen[index] = string(event.Kind)
	}
	return seen
}

func TestATurnCancelledAfterTwoToolCallsLeavesBothCallsAndBothResultsOnDisk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := &cancellingTool{cancel: cancel, after: 2}
	config, store := recordingConfig(t, &contextModel{decisions: []llm.Decision{twoCalls()}}, NewRegistry(tool))

	row, err := Run(ctx, config)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled run returned %v, want a cancellation", err)
	}

	events, err := store.Body(row.ID)
	if err != nil {
		t.Fatalf("reading the record of %s: %v", row.ID, err)
	}
	recorded, err := ConversationFrom(events)
	if err != nil {
		t.Fatalf("reading the conversation back: %v", err)
	}
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleTool}
	if roles := rolesOf(recorded); !slices.Equal(roles, want) {
		t.Fatalf("the record of a cancelled turn reads %v, want %v", roles, want)
	}
	if len(recorded[1].ToolCalls) != 2 {
		t.Fatalf("the recorded assistant message carries %d calls, want both", len(recorded[1].ToolCalls))
	}
	for index, result := range recorded[2:] {
		if result.Content != "file contents" {
			t.Fatalf("recorded result %d reads %q, want the body the tool returned", index+1, result.Content)
		}
	}
	header, err := store.Header(row.ID)
	if err != nil {
		t.Fatalf("reading the header of %s: %v", row.ID, err)
	}
	if header.Outcome != OutcomeError.String() {
		t.Fatalf("the header of a cancelled turn reads outcome %q", header.Outcome)
	}
	t.Logf("%s: %v, both results on disk, header outcome %s", row.ID, kinds(events), header.Outcome)
}

func TestTheRecordExistsBeforeTheFirstModelRequestReturns(t *testing.T) {
	asked := make(chan string)
	release := make(chan struct{})
	config, store := recordingConfig(t, askFunc(func(context.Context, llm.Request) (llm.Decision, error) {
		asked <- "the first request is out"
		<-release
		return messageDecision(), nil
	}), NewRegistry())
	config.NewID = func() string { return "turn-under-way" }

	done := make(chan Row, 1)
	go func() {
		row, err := Run(context.Background(), config)
		if err != nil {
			t.Error("run: " + err.Error())
		}
		done <- row
	}()

	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		t.Fatal("the model was never asked")
	}
	header, err := store.Header("turn-under-way")
	if err != nil {
		t.Fatalf("the record does not exist while the first request is still out: %v", err)
	}
	if header.Task != "say pong" || header.Root != "turn-under-way" {
		t.Fatalf("the record under way reads %+v, want the task and its own root", header)
	}
	t.Logf("while the first request was still out, the store already held %s, task %q, outcome %q",
		header.ID, header.Task, header.Outcome)
	close(release)
	<-done
}

func TestATurnThatEndsInAnErrorIsRecorded(t *testing.T) {
	config, store := recordingConfig(t, &errorModel{err: errors.New("the wire is down")}, NewRegistry())
	row, err := Run(context.Background(), config)
	if err == nil {
		t.Fatal("the run was expected to fail")
	}
	header, readErr := store.Header(row.ID)
	if readErr != nil {
		t.Fatalf("reading the record of the failed turn: %v", readErr)
	}
	if header.Outcome != OutcomeError.String() {
		t.Fatalf("the header reads outcome %q, want error", header.Outcome)
	}
	events, readErr := store.Body(row.ID)
	if readErr != nil {
		t.Fatalf("reading the body of the failed turn: %v", readErr)
	}
	if !slices.Contains(kinds(events), string(session.EventOutcome)) {
		t.Fatalf("the record of a failed turn holds %v, want an outcome", kinds(events))
	}
	t.Logf("%s failed with %v and reads back as %v, outcome %s", row.ID, err, kinds(events), header.Outcome)
}

func TestEveryCapIsRecorded(t *testing.T) {
	for _, capped := range []struct {
		name    string
		caps    Caps
		gate    Gate
		outcome Outcome
	}{
		{name: "the step cap", caps: Caps{MaxSteps: 1}, outcome: OutcomeStepCap},
		{name: "the decision cap", caps: Caps{MaxSteps: 10, MaxDecisions: 1}, gate: allowingGate{}, outcome: OutcomeDecisionCap},
	} {
		t.Run(capped.name, func(t *testing.T) {
			tool := &stubTool{name: "read", result: Result{Content: "file contents"}}
			config, store := recordingConfig(t, &contextModel{decisions: []llm.Decision{
				twoCalls(), twoCalls(), messageDecision(),
			}}, NewRegistry(tool))
			config.Caps, config.Gate = capped.caps, capped.gate

			row, err := Run(context.Background(), config)
			if err != nil {
				t.Fatalf("the capped run: %v", err)
			}
			if row.Outcome != capped.outcome {
				t.Fatalf("outcome = %s, want %s", row.Outcome, capped.outcome)
			}
			header, readErr := store.Header(row.ID)
			if readErr != nil {
				t.Fatalf("reading the record: %v", readErr)
			}
			if header.Outcome != capped.outcome.String() {
				t.Fatalf("the header reads outcome %q, want %s", header.Outcome, capped.outcome)
			}
			events, readErr := store.Body(row.ID)
			if readErr != nil {
				t.Fatalf("reading the body: %v", readErr)
			}
			t.Logf("%s: %s, recorded as %v", row.ID, capped.outcome, kinds(events))
		})
	}
}

func TestTheRecordedConversationIsTheMessageListTheNextTurnWouldSend(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	config, store := recordingConfig(t, &contextModel{decisions: []llm.Decision{
		twoCalls(), messageDecision(),
	}}, NewRegistry(tool))
	config.System = "the system prompt"

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the first run: %v", err)
	}
	events, err := store.Body(row.ID)
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	recorded, err := ConversationFrom(events)
	if err != nil {
		t.Fatalf("reading the conversation back: %v", err)
	}

	if len(recorded) != len(row.Conversation) {
		t.Fatalf("the record reads back %d messages and the turn carried %d: %v against %v",
			len(recorded), len(row.Conversation), rolesOf(recorded), rolesOf(row.Conversation))
	}
	for index, want := range row.Conversation {
		read := recorded[index]
		if read.Role != want.Role || read.Content != want.Content || read.ToolCallID != want.ToolCallID {
			t.Fatalf("message %d reads %+v, want %+v", index, read, want)
		}
		if len(read.ToolCalls) != len(want.ToolCalls) {
			t.Fatalf("message %d reads %d tool calls, want %d", index, len(read.ToolCalls), len(want.ToolCalls))
		}
		for call, recordedCall := range read.ToolCalls {
			wanted := want.ToolCalls[call]
			if recordedCall.ID != wanted.ID || recordedCall.Name != wanted.Name || string(recordedCall.Arguments) != string(wanted.Arguments) {
				t.Fatalf("call %d of message %d reads %+v, want %+v", call, index, recordedCall, wanted)
			}
		}
	}

	second := &contextModel{decisions: []llm.Decision{messageDecision()}}
	config.Model, config.Task, config.History = second, "please continue", recorded
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("the send built from the record: %v", err)
	}
	sent := second.requests[0].Messages
	if len(sent) != len(recorded)+2 {
		t.Fatalf("the send built from the record carried %d messages, want the system prompt, %d recorded and the new task",
			len(sent), len(recorded))
	}
	t.Logf("%d messages on disk, %d sent: %v", len(recorded), len(sent), rolesOf(sent))
}

func TestACompleteTurnStillRecordsWhatItRecordsToday(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}
	config, store := recordingConfig(t, model, NewRegistry(tool))
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the run: %v", err)
	}

	events, err := store.Body(row.ID)
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	var steps []StepRow
	var outcome Row
	for _, event := range events {
		switch event.Kind {
		case session.EventStep:
			var step StepRow
			if err := json.Unmarshal(event.Body, &step); err != nil {
				t.Fatalf("a step does not read back: %v", err)
			}
			steps = append(steps, step)
		case session.EventOutcome:
			if err := json.Unmarshal(event.Body, &outcome); err != nil {
				t.Fatalf("the outcome does not read back: %v", err)
			}
		case session.EventMessage:
		}
	}
	if len(steps) != len(row.Steps) {
		t.Fatalf("the record holds %d steps and the turn ran %d", len(steps), len(row.Steps))
	}
	if steps[0].ToolCalls[0].Tool != "read" || steps[0].ToolCalls[0].Command != "read a.txt" {
		t.Fatalf("the recorded tool call reads %+v", steps[0].ToolCalls[0])
	}
	if outcome.ID != row.ID || outcome.Outcome != OutcomeStopped || outcome.Spend != SpendAPIKey || outcome.Schema != SchemaVersion {
		t.Fatalf("the recorded outcome reads %+v", outcome.Summary())
	}
	if outcome.Steps != nil {
		t.Fatalf("the outcome event repeats %d steps that are already their own events", len(outcome.Steps))
	}
	t.Logf("%s recorded as %v", row.ID, kinds(events))
}

func TestBothFixturesFromBeforeTheLiveRecordStillRead(t *testing.T) {
	store := session.NewStore("../session/testdata")
	for _, fixture := range []struct {
		id      string
		outcome string
	}{
		{id: "turn-18d68bcceb3d56e8", outcome: "stopped"},
		{id: "turn-18d6d295dfac466c-f2", outcome: "forked"},
	} {
		header, err := store.Header(fixture.id)
		if err != nil {
			t.Fatalf("the header of %s: %v", fixture.id, err)
		}
		if header.Outcome != fixture.outcome {
			t.Fatalf("%s reads outcome %q, want %s", fixture.id, header.Outcome, fixture.outcome)
		}
		events, err := store.Body(fixture.id)
		if err != nil {
			t.Fatalf("the body of %s: %v", fixture.id, err)
		}
		messages, err := ConversationFrom(events)
		if err != nil {
			t.Fatalf("reading %s as a conversation: %v", fixture.id, err)
		}
		if len(messages) != 0 {
			t.Fatalf("%s was written before any message was recorded and read back %d messages", fixture.id, len(messages))
		}
		t.Logf("%s: outcome %s, %d events, %s", fixture.id, header.Outcome, len(events), strings.Join(kinds(events), " "))
	}
}

func TestWritingDuringTheTurnCostsLessThanATwentiethOfATurnWithTwentyToolCalls(t *testing.T) {
	const (
		calls              = 20
		repeats            = 9
		shortestRealTurnMS = 99906
		budget             = 0.05
		budgetForWriting   = 50 * time.Millisecond
	)
	measure := func(recording bool) time.Duration {
		runs := make([]time.Duration, repeats)
		for repeat := range runs {
			decisions := make([]llm.Decision, calls)
			for index := range decisions {
				decisions[index] = toolCallDecision(llm.ToolCall{
					ID: "call-" + string(rune('a'+index)), Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`),
				})
			}
			decisions = append(decisions, messageDecision())
			config, _ := recordingConfig(t, &contextModel{decisions: decisions},
				NewRegistry(&stubTool{name: "read", result: Result{Content: strings.Repeat("x", 4000)}}))
			config.Caps = Caps{MaxSteps: calls + 2}
			if !recording {
				config.Sessions = nil
				config.EndedSession = func(Row) error { return nil }
			}
			started := time.Now()
			if _, err := Run(context.Background(), config); err != nil {
				t.Fatalf("the measured run: %v", err)
			}
			runs[repeat] = time.Since(started)
		}
		slices.Sort(runs)
		return runs[len(runs)/2]
	}

	silent, recorded := measure(false), measure(true)
	writing := recorded - silent
	realTurn := time.Duration(shortestRealTurnMS) * time.Millisecond
	t.Logf("median of %d runs of %d tool calls: %v not recording, %v recording, %v of it writing, which is %.4f%% of %v, the shortest turn this machine has on disk, and %.1f%% of the stubbed one",
		repeats, calls, silent, recorded, writing,
		float64(writing)/float64(realTurn)*100, realTurn, float64(writing)/float64(recorded)*100)
	if writing > budgetForWriting {
		t.Fatalf("writing cost %v, over the %v allowed, which is far inside %.0f%% of %v but tight enough that a header rewritten per event would not fit",
			writing, budgetForWriting, budget*100, realTurn)
	}
}

type askFunc func(context.Context, llm.Request) (llm.Decision, error)

func (a askFunc) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	return a(ctx, request)
}

type allowingGate struct{}

func (allowingGate) Decide(context.Context, GateRequest) (GateDecision, error) {
	return GateDecision{ID: "decision-1"}, nil
}
