package tui

import (
	"cmp"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	roster "tofu/internal/agent/subagents"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/store/session"
	"tofu/internal/sys"
	"tofu/internal/tools/shell/process"
	app "tofu/internal/ui/tui/app"
	"tofu/internal/ui/tui/frame/frame"
	"tofu/internal/ui/tui/panels/shells"
	"tofu/internal/ui/tui/panels/subagent"
)

const (
	recentTurns   = 30
	everyTurn     = 0
	spawnTool     = "spawn"
	diffHeader    = "--- "
	createdHeader = "created "
	continuation  = "continuation"
	commandMark   = "/"
	quotedColumns = 60
	commandSettle = 20 * time.Millisecond
)

type recordedCall struct {
	ID          string          `json:"id"`
	Call        string          `json:"call"`
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	Command     string          `json:"command"`
	SubAgentID  string          `json:"sub_agent_id"`
	ExitCode    *int            `json:"exit_code"`
	Error       string          `json:"error"`
	ResultBytes int             `json:"result_bytes"`
}

type recordedStep struct {
	AssistantText    string         `json:"assistant_text"`
	ToolCalls        []recordedCall `json:"tool_calls"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	CacheReadTokens  int            `json:"cache_read_tokens"`
}

type recordedResult struct {
	content string
	outcome string
}

type recorded struct {
	store   *session.Store
	headers []session.Header
}

type replayedTurn struct {
	task   string
	events []app.Event
}

type sitting struct {
	turns       []replayedTurn
	events      int
	largestEdit int
	largestRows int
	subAgents   int
	outcomes    map[host.ToolOutcome]int
}

func readRecorded(b *testing.B) recorded {
	b.Helper()
	store := session.NewStore(sys.RecordedStateDir("sessions"))
	listing, err := store.Listing()
	if err != nil {
		b.Fatal(err)
	}
	if len(listing.Sessions) == 0 {
		b.Skip("no recorded session under " + sys.RecordedStateDir("sessions"))
	}
	headers := slices.Clone(listing.Sessions)
	slices.Reverse(headers)
	return recorded{store: store, headers: headers}
}

func (r recorded) read(b *testing.B, id string) ([]recordedStep, []recordedResult) {
	b.Helper()
	events, err := r.store.Body(id)
	if err != nil {
		b.Fatal(err)
	}
	var steps []recordedStep
	var order []string
	said := map[string]recordedResult{}
	for _, event := range events {
		switch event.Kind {
		case session.EventStep:
			var step recordedStep
			if err := json.Unmarshal(event.Body, &step); err != nil {
				b.Fatal(err)
			}
			steps = append(steps, step)
		case session.EventMessage:
			var message session.MessageBody
			if err := json.Unmarshal(event.Body, &message); err != nil {
				b.Fatal(err)
			}
			for _, call := range message.ToolCalls {
				if !slices.Contains(order, call.ID) {
					order = append(order, call.ID)
				}
			}
			if message.Role == session.RoleTool {
				said[message.ToolCallID] = recordedResult{content: message.Content, outcome: message.ToolOutcome}
			}
		}
	}
	results := make([]recordedResult, len(order))
	for index, id := range order {
		results[index] = said[id]
	}
	return steps, results
}

func (r recorded) sitting(b *testing.B, last int) sitting {
	b.Helper()
	var top []session.Header
	for _, header := range r.headers {
		if header.Parent == "" || header.ForkKind == continuation {
			top = append(top, header)
		}
	}
	if last != everyTurn {
		top = top[max(0, len(top)-last):]
	}
	replayed := sitting{outcomes: map[host.ToolOutcome]int{}}
	edits := 0
	for _, header := range top {
		steps, results := r.read(b, header.ID)
		name := header.ID
		if header.Name != nil {
			name = *header.Name
		}
		events := []app.Event{{Kind: app.EventSession, Text: name, ID: header.Root}}
		in, out, cached, call := 0, 0, 0, 0
		var subAgents []subagent.Row
		for _, step := range steps {
			in, out, cached = in+step.PromptTokens, out+step.CompletionTokens, cached+step.CacheReadTokens
			events = append(events,
				app.Event{Kind: app.EventRequesting},
				app.Event{Kind: app.EventStats, Model: header.Model, TokensIn: in, TokensOut: out, CacheRead: cached},
				app.Event{Kind: app.EventContext, Context: frame.Context{Used: step.PromptTokens, Budget: header.ContextCeiling}})
			if text := strings.TrimSpace(step.AssistantText); text != "" {
				events = append(events, app.Event{Kind: app.EventText, Text: text})
			}
			for _, recordedCall := range step.ToolCalls {
				id := session.EventIDFor(header.ID, cmp.Or(recordedCall.ID, strconv.Itoa(call)))
				events = append(events, app.Event{Kind: app.EventToolCall, ID: id, Tool: recordedCall.Tool, Text: recordedCall.Command,
					Detail: argText(recordedCall.Args, "command"), Promote: recordedCall.Tool == spawnTool})
				answered := resultAt(results, call)
				content, outcome := answered.content, outcomeOf(b, answered.outcome, recordedCall)
				if recordedCall.SubAgentID != "" {
					subAgents = append(subAgents, r.subAgentRow(b, header, recordedCall))
					events = append(events, app.Event{Kind: app.EventSubAgent, SubAgents: slices.Clone(subAgents)})
				}
				result := app.Event{Kind: app.EventToolResult, ID: id, Text: summary(content, recordedCall.ResultBytes), Bytes: recordedCall.ResultBytes,
					Outcome: outcome}
				if !outcome.Failed() && strings.HasPrefix(content, diffHeader) {
					result.Diff = content
				}
				if !outcome.Failed() && strings.HasPrefix(content, createdHeader) {
					result.Created = argText(recordedCall.Args, "content")
				}
				if rows := strings.Count(result.Diff+result.Created, "\n"); result.Diff+result.Created != "" {
					if rows > replayed.largestRows {
						replayed.largestEdit, replayed.largestRows = edits, rows
					}
					edits++
				}
				events = append(events, result)
				replayed.outcomes[outcome]++
				call++
			}
		}
		replayed.subAgents += len(subAgents)
		events = append(events, app.Event{Kind: app.EventDone, Text: header.Outcome})
		replayed.turns = append(replayed.turns, replayedTurn{task: header.Task, events: events})
		replayed.events += len(events)
	}
	return replayed
}

func (r recorded) subAgentRow(b *testing.B, header session.Header, spawn recordedCall) subagent.Row {
	b.Helper()
	part := header.ID + session.TurnMark + spawn.SubAgentID
	steps, results := r.read(b, part)
	if len(steps) == 0 {
		b.Fatal("bench/suites/perf/tui: recorded spawn " + spawn.Call + " in session " + header.ID + " names sub-agent " + spawn.SubAgentID + ", and the session records nothing it did")
	}
	row := subagent.Row{Name: spawn.SubAgentID, Total: konst.TurnMaxSteps, State: roster.Finished}
	events, err := r.store.Events(header.ID)
	if err != nil {
		b.Fatal(err)
	}
	for _, event := range events {
		var spawned session.SpawnBody
		if event.Kind == session.EventSpawn && json.Unmarshal(event.Body, &spawned) == nil && spawned.Agent == spawn.SubAgentID {
			row.Agent, row.Model, row.Owns, row.Ticket, row.Doing = spawned.Definition, spawned.Model, spawned.Owns, spawned.Ticket, spawned.Mission
		}
	}
	for _, run := range header.Agents {
		for _, state := range roster.States() {
			if run.Agent == spawn.SubAgentID && state.String() == run.Status {
				row.State = state
			}
		}
	}
	call := 0
	for _, step := range steps {
		row.Steps++
		row.Tokens += step.PromptTokens + step.CompletionTokens
		row.Report = cmp.Or(step.AssistantText, row.Report)
		for _, recordedCall := range step.ToolCalls {
			row.Calls = append(row.Calls, subagent.Call{ID: session.EventIDFor(part, recordedCall.ID), Tool: recordedCall.Tool,
				Text: recordedCall.Command, Result: summary(resultAt(results, call).content, recordedCall.ResultBytes)})
			call++
		}
	}
	return row
}

func resultAt(results []recordedResult, call int) recordedResult {
	if call >= len(results) {
		return recordedResult{}
	}
	return results[call]
}

func outcomeOf(b *testing.B, said string, call recordedCall) host.ToolOutcome {
	b.Helper()
	if said == "" && (call.Error != "" || call.ExitCode != nil && *call.ExitCode != 0) {
		return host.ToolFailed
	}
	var outcome host.ToolOutcome
	if err := outcome.UnmarshalJSON([]byte(strconv.Quote(cmp.Or(said, string(host.ToolRan))))); err != nil {
		b.Fatal("bench/suites/perf/tui: recorded call " + call.ID + ": " + err.Error())
	}
	return outcome
}

func argText(raw json.RawMessage, key string) string {
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	text, _ := fields[key].(string)
	return text
}

func summary(content string, bytes int) string {
	trimmed := strings.TrimRight(content, "\n")
	lines := strings.Count(trimmed, "\n") + 1
	switch {
	case trimmed == "":
		return strconv.Itoa(bytes) + " bytes"
	case lines == 1 && len([]rune(trimmed)) <= quotedColumns:
		return trimmed
	}
	return strconv.Itoa(lines) + " lines, " + strconv.Itoa(len(content)) + " bytes"
}

func recordedShells() []shells.Entry {
	registry := shell.OpenAt(filepath.Join(filepath.Dir(filepath.Dir(sys.SourceRoot())), "admin-template", ".tofu", "shells"))
	found, err := registry.List()
	if err != nil {
		return nil
	}
	entries := make([]shells.Entry, 0, len(found))
	for _, one := range found {
		log, _ := registry.Tail(one.Name, shell.DefaultTail)
		entries = append(entries, shells.Entry{Name: one.Name, Command: one.Command, State: shells.State(one.State), Started: one.Started,
			Ended: one.Ended, ExitCode: one.ExitCode, PID: one.PID, Dir: one.Dir, Owner: one.Owner, Log: log})
	}
	return entries
}

func run(built *app.App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			for _, inner := range batch {
				run(built, inner)
			}
			return
		}
		if msg != nil {
			built.Update(msg)
		}
	case <-time.After(commandSettle):
	}
}

func replayedApp(b *testing.B, replayed sitting) *app.App {
	b.Helper()
	at := time.Date(2026, 9, 25, 6, 30, 0, 0, time.UTC)
	built := app.New(app.Options{
		Repo:    "bob",
		Branch:  "develop",
		Release: "bench",
		Keymap:  filepath.Join(b.TempDir(), "shortcuts.json"),
		Wires: func() []app.Wire {
			return []app.Wire{{Name: "anthropic", Model: "claude-opus-5", Provider: "claude-sub"}}
		},
		Now:    func() time.Time { return at },
		Turn:   func(context.Context, app.Pick, string, app.CalledFromInsideTheTurnAndNeverAfterItReturns) {},
		Shells: recordedShells,
	})
	built.Init()
	built.Update(tea.WindowSizeMsg{Width: benchWidth, Height: benchHeight})
	for _, turn := range replayed.turns {
		built.Update(tea.PasteMsg{Content: turn.task})
		built.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if strings.HasPrefix(turn.task, commandMark) {
			built.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		}
		for _, event := range turn.events {
			built.Update(event)
		}
		_, cmd := built.Update(app.Closed{})
		run(built, cmd)
	}
	return built
}
