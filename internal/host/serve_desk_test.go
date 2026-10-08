package host

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/cron"
	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
)

func TestDeskACronFireThatStartsATurnSaysSoOnlyByItsOriginAndDeleteAllEmptiesTheBook(t *testing.T) {
	play := func(ctx context.Context, _ Pick, task string, live Live) {
		if !strings.Contains(task, "c1 fired") {
			return
		}
		select {
		case <-live.Steering:
		case <-ctx.Done():
		}
	}
	c, live, _ := serving(t, play, ServeConfig{})
	c.ask("1", "initialize", `{"client":"desk"}`)
	c.answer("1", &InitializeResult{})
	c.ask("2", "session.open", `{}`)
	c.answer("2", &SessionOpenResult{})
	for id, line := range map[string]string{"3a": "/loop 1m say hi", "3b": "/loop 2m say bye"} {
		c.ask(id, "cron.command", `{"line":`+quoted(line)+`}`)
		c.answer(id, &CronCommandResult{})
	}

	go live.fire(live.cron.Due(context.Background(), time.Now().Add(3*time.Minute), cron.Tick))

	var notes []string
	var origins []Origin
	for ended := 0; ended < 2; {
		line := c.until(func(line wireLine) bool {
			return line.Method == "note" || line.Method == "turn.started" || line.Method == "turn.completed"
		}, "two cron turns")
		switch line.Method {
		case "note":
			var said Said
			_ = json.Unmarshal(line.Params, &said)
			notes = append(notes, said.Text)
		case "turn.started":
			var started TurnStarted
			_ = json.Unmarshal(line.Params, &started)
			origins = append(origins, started.Origin)
		case "turn.completed":
			ended++
		}
	}
	if len(origins) != 2 || origins[0].Kind != OriginCron || origins[1].Kind != OriginCron {
		t.Errorf("the fires started turns with origins %+v, want two of kind cron", origins)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "c2 fired") {
		t.Errorf("the notes were %q, want only c2's, the fire that joined a running turn, since a fire that starts a turn is told by its origin", notes)
	}

	c.ask("4", "cron.command", `{"line":"/cron delete all"}`)
	var deleted CronCommandResult
	c.answer("4", &deleted)
	c.ask("5", "query.cron", `{}`)
	var left CronState
	c.answer("5", &left)
	if len(left.Jobs) != 0 || left.Live != 0 {
		t.Errorf("after /cron delete all, query.cron holds %+v, want no job", left)
	}
	if !strings.Contains(deleted.Note, "c1") || !strings.Contains(deleted.Note, "c2") {
		t.Errorf("/cron delete all answered %q, want both jobs named", deleted.Note)
	}
}

func TestDeskQueryUsageAnswersWhatItLastReadAtOnceAndSaysHowOld(t *testing.T) {
	var reads atomic.Int32
	verb := func(args []string) (VerbResult, error) {
		if args[0] != "usage" {
			return VerbResult{}, nil
		}
		reads.Add(1)
		time.Sleep(300 * time.Millisecond)
		return VerbResult{OK: true, Data: json.RawMessage(`{"state":"serving","providers":[{"provider":"claude-sub","state":"serving"}],"spend_limit":"none"}`)}, nil
	}
	c, _, _ := serving(t, nil, ServeConfig{Verb: verb})
	c.ask("1", "initialize", `{"client":"desk"}`)
	c.answer("1", &InitializeResult{})
	c.ask("2", "query.usage", `{}`)
	var cold UsageAnswer
	c.answer("2", &cold)

	asked := time.Now()
	c.ask("3", "query.usage", `{}`)
	var warm UsageAnswer
	c.answer("3", &warm)
	took := time.Since(asked)

	if took > 150*time.Millisecond {
		t.Errorf("a warm query.usage took %s, want the reading it already holds", took)
	}
	if got := reads.Load(); got != 1 {
		t.Errorf("two query.usage read the providers %d times, want once", got)
	}
	if warm.ReadAt.IsZero() || !warm.ReadAt.Equal(cold.ReadAt) || warm.AgeMs < 0 || len(warm.Providers) != 1 {
		t.Errorf("the warm answer is %+v after %+v, want the same reading with its age", warm, cold)
	}
}

func TestDeskAnEndedSubAgentSaysWhenItEndedTheSameLiveAndOnReplay(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	began := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	spawned, ended := began.Add(time.Second), began.Add(4*time.Second+250*time.Millisecond)
	id, turnID := session.NewEventID(), "turn-20261008-090000"
	run := session.AgentRun{Agent: "research-1", Definition: "research", SpawnTurn: turnID, Status: roster.Finished.String(), StartedAt: spawned, EndedAt: &ended}
	report := "sub-agent research-1 is finished, read the frame loop"
	recordSession(t, store, session.Header{ID: id, At: began, Agents: []session.AgentRun{run}},
		append(spawnedBy("call-1", "research-1", "read the frame loop", spawned.Add(-time.Millisecond)),
			reported("research-1", roster.Finished, report, ended.Add(time.Millisecond)))...)
	args, _ := json.Marshal(map[string]string{"agent": "research", "task": "read the frame loop"})
	carry := Carry{Session: id, Messages: []llm.Message{
		{Role: llm.RoleUser, Content: "read it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "spawn", Arguments: args}}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Content: "research-1 is running in the background"},
		{Role: llm.RoleUser, Content: report},
	}}
	replay := newItems(id)
	replayed := agentEnded(t, replay.all(resumedChat(carry, project)))

	held := &roster.Roster{}
	if err := held.Hold(roster.SubAgent{ID: "research-1", Agent: "research", State: roster.Working, Started: spawned.Add(-time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	held.Reached("research-1", roster.Finished, report)
	watch := &watcher{held: held, now: func() time.Time { return ended.Add(time.Minute) }, ran: func(string) session.AgentRun { return run }}
	lively := newItems(id)
	lively.turn = "turn-later"
	seen := agentEnded(t, lively.translate(Event{Kind: EventSubAgent, SubAgents: watch.subAgents()}, ended.Add(time.Minute)))

	for name, got := range map[string]*AgentEnded{"replayed": replayed, "live": seen} {
		if !got.EndedAt.Equal(ended) || got.DurationMs != 3250 || got.Turn != turnID {
			t.Errorf("%s agent.ended = endedAt %s, durationMs %d, turn %q; want %s, 3250 and %q, the run the session recorded", name, got.EndedAt, got.DurationMs, got.Turn, ended, turnID)
		}
	}
}

func agentEnded(t *testing.T, lines []outgoing) *AgentEnded {
	t.Helper()
	for _, line := range lines {
		if line.msg.Method == "agent.ended" {
			return line.msg.Params.(*AgentEnded)
		}
	}
	t.Fatal("no agent.ended among the lines")
	return nil
}
