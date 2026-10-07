package host

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
)

type recordedEvent struct {
	event session.Event
	body  any
}

func recordSession(t *testing.T, store *session.Store, header session.Header, events ...recordedEvent) {
	t.Helper()
	log, err := store.Open(header)
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range events {
		if _, err := log.Append(recorded.event, recorded.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
}

func spawnedBy(call, agent, mission string, at time.Time) []recordedEvent {
	args, _ := json.Marshal(map[string]string{"agent": "research", "task": mission})
	return []recordedEvent{
		{session.Event{At: at, Kind: session.EventToolCall, Call: call}, session.CallBody{Tool: "spawn", Args: args}},
		{session.Event{At: at, Kind: session.EventSpawn, Call: call}, session.SpawnBody{Agent: agent, Definition: "research", Mission: mission}},
		{session.Event{At: at, Kind: session.EventToolResult, Call: call}, session.ResultBody{Content: agent + " is running in the background"}},
	}
}

func reported(agent string, state roster.State, text string, at time.Time) recordedEvent {
	return recordedEvent{session.Event{At: at, Agent: agent, Kind: session.EventReport}, session.ReportBody{State: state.String(), Text: text}}
}

func TestAResumedChatDrawsEachSubAgentReportAsAReportAndNeverAsSomethingThePersonTyped(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	forked := time.Date(2026, 10, 6, 20, 42, 56, 0, time.UTC)
	parent, child := session.NewEventID(), session.NewEventID()
	early := "research-1 ran as research\n\nsub-agent research-1 is finished, done, stopped after 3 steps"
	late := "sub-agent research-2 is errored: the wire closed\n\nresearch-2 ran as research"
	again := "research-1 ran as research\n\nsub-agent research-1 is finished, done, the second round"
	typed := "and keep the tests short"
	env := "<env>working directory: " + project + "</env>\n\n"
	joined, noted := env+late+"\n\n"+typed, env+again+"\n\nthe person has not asked tofu to check a sub-agent's work"
	asked := func(content, origin string) recordedEvent {
		return recordedEvent{session.Event{At: forked.Add(5 * time.Minute), Kind: session.EventMessage}, session.MessageBody{Role: session.RoleUser, Content: content, Origin: origin}}
	}
	recordSession(t, store, session.Header{ID: parent, At: forked.Add(-time.Hour), ForkedInto: child},
		append(spawnedBy("call-1", "research-1", "read the frame loop", forked.Add(-time.Hour)),
			reported("research-1", roster.Finished, early, forked.Add(-time.Minute)))...)
	recordSession(t, store, session.Header{ID: child, At: forked, CarriedFrom: &session.Carried{Session: parent}},
		append(spawnedBy("call-2", "research-2", "read the shaders", forked.Add(time.Minute)),
			spawnedBy("call-3", "research-3", "still running at quit", forked.Add(2*time.Minute))[1],
			reported("research-2", roster.Errored, late, forked.Add(3*time.Minute)),
			reported("research-1", roster.Finished, again, forked.Add(4*time.Minute)),
			asked(joined, "sub-agent report and typed by the person"),
			asked(noted, "sub-agent report"))...)
	spawn := func(call, task string) llm.ToolCall {
		args, _ := json.Marshal(map[string]string{"agent": "research", "task": task})
		return llm.ToolCall{ID: call, Name: "spawn", Arguments: args}
	}
	carry := Carry{Session: child, Messages: []llm.Message{
		{Role: llm.RoleUser, Content: env + early},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{spawn("call-2", "read the shaders"), spawn("call-3", "still running at quit")}},
		{Role: llm.RoleTool, ToolCallID: "call-2", Content: "research-2 is running in the background"},
		{Role: llm.RoleTool, ToolCallID: "call-3", Content: "research-3 is running in the background"},
		{Role: llm.RoleUser, Content: joined},
		{Role: llm.RoleUser, Content: noted},
	}}

	chat := resumedChat(carry, project)

	var tasks, drawn []string
	var last []SubAgentRow
	linked := map[string]bool{}
	for _, event := range chat {
		switch event.Kind {
		case EventTask:
			tasks = append(tasks, event.Text)
		case EventToolCall:
			if event.Promote {
				linked[event.ID] = true
			}
		case EventSubAgent:
			if len(linked) < len(event.SubAgents) {
				t.Errorf("a snapshot of %d sub-agents came after %d spawn calls, so the interface links a report to a spawn it never drew", len(event.SubAgents), len(linked))
			}
			for _, row := range event.SubAgents {
				if row.Report != "" && !slices.Contains(drawn, row.Name+": "+row.Report) {
					drawn = append(drawn, row.Name+": "+row.Report)
				}
			}
			last = event.SubAgents
		}
	}
	if !slices.Equal(tasks, []string{typed}) {
		t.Errorf("the chat drew %q as typed by the person, want only %q", tasks, typed)
	}
	want := []string{"research-1: " + early, "research-2: " + late, "research-1: " + again}
	if !slices.Equal(drawn, want) {
		t.Errorf("the reports drawn are %q, want %q", drawn, want)
	}
	for _, row := range last {
		if row.State == roster.Working {
			t.Errorf("%s reads as working on a resumed chat where nothing runs", row.Name)
		}
	}
}
