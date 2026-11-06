package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/agent/context/recall"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/memory/tree"
	"tofu/internal/store/session"
	"tofu/internal/tools/agents/note"
	"tofu/internal/tools/contract"
)

const (
	HistoryToolName = "history"
	forkStateLead   = "where the work stood when the last session ended, as the agent wrote it there:\n"
	forkLookupLine  = "to see any call named below again, call history with mode lookup and the call id as its anchor instead of reading or running it again: it returns that call and its result whole, from session %s or any earlier session of its line.\n"
)

type historyMode string

const (
	historyQuote  historyMode = "quote"
	historyRecall historyMode = "recall"
	historyZoom   historyMode = "zoom"
	historyLookup historyMode = "lookup"
)

func foldedIntoHistory() []historyMode {
	return []historyMode{historyQuote, historyRecall, historyZoom}
}

const (
	episodeOfPerson = tree.KindPerson
	episodeOfLead   = tree.KindLead
	episodeOfReport = tree.KindWork
)

type RecordedAsk func(ctx context.Context, why, wire string, model Model, request core.Request) (core.Decision, error)

type episodeKeeper interface {
	Episodes(budget int, saidElsewhere []string) (string, error)
	Keep(kind, text string) error
	Compact(ctx context.Context, ask RecordedAsk) error
}

func standsAtTheHead(message core.Message) bool {
	return message.Role == core.RoleSystem || message.Origin.Source == core.SourceMemoryView || message.Origin.Source == core.SourceEpisodes
}

func isReport(origin core.Origin) bool {
	return origin.Source == core.SourceReport || origin.Source == core.SourceReportFor
}

func episodeKind(origin core.Origin) string {
	switch {
	case isReport(origin):
		return episodeOfReport
	case saidByThePerson(origin):
		return episodeOfPerson
	}
	return ""
}

func withoutEpisodes(messages []core.Message) []core.Message {
	return slices.DeleteFunc(messages, func(message core.Message) bool { return message.Origin.Source == core.SourceEpisodes })
}

func withEpisodes(messages []core.Message, view string) []core.Message {
	if view == "" {
		return messages
	}
	return afterTheHead(messages, core.Message{Role: core.RoleUser, Content: view, Origin: core.Origin{Source: core.SourceEpisodes}})
}

func withNotesKept(beforeFork, begun []core.Message) []core.Message {
	latest, held := note.Latest(beforeFork)
	if !held {
		return begun
	}
	return afterTheHead(slices.DeleteFunc(begun, note.Kept), latest)
}

func afterTheHead(messages []core.Message, message core.Message) []core.Message {
	at := slices.IndexFunc(messages, func(message core.Message) bool { return !standsAtTheHead(message) })
	if at < 0 {
		at = len(messages)
	}
	return slices.Insert(messages, at, message)
}

type quietAskKey struct{}

func AskedQuietly(ctx context.Context) bool {
	quiet, _ := ctx.Value(quietAskKey{}).(bool)
	return quiet
}

func forkStateAsk() string {
	return "this conversation has reached its context budget, and a new session will continue the work from a short carry. " +
		"write the working state for it, under these headings: goal; decided, and why; done; half done; next, in order; open questions. " +
		"if the person asked something that is not answered yet, quote it exactly. name files, functions, commands and errors exactly, " +
		"and copy no file contents, scripts or command output: the next session can look up every call by its id. " +
		"answer with the working state and nothing else, in at most " + strconv.Itoa(konst.ForkStateWords) + " words."
}

func carryState(fork *Fork, begun []core.Message, state, session string) {
	at := slices.IndexFunc(begun, func(message core.Message) bool {
		return message.Origin.Source == core.SourceForkCarry && message.Content == fork.Carry.Text
	})
	if state != "" {
		fork.Carry = fork.Carry.WithoutFiles()
	}
	counted, rest, _ := strings.Cut(fork.Carry.Text, "\n")
	text := counted + "\n"
	if state != "" {
		text += forkStateLead + state + "\n"
	}
	if session != "" {
		text += fmt.Sprintf(forkLookupLine, session)
	}
	fork.Carry.Text, fork.Carry.State = text+rest, state
	begun[at].Content = fork.Carry.Text
}

func picturesLeftOut(before, after []core.Message) []recall.Drop {
	pictured := map[string]bool{}
	for _, message := range after {
		if message.Role == core.RoleTool && len(message.Images) > 0 {
			pictured[message.ToolCallID] = true
		}
	}
	calls := map[string]core.ToolCall{}
	var drops []recall.Drop
	step := 0
	for _, message := range before {
		if message.Role == core.RoleAssistant {
			step++
		}
		for _, call := range message.ToolCalls {
			calls[call.ID] = call
		}
		if message.Role != core.RoleTool || len(message.Images) == 0 || pictured[message.ToolCallID] {
			continue
		}
		drops = append(drops, pictureDrop(step, calls[message.ToolCallID], message, len(message.Images)*konst.ImageTokens))
	}
	return drops
}

type historyTool struct {
	sessions *session.Store
	current  func() string
	modes    []contract.Tool
}

func NewHistoryTool(sessions *session.Store, recorded string, modes ...contract.Tool) contract.Tool {
	return historyTool{sessions: sessions, current: func() string { return recorded }, modes: modes}
}

func withHistory(tools []contract.Tool, history historyTool) []contract.Tool {
	var kept []contract.Tool
	for _, tool := range tools {
		if slices.Contains(foldedIntoHistory(), historyMode(tool.Name())) {
			history.modes = append(history.modes, tool)
		} else {
			kept = append(kept, tool)
		}
	}
	if history.sessions == nil && len(history.modes) == 0 {
		return kept
	}
	return append(kept, history)
}

