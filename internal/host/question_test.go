package host

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"tofu/internal/turn"
)

func questionServer(t *testing.T, declared bool) (*server, turn.PersonForm, chan string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{})
	s := &server{ServeConfig: ServeConfig{Host: h}, box: newOutbox(), pending: map[string]ApprovalRequest{}, asked: map[string]QuestionRequest{},
		items: items{tools: map[string]openTool{}, agents: map[string]SubAgentRow{}}}
	if _, err := s.initialize(InitializeParams{Client: "desk", Capabilities: slices.DeleteFunc([]string{"questions"}, func(string) bool { return !declared })}); err != nil {
		t.Fatal(err)
	}
	awaited := make(chan string, 1)
	emit := func(event Event) {
		s.publish(event)
		if event.Kind == EventAwaitPerson {
			awaited <- event.ID
		}
	}
	return s, askForm(emit, h.questions), awaited
}

func twoLibraries() []turn.PersonQuestion {
	first := 0
	return []turn.PersonQuestion{{ID: "lib", Header: "HTTP client", Question: "Which HTTP client?", Type: turn.QuestionChoice, Recommended: &first,
		Options: []turn.PersonOption{{Label: "net/http", Description: "standard"}, {Label: "resty", Description: "retries"}}}}
}

func sent(s *server) []string {
	s.box.mu.Lock()
	defer s.box.mu.Unlock()
	var methods []string
	for _, out := range s.box.queue {
		methods = append(methods, out.msg.Method)
	}
	return methods
}

func TestAClientThatAnswersQuestionsGetsTheRequestTheStateAndTheResolution(t *testing.T) {
	s, form, awaited := questionServer(t, true)
	got := make(chan []turn.PersonReply, 1)
	go func() {
		replies, _ := form(t.Context(), twoLibraries(), 2*time.Minute)
		got <- replies
	}()
	id := <-awaited
	if !slices.Contains(sent(s), QuestionMethod) {
		t.Fatalf("the client was sent %v, want %s", sent(s), QuestionMethod)
	}
	state := s.state()
	if len(state.Questions) != 1 || state.Questions[0].Question != id || state.Questions[0].Blocking || state.Questions[0].WaitMs != 120000 {
		t.Fatalf("session.state lists %+v, want the open question, not blocking, 120000 ms", state.Questions)
	}
	unoffered := s.answered(json.RawMessage(`"`+id+`"`), json.RawMessage(`{"outcome":"submitted","answers":[{"id":"lib","chosen":["requests"]}]}`))
	approval := s.answered(json.RawMessage(`"`+id+`"`), json.RawMessage(`{"decision":"allow_once"}`))
	if len(s.state().Questions) != 1 || unoffered == nil || approval == nil {
		t.Fatalf("an answer naming no offered option (%v), or an approval answer (%v), closed the question or got no error", unoffered, approval)
	}
	if err := s.answered(json.RawMessage(`"`+id+`"`), json.RawMessage(`{"outcome":"submitted","answers":[{"id":"lib","chosen":["resty"]}]}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case replies := <-got:
		if len(replies) != 1 || replies[0].Chosen[0] != "resty" {
			t.Fatalf("the lead got %+v, want resty", replies)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the answer never reached the lead")
	}
	if !slices.Contains(sent(s), "question.resolved") || len(s.state().Questions) != 0 {
		t.Fatalf("after the answer the client was sent %v and the state lists %d questions", sent(s), len(s.state().Questions))
	}
}

func TestAClientWithoutQuestionsIsUndeliveredAtOnce(t *testing.T) {
	s, form, _ := questionServer(t, false)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := form(ctx, twoLibraries(), 0)
	var undelivered turn.QuestionUndelivered
	if !errors.As(err, &undelivered) {
		t.Fatalf("got %v, want undelivered at once, without the blocking wait", err)
	}
	if slices.Contains(sent(s), QuestionMethod) || len(s.state().Questions) != 0 {
		t.Fatalf("a client that never declared questions was sent %v", sent(s))
	}
}

func TestAQuestionOutlivesTheTurnInTheStatus(t *testing.T) {
	var feed statusFeed
	feed.follow(Event{Kind: EventAwaitPerson, ID: "q1", Tool: turn.AskPersonToolName, Text: "Which HTTP client?", Questions: twoLibraries()})
	feed.follow(Event{Kind: EventTurnEnded})
	lead := feed.records(nil)[0]
	if lead.State != "blocked" || lead.Kind != "question" {
		t.Fatalf("after the turn ended the lead reads %s:%s, want blocked:question while the question is open", lead.State, lead.Kind)
	}
}
