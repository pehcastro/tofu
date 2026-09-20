package turn

import (
	"context"
	"slices"
	"testing"

	"boji/internal/llm"
	"boji/internal/session"
)

const (
	steerOne = "stop rewriting the gate, read the policy first"
	steerTwo = "and leave the changelog alone"
)

func steeringRounds(rounds ...[]string) func() []string {
	round := 0
	return func() []string {
		if round >= len(rounds) {
			return nil
		}
		taken := rounds[round]
		round++
		return taken
	}
}

func userContent(messages []llm.Message) []string {
	var said []string
	for _, message := range messages {
		if message.Role == llm.RoleUser {
			said = append(said, message.Content)
		}
	}
	return said
}

func steeredConfig(steering func() []string) (Config, *stubModel) {
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: "read", Arguments: []byte(`{}`)}),
		messageDecision(),
	}}
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(&stubTool{name: "read", result: Result{Content: "file contents"}}),
		Task:           "rewrite the gate",
		ResultBytesCap: 4096,
		Caps:           Caps{MaxSteps: 10},
		NoFork:         true,
		NoCompaction:   true,
		Steering:       steering,
	}, model
}

func TestAMessageQueuedDuringAStepReachesTheModelAtTheNextStep(t *testing.T) {
	config, model := steeredConfig(steeringRounds(nil, []string{steerOne}))
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("the model was asked %d times, want two steps", len(model.requests))
	}
	if said := userContent(model.requests[0].Messages); slices.Contains(said, steerOne) {
		t.Fatalf("the first request already carried %q, so it was not queued during the step", steerOne)
	}
	said := userContent(model.requests[1].Messages)
	if !slices.Contains(said, steerOne) {
		t.Fatalf("the second request carries the user messages %q, want the steered one among them", said)
	}
	t.Logf("step two was given %q", said)
}

func TestTwoMessagesQueuedInOneStepBothReachTheModelInOrder(t *testing.T) {
	config, model := steeredConfig(steeringRounds(nil, []string{steerOne, steerTwo}))
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	said := userContent(model.requests[1].Messages)
	at, next := slices.Index(said, steerOne), slices.Index(said, steerTwo)
	if at < 0 || next < 0 {
		t.Fatalf("the second request carries %q, want both steered messages", said)
	}
	if at > next {
		t.Fatalf("the second request carries %q, want them in the order they were queued", said)
	}
}

func TestASteeredMessageIsRecordedAsAUserMessage(t *testing.T) {
	config, _ := steeredConfig(steeringRounds(nil, []string{steerOne}))
	store := session.NewStore(t.TempDir())
	config.Sessions = store
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	events, err := store.Body(row.ID)
	if err != nil {
		t.Fatalf("reading the record of %s: %v", row.ID, err)
	}
	recorded, err := ConversationFrom(events)
	if err != nil {
		t.Fatalf("reading the conversation back: %v", err)
	}
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleUser, llm.RoleAssistant}
	if roles := rolesOf(recorded); !slices.Equal(roles, want) {
		t.Fatalf("the record reads %v, want %v with the steered message as a user message after the tool result", roles, want)
	}
	if recorded[3].Content != steerOne {
		t.Fatalf("the recorded user message after the tool result reads %q, want the steered message", recorded[3].Content)
	}
}

func TestATurnWithNothingSteeredAsksForExactlyTheSameMessages(t *testing.T) {
	plain, plainModel := steeredConfig(nil)
	steered, steeredModel := steeredConfig(steeringRounds(nil, nil, nil))
	for _, config := range []Config{plain, steered} {
		if _, err := Run(context.Background(), config); err != nil {
			t.Fatalf("the run failed: %v", err)
		}
	}
	for step := range plainModel.requests {
		want := plainModel.requests[step].Messages
		got := steeredModel.requests[step].Messages
		if !slices.Equal(rolesOf(got), rolesOf(want)) || len(got) != len(want) {
			t.Fatalf("step %d sent %v with an empty queue, want %v", step+1, rolesOf(got), rolesOf(want))
		}
	}
}
