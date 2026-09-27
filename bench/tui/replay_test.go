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

	app "tofu/interface/tui"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/internal/konst"
	"tofu/internal/session"
	"tofu/internal/shell"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	shellsDir     = "F:/localhost/admin-template/.tofu/shells"
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
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	Command     string          `json:"command"`
	SubAgentID  string          `json:"child_id"`
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

func (r recorded) read(b *testing.B, id string) ([]recordedStep, []string) {
	b.Helper()
	events, err := r.store.Body(id)
	if err != nil {
		b.Fatal(err)
	}
	var steps []recordedStep
	var order []string
	contents := map[string]string{}
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
				contents[message.ToolCallID] = message.Content
			}
		}
	}
	results := make([]string, len(order))
	for index, id := range order {
		results[index] = contents[id]
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
	var replayed sitting
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
				content := resultAt(results, call)
				if recordedCall.SubAgentID != "" {
					subAgents = append(subAgents, r.subAgentRow(b, recordedCall, len(subAgents)))
					events = append(events, app.Event{Kind: app.EventSubAgent, SubAgents: slices.Clone(subAgents)})
				}
				result := app.Event{Kind: app.EventToolResult, ID: id, Text: summary(content, recordedCall.ResultBytes), Bytes: recordedCall.ResultBytes,
					Failed: recordedCall.Error != "" || recordedCall.ExitCode != nil && *recordedCall.ExitCode != 0}
				if !result.Failed && strings.HasPrefix(content, diffHeader) {
					result.Diff = content
				}
				if !result.Failed && strings.HasPrefix(content, createdHeader) {
					result.Created = argText(recordedCall.Args, "content")
				}
				if rows := strings.Count(result.Diff+result.Created, "\n"); result.Diff+result.Created != "" {
					if rows > replayed.largestRows {
						replayed.largestEdit, replayed.largestRows = edits, rows
					}
					edits++
				}
				events = append(events, result)
				call++
			}
		}
		events = append(events, app.Event{Kind: app.EventDone, Text: header.Outcome})
		replayed.turns = append(replayed.turns, replayedTurn{task: header.Task, events: events})
		replayed.events += len(events)
	}
	return replayed
}

func (r recorded) subAgentRow(b *testing.B, spawn recordedCall, index int) subagent.Row {
	b.Helper()
	var args struct {
		Owns    []string `json:"owns"`
		Mission string   `json:"mission"`
	}
	if err := json.Unmarshal(spawn.Args, &args); err != nil {
		b.Fatal(err)
	}
	row := subagent.Row{Name: "c" + strconv.Itoa(index+1), Owns: args.Owns, Doing: args.Mission, Total: konst.TurnMaxSteps, State: roster.Finished}
	if fields := strings.Fields(spawn.Command); len(fields) > 1 {
		for _, state := range roster.States() {
			if state.String() == strings.TrimSuffix(fields[1], ":") {
				row.State = state
			}
		}
	}
	for _, header := range r.headers {
		if !strings.HasPrefix(header.ID, spawn.SubAgentID) {
			continue
		}
		steps, results := r.read(b, header.ID)
		call := 0
		for _, step := range steps {
			row.Steps++
			row.Tokens += step.PromptTokens + step.CompletionTokens
			row.Report = cmp.Or(step.AssistantText, row.Report)
			for _, recordedCall := range step.ToolCalls {
				row.Calls = append(row.Calls, subagent.Call{ID: session.EventIDFor(header.ID, recordedCall.ID), Tool: recordedCall.Tool,
					Text: recordedCall.Command, Result: summary(resultAt(results, call), recordedCall.ResultBytes)})
				call++
			}
		}
	}
	return row
}

func resultAt(results []string, call int) string {
	if call >= len(results) {
		return ""
	}
	return results[call]
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
	registry := shell.OpenAt(shellsDir)
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