func historyRenamed(tool string) (string, bool) {
	if !slices.Contains(append(foldedIntoHistory(), historyLookup), historyMode(tool)) {
		return "", false
	}
	return "is a mode of " + HistoryToolName + " now: call " + HistoryToolName + " with mode " + tool + " and what you asked for as its anchor", true
}

func (mode historyMode) said() string {
	switch mode {
	case historyQuote:
		return "quote: anchor is a reference the person wrote, [quote#abcd], or the short id inside it. it returns that recorded turn or tool call of this session or any it was forked from: cite its words rather than paraphrase, and when an id matches no item or two, ask for the reference again rather than quoting the nearest"
	case historyRecall:
		return "recall: anchor is a regular expression over the raw items of the memory stores, store narrowing it to one, and every match comes back word for word as store id+n|kind: text. an anchor that is a reference, [tool#abc12], or one of your own call ids returns that call instead"
	case historyZoom:
		return "zoom: anchor is a view line's id+n and store its store. it returns the two lines that line sums up, and a line of one item the item itself, word for word: zoom until you have the part you need whole before you act on it, guess at it or ask about it, since a summary line is a pointer, not the thing"
	case historyLookup:
		return "lookup: anchor is a call id a fork carry names. it returns that call and its result whole, from session, this one when left out, or any earlier session of its line"
	}
	panic("history: unknown mode " + string(mode))
}

func (historyTool) Name() string { return HistoryToolName }

func (t historyTool) offered() []string {
	var modes []string
	for _, mode := range t.modes {
		modes = append(modes, mode.Name())
	}
	if t.sessions != nil {
		modes = append(modes, string(historyLookup))
	}
	return modes
}

func (t historyTool) Definition() core.Tool {
	properties := map[string]any{
		"mode":   map[string]any{"type": "string", "enum": t.offered()},
		"anchor": map[string]any{"type": "string"},
	}
	for _, mode := range t.modes {
		parameters, _ := mode.Definition().Parameters.(map[string]any)
		held, _ := parameters["properties"].(map[string]any)
		if store, holds := held["store"]; holds {
			properties["store"] = store
		}
	}
	if t.sessions != nil {
		properties["session"] = map[string]any{"type": "string"}
	}
	said := []string{"answers from the record of this work by mode, each mode taking one anchor:"}
	for _, mode := range t.offered() {
		said = append(said, historyMode(mode).said())
	}
	return core.Tool{
		Name:        HistoryToolName,
		Description: strings.Join(said, "\n"),
		Parameters:  map[string]any{"type": "object", "properties": properties, "required": []string{"mode", "anchor"}},
	}
}

func (t historyTool) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Mode    historyMode `json:"mode"`
		Anchor  string      `json:"anchor"`
		Store   string      `json:"store"`
		Session string      `json:"session"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("history: arguments are not the expected shape: %w", err)
	}
	if args.Mode == historyLookup && t.sessions != nil {
		return t.lookup(args.Session, args.Anchor)
	}
	at := slices.IndexFunc(t.modes, func(mode contract.Tool) bool { return mode.Name() == string(args.Mode) })
	if at < 0 {
		return contract.Result{}, fmt.Errorf("history: mode %q is not offered to this turn, which offers %s", args.Mode, strings.Join(t.offered(), ", "))
	}
	var asked any
	switch args.Mode {
	case historyQuote:
		asked = map[string]string{"id": args.Anchor}
	case historyRecall:
		asked = map[string]string{"store": args.Store, "regex": args.Anchor}
	case historyZoom:
		id, n, split := strings.Cut(args.Anchor, "+")
		line, lineErr := strconv.Atoi(id)
		count, countErr := strconv.Atoi(n)
		if !split || lineErr != nil || countErr != nil {
			return contract.Result{}, fmt.Errorf("history: zoom takes a view line's id+n as its anchor, for instance 4+2, not %q", args.Anchor)
		}
		asked = map[string]any{"store": args.Store, "id": line, "n": count}
	default:
		panic("history: a mode folded in without its arguments: " + string(args.Mode))
	}
	encoded, _ := json.Marshal(asked)
	return t.modes[at].Run(ctx, encoded)
}

func (t historyTool) lookup(asked, call string) (contract.Result, error) {
	if asked == "" {
		asked = t.current()
	}
	seen := map[string]bool{}
	for id := asked; id != "" && !seen[id]; {
		seen[id] = true
		events, err := t.sessions.Events(id)
		if err != nil {
			return contract.Result{}, fmt.Errorf("lookup: session %s: %w", id, err)
		}
		var called session.CallBody
		var result session.ResultBody
		for _, event := range events {
			switch {
			case event.Call != call:
			case event.Kind == session.EventToolCall:
				err = json.Unmarshal(event.Body, &called)
			case event.Kind == session.EventToolResult:
				err = json.Unmarshal(event.Body, &result)
			}
			if err != nil {
				return contract.Result{}, fmt.Errorf("lookup: call %s in session %s does not parse: %w", call, id, err)
			}
		}
		if called.Tool != "" {
			return contract.Result{Content: "the call: " + called.Tool + " " + string(called.Args) + "\nits result:\n" + result.Content,
				Command: "call " + call + " in session " + id}, nil
		}
		header, err := t.sessions.Header(id)
		if err != nil {
			return contract.Result{}, fmt.Errorf("lookup: session %s: %w", id, err)
		}
		id = ""
		if header.CarriedFrom != nil {
			id = header.CarriedFrom.Session
		}
	}
	return contract.Result{}, fmt.Errorf("lookup: no call %s in session %s or any session it was carried from", call, asked)
}
