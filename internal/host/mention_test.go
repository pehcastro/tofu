package host

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type heldModel struct {
	asked   chan struct{}
	release chan struct{}
	replies []llm.Decision
}

func (m *heldModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if m.asked != nil {
		m.asked <- struct{}{}
		<-m.release
		m.asked = nil
	}
	if len(m.replies) == 0 {
		return llm.Decision{Build: "stub", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return reply, nil
}

type heldEngine struct {
	dir   string
	model *heldModel
}

func (e heldEngine) Prepare(start Turn, hooks Hooks) (Prepared, error) {
	store, err := session.OpenIn(e.dir)
	pick := func(context.Context) (turn.Account, error) { return turn.Account{Model: hooks.Lead(e.model)}, nil }
	return Prepared{Config: turn.Config{Accounts: turn.Accounts{Pick: pick}, Sessions: store, Session: start.Session, NewID: func() string { return start.ID }, Task: start.Task, Caps: turn.Caps{MaxSteps: 4}, Inbox: hooks.Inbox, ResultBytesCap: konst.TurnResultBytesCap, Spend: turn.SpendSubscription}, Sessions: store}, err
}

func (heldEngine) Renew() {}

func (heldEngine) OneTurnPerProject() bool { return false }

type handedRef struct {
	method  string
	ref     string
	session string
}

func TestEveryRefTheWireHandsOutResolvesToItsItem(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	model := &heldModel{asked: make(chan struct{}, 1), release: make(chan struct{}), replies: []llm.Decision{
		{Build: "stub", Outcome: llm.OutcomeToolCalls, Content: "looking first", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "nothing", Arguments: json.RawMessage(`{}`)}}},
		{Build: "stub", Outcome: llm.OutcomeMessage, Content: "the port is 8080"},
	}}
	h, _ := New(Config{Dir: project, Engine: heldEngine{dir: project, model: model}})
	t.Cleanup(h.Close)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	carry := func(id string) (Carry, error) {
		events, err := store.Body(id)
		if err != nil {
			return Carry{}, err
		}
		messages, err := turn.ConversationFrom(events)
		return Carry{Session: id, Messages: messages, Store: store}, err
	}
	c := servingHost(t, h, project, ServeConfig{Carry: carry})
	var refs []handedRef
	answered := func(id string, into any) {
		line := c.until(func(line wireLine) bool {
			var about Identity
			if json.Unmarshal(line.Params, &about) == nil && about.Ref != "" {
				kind := line.Method
				if about.Agent != "" && about.Agent != about.Item {
					kind += " by a sub-agent"
				}
				refs = append(refs, handedRef{kind, about.Ref, about.Session})
			}
			return string(line.ID) == `"`+id+`"` || id == "" && line.Method == "turn.completed"
		}, "the answer to "+id)
		if into != nil {
			_ = json.Unmarshal(line.Result, into)
		}
	}
	kinds := []string{"turn.started", "message.started", "message.completed", "message.user", "turn.steered", "tool.started", "tool.completed", "tool.started by a sub-agent", "agent.started"}
	resolved := map[string]bool{}
	resolveAll := func() {
		for index, handed := range refs {
			if !slices.Contains(kinds, handed.method) {
				continue
			}
			id := "resolve " + strconv.Itoa(index)
			c.ask(id, "mention.resolve", `{"session":"`+handed.session+`","ref":"`+handed.ref+`"}`)
			var answer MentionResolved
			c.answer(id, &answer)
			if answer.Outcome != MentionItem {
				t.Errorf("%s handed out %s in %s, and mention.resolve answered %s", handed.method, handed.ref, handed.session, answer.Outcome)
				continue
			}
			resolved[handed.method] = true
		}
		refs = nil
	}
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	answered("2", &opened)
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"which port does it use"}`)
	var running TurnResult
	answered("3", &running)
	<-model.asked
	c.ask("4", "turn.steer", `{"session":"`+opened.Session+`","expectedTurnId":"`+running.Turn+`","text":"and say it once"}`)
	answered("4", nil)
	close(model.release)
	answered("", nil)
	resolveAll()

	c.ask("5", "session.open", `{"session":"`+opened.Session+`"}`)
	answered("5", nil)
	resolveAll()

	spawning, at, lead := session.NewEventID(), time.Now(), "turn-spawning"
	args, _ := json.Marshal(map[string]string{"agent": "research", "task": "read the frame loop"})
	recordSession(t, store, session.Header{ID: spawning, At: at, Agents: []session.AgentRun{{Agent: "research-1", Definition: "research", SpawnCall: "call-9", SpawnTurn: lead, Status: roster.Finished.String(), StartedAt: at}}},
		recordedEvent{session.Event{At: at, Turn: lead, Kind: session.EventMessage}, session.MessageBody{Role: session.RoleUser, Content: "read the frame loop through a sub-agent", Origin: taskSource}},
		recordedEvent{session.Event{At: at, Turn: lead, Kind: session.EventMessage, Request: "asked-1"}, session.MessageBody{Role: session.RoleAssistant, Content: "spawning"}},
		recordedEvent{session.Event{ID: session.EventIDFor(lead, "call-9"), At: at, Turn: lead, Kind: session.EventToolCall, Call: "call-9", Request: "asked-1"}, session.CallBody{Tool: "spawn", Args: args}},
		recordedEvent{session.Event{At: at, Turn: lead, Kind: session.EventSpawn, Call: "call-9"}, session.SpawnBody{Agent: "research-1", Definition: "research", Mission: "read the frame loop"}},
		recordedEvent{session.Event{ID: session.EventIDFor("research-1", "sub-1"), At: at, Turn: lead, Agent: "research-1", Kind: session.EventToolCall, Call: "sub-1"}, session.CallBody{Tool: "read", Args: json.RawMessage(`{"path":"frame.go"}`)}},
		recordedEvent{session.Event{At: at, Turn: lead, Agent: "research-1", Kind: session.EventToolResult, Call: "sub-1"}, session.ResultBody{Content: "package frame"}},
		recordedEvent{session.Event{At: at, Turn: lead, Kind: session.EventToolResult, Call: "call-9"}, session.ResultBody{Content: "research-1 is running in the background"}},
		reported("research-1", roster.Finished, "the frame loop holds", at))
	c.ask("6", "session.open", `{"session":"`+spawning+`"}`)
	answered("6", nil)
	resolveAll()

	for _, kind := range kinds {
		if !resolved[kind] {
			t.Errorf("no %s ref resolved to an item", kind)
		}
	}
}

func TestTurnSendKeepsAMentionWhereItWasAndEveryLineCarriesTheTokenForItsItem(t *testing.T) {
	tasks := make(chan string, 1)
	play := func(_ context.Context, _ Pick, task string, live Live) {
		tasks <- task
		live.Emit(Event{Kind: EventText, Text: "it changed the port"})
	}
	c, _, _ := serving(t, play, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)

	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"what did [quote#abc123] change","mentions":["[quote#abc123]","[quote#def456]","[quote#def456]","notes/a.md"]}`)
	if task, want := <-tasks, "what did [quote#abc123] change [quote#def456] @notes/a.md"; task != want {
		t.Errorf("the lead read %q, want %q", task, want)
	}
	lines := 0
	c.until(func(line wireLine) bool {
		var about Identity
		if json.Unmarshal(line.Params, &about) == nil && about.Item != "" {
			lines++
			if about.Ref != tools.QuoteRef(about.Item) {
				t.Errorf("%s carries ref %q for item %q, want %q", line.Method, about.Ref, about.Item, tools.QuoteRef(about.Item))
			}
		}
		return line.Method == "turn.completed"
	}, "turn.completed")
	if lines == 0 {
		t.Fatal("no line carried an item, so no ref was checked")
	}
}

func TestMentionResolveTypesAMadeUpTokenAndRefusesAnEmptyOne(t *testing.T) {
	play := func(_ context.Context, _ Pick, _ string, live Live) {
		live.Emit(Event{Kind: EventText, Text: "done"})
	}
	c, _, _ := serving(t, play, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"hello"}`)
	c.until(func(line wireLine) bool { return line.Method == "turn.completed" }, "turn.completed")

	c.ask("4", "mention.resolve", `{"ref":"[quote#000000]"}`)
	var resolved MentionResolved
	c.answer("4", &resolved)
	if resolved.Outcome != MentionNotFound || resolved.Item != "" {
		t.Errorf("a made-up token resolved to %+v, want not_found and no item", resolved)
	}
	c.ask("5", "mention.resolve", `{"ref":"[quote#]"}`)
	if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"5"` }, "the answer to 5"); refused.Error == nil || refused.Error.Code != CodeBadParams {
		t.Errorf("an empty token answered %s %+v, want a bad params refusal", refused.Result, refused.Error)
	}
}
