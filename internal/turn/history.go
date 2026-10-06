package turn

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
)

type ForkKind string

const (
	ForkContinuation ForkKind = "continuation"
	ForkAccountSpent ForkKind = "account-spent"
	ForkCompact      ForkKind = "compact"
)

type Fork struct {
	Step          int          `json:"step"`
	Kind          ForkKind     `json:"kind"`
	Into          string       `json:"into"`
	TokensBefore  int          `json:"tokens_before"`
	TokensAfter   int          `json:"tokens_after"`
	BlockedMicros int64        `json:"blocked_micros"`
	TailMessages  int          `json:"tail_messages,omitempty"`
	Carry         recall.Carry `json:"carry"`
}

type Compaction struct {
	Step         int           `json:"step"`
	TokensBefore int           `json:"tokens_before"`
	TokensAfter  int           `json:"tokens_after"`
	Drops        []recall.Drop `json:"drops"`
}

func historyOf(messages []llm.Message) recall.Conversation {
	var conversation recall.Conversation
	calls := make(map[string]llm.ToolCall)
	step := 0
	for _, message := range messages {
		if message.Role == llm.RoleSystem {
			conversation.Instructions += message.Content
			continue
		}
		if message.Role == llm.RoleAssistant {
			step++
		}
		entry := recall.Entry{Step: step, Text: message.Content}
		for _, call := range message.ToolCalls {
			calls[call.ID] = call
			entry.Text += "\n" + call.Name + " " + string(call.Arguments)
		}
		if call, answered := calls[message.ToolCallID]; answered {
			entry.Tool, entry.SupersedeKey, entry.Handle = call.Name, call.Name+" "+string(call.Arguments), shrunkHandle(message.Content)
		}
		conversation.Entries = append(conversation.Entries, entry)
	}
	return conversation
}

const (
	shrunkPageBytes  = 600
	shrunkTitleBytes = 40
	shrunkNoteBytes  = 200
	shrunkPageMark   = " holds this page whole"
	wholePagesHeld   = 4
	wholePagesKept   = 2
)

type ShrinkGate string

const (
	ShrinkWhenFull ShrinkGate = "when-full"
	ShrinkWhenPaid ShrinkGate = "when-paid"
)

type duePage struct {
	at                        int
	tool, header, note, after string
}

func (g ShrinkGate) admit(preview recall.Config, messages []llm.Message, due []duePage) []duePage {
	switch g {
	case ShrinkWhenFull:
		return due
	case ShrinkWhenPaid:
		savedBytes := 0
		for _, page := range due {
			text := messages[page.at].Content
			savedBytes += len(text) - len(shrunkPage(text, page.header, page.note, page.after, ""))
		}
		saved := savedBytes * 1000 / preview.BytesPerThousandTokens
		if saved >= HistoryTokens(preview, messages[due[0].at:])-saved {
			return due
		}
		return nil
	}
	panic("turn: unknown shrink gate " + string(g))
}

func ShrinkPages(store *recall.Store, preview recall.Config, messages []llm.Message, gate ShrinkGate) error {
	calls := make(map[string]llm.ToolCall)
	var pages []int
	whole := 0
	for i, message := range messages {
		for _, call := range message.ToolCalls {
			calls[call.ID] = call
		}
		if name := calls[message.ToolCallID].Name; message.Role == llm.RoleTool && (name == "browser_observe" || name == "browser_act") && !strings.HasPrefix(message.Content, "error: ") {
			pages = append(pages, i)
			if shrunkHandle(message.Content) == "" {
				whole++
			}
		}
	}
	if whole <= wholePagesHeld {
		return nil
	}
	header := ""
	var due []duePage
	for _, i := range pages {
		text, call := messages[i].Content, calls[messages[i].ToolCallID]
		header = cmp.Or(pageHeader(text), header)
		if shrunkHandle(text) != "" {
			continue
		}
		if whole == wholePagesKept {
			break
		}
		whole--
		due = append(due, duePage{at: i, tool: call.Name, header: header, note: noteOf(call), after: noteAfter(messages[i+1:])})
	}
	for _, page := range gate.admit(preview, messages, due) {
		text := messages[page.at].Content
		handle, err := heldHandle(store, text)
		if err != nil {
			return fmt.Errorf("the %s result that a newer page replaced could not be held whole: %w", page.tool, err)
		}
		messages[page.at].Content = shrunkPage(text, page.header, page.note, page.after, handle)
	}
	return nil
}

func heldHandle(store *recall.Store, text string) (string, error) {
	if held, rendered := strings.CutPrefix(text, "artifact "); rendered {
		handle, _, _ := strings.Cut(held, " ")
		return handle, nil
	}
	elided, err := recall.Elide(store, recall.Config{}, []byte(text), true)
	return elided.Reference.ID, err
}

type overflowShrink struct {
	results     int
	bytesBefore int
	bytesAfter  int
}

func (s overflowShrink) String() string {
	return fmt.Sprintf("shrank %d old tool result(s) to an artifact handle each, %d bytes of messages to %d", s.results, s.bytesBefore, s.bytesAfter)
}

type shrinkCause string

const (
	causeOverflow shrinkCause = "the context window overflowed"
	causeCompact  shrinkCause = "/compact ran"
)

func shrinkOverflow(artifacts Artifacts, messages []llm.Message, sentTokens, windowTokens int) (overflowShrink, error) {
	target := sentTokens
	if windowTokens > 0 {
		target = min(target, windowTokens)
	}
	return shrinkTo(artifacts, messages, sentTokens, target*konst.TurnOverflowKeepPercent/100, causeOverflow)
}

type Compacted struct {
	Results      int
	TokensBefore int
	TokensAfter  int
	Messages     []llm.Message
}

func CompactCarried(artifactDir, wire string, carried []llm.Message) (Compacted, error) {
	artifacts, err := NewArtifacts(artifactDir, true)
	if err != nil {
		return Compacted{}, err
	}
	artifacts.preview = artifacts.preview.OnWire(wire)
	shrunk := slices.Clone(carried)
	before := HistoryTokens(artifacts.preview, shrunk)
	shrink, err := shrinkTo(artifacts, shrunk, before, 0, causeCompact)
	if err != nil {
		return Compacted{}, err
	}
	return Compacted{Results: shrink.results, TokensBefore: before, TokensAfter: HistoryTokens(artifacts.preview, shrunk), Messages: shrunk}, nil
}

func RecordCarried(store *session.Store, from string, compacted Compacted, at time.Time) (string, error) {
	if _, err := store.Header(from); err != nil {
		return "", err
	}
	ended, err := store.Open(session.Header{ID: from})
	if err != nil {
		return "", err
	}
	was, into := ended.Header(), session.NewEventID()
	log, err := store.Open(session.Header{ID: into, At: at, ForkKind: string(ForkCompact), Root: cmp.Or(was.Root, was.ID), Task: was.Task,
		Wire: was.Wire, Model: was.Model, CarriedFrom: &session.Carried{Session: was.ID, Event: was.Head}})
	if err != nil {
		return "", errors.Join(err, ended.Close())
	}
	written := &record{store: store, log: log, scope: into, turn: into, said: map[string]string{}}
	request := ""
	for _, message := range compacted.Messages {
		if message.Role == llm.RoleAssistant {
			request = session.NewEventID()
		}
		written.message(message, request, nil)
	}
	if len(written.failed) > 0 {
		return "", errors.Join(errors.New(strings.Join(written.failed, "; ")), log.Close(), ended.Close())
	}
	edited := ended.Edit(func(header *session.Header) {
		header.ForkedInto, header.EndedAt, header.EndReason = into, &at, session.EndedByFork
		header.ForkTokensBefore, header.ForkTokensAfter = compacted.TokensBefore, compacted.TokensAfter
	})
	return into, errors.Join(edited, log.Close(), ended.Close())
}

func shrinkTo(artifacts Artifacts, messages []llm.Message, estimate, target int, cause shrinkCause) (overflowShrink, error) {
	lastStep := len(messages) - 1
	for lastStep >= 0 && messages[lastStep].Role != llm.RoleAssistant {
		lastStep--
	}
	shrink := overflowShrink{bytesBefore: messagesBytes(messages)}
	names := make(map[string]string)
	for i, message := range messages[:max(lastStep, 0)] {
		for _, call := range message.ToolCalls {
			names[call.ID] = call.Name
		}
		if estimate <= target {
			break
		}
		if message.Role != llm.RoleTool || len(message.Content) < artifacts.preview.CompactFloorBytes ||
			recall.AlreadyDropped(message.Content) || shrunkHandle(message.Content) != "" {
			continue
		}
		note, err := shrunkNote(artifacts, names[message.ToolCallID], message.Content, cause)
		if err != nil {
			return overflowShrink{}, err
		}
		estimate -= artifacts.preview.MessageTokens(message.Content) - artifacts.preview.MessageTokens(note)
		messages[i].Content = note
		shrink.results++
	}
	shrink.bytesAfter = messagesBytes(messages)
	return shrink, nil
}

func shrunkNote(artifacts Artifacts, tool, result string, cause shrinkCause) (string, error) {
	if !artifacts.handles {
		return "the " + tool + " result that stood here was dropped when " + string(cause) + ", and nothing holds it: run the call again if it is still needed.", nil
	}
	handle, err := heldHandle(artifacts.store, result)
	if err != nil {
		return "", fmt.Errorf("the %s result to shrink after %s could not be held whole: %w", tool, cause, err)
	}
	return "the " + tool + " result that stood here is held whole in artifact " + handle +
		": it was shrunk when " + string(cause) + ". call artifact_fetch with that handle, an offset and a length to read any range of it.", nil
}

func HistoryTokens(preview recall.Config, messages []llm.Message) int {
	return len(messages)*konst.MessageFramingTokens + messagesBytes(messages)*1000/preview.BytesPerThousandTokens
}

func messagesBytes(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += len(message.Content)
		for _, call := range message.ToolCalls {
			total += len(call.Arguments)
		}
	}
	return total
}

func noteOf(call llm.ToolCall) string {
	var said struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal(call.Arguments, &said)
	return said.Note
}

func noteAfter(messages []llm.Message) string {
	for _, message := range messages {
		if len(message.ToolCalls) == 0 {
			continue
		}
		for _, call := range message.ToolCalls {
			if note := noteOf(call); note != "" {
				return note
			}
		}
		return ""
	}
	return ""
}

func shrunkPage(text, header, note, after, handle string) string {
	var kept []string
	if tab, rest, found := strings.Cut(strings.TrimPrefix(header, "tab "), " "); found {
		url, title, _ := strings.Cut(rest, " ")
		kept = append(kept, "tab "+tab+" "+url+" "+runeSafeHead(title, shrunkTitleBytes))
	}
	kept = append(kept, "note: "+runeSafeHead(note, shrunkNoteBytes))
	if after != "" {
		kept = append(kept, "after reading: "+runeSafeHead(after, shrunkNoteBytes))
	}
	held := "artifact " + handle + shrunkPageMark
	lead, _, _ := strings.Cut(text, "<<<")
	var acts []string
	for _, line := range strings.Split(lead, "\n") {
		number, _, found := strings.Cut(line, ". ")
		if _, err := strconv.Atoi(number); found && err == nil || strings.HasPrefix(line, "ran ") {
			acts = append(acts, line)
		}
	}
	room := shrunkPageBytes - len(strings.Join(kept, "\n")) - len(held) - 2
	if did := runeSafeHead(strings.Join(acts, "\n"), max(room, 0)); did != "" {
		kept = append(kept, did)
	}
	return strings.Join(append(kept, held), "\n")
}

func pageHeader(text string) string {
	if _, body, found := strings.Cut(text, " begins>>>\n"); found {
		text = body
	}
	line, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(line, "tab ") {
		return ""
	}
	return line
}

func shrunkHandle(text string) string {
	handle, shrunk := strings.CutSuffix(text[strings.LastIndex(text, "\n")+1:], shrunkPageMark)
	if handle, named := strings.CutPrefix(handle, "artifact "); named && shrunk {
		return handle
	}
	return ""
}

func forkHistory(artifacts Artifacts, budget recall.Budget, task string, messages []llm.Message, forced ForkKind, number, most int) (*Fork, []llm.Message, error) {
	if err := ShrinkPages(artifacts.store, artifacts.preview, messages, ShrinkWhenPaid); err != nil {
		return nil, nil, err
	}
	ended := historyOf(messages)
	kind := forced
	if kind == "" {
		if !budget.Crossed(artifacts.preview, ended) {
			return nil, messages, nil
		}
		kind = ForkContinuation
	}
	started := time.Now()
	counted := "this is fork " + strconv.Itoa(number)
	if most > 0 {
		counted += " of at most " + strconv.Itoa(most) + ", and the turn stops at the cap"
	}
	var opening []llm.Message
	for _, message := range messages {
		if message.Role == llm.RoleSystem {
			opening = append(opening, message)
		}
	}
	opening = append(opening, llm.Message{Role: llm.RoleUser, Content: task, Origin: llm.Origin{Source: sourceForkTask, TakenAt: started}})
	carried := func(tail []llm.Message) ([]llm.Message, recall.Carry, error) {
		ended.HeldWhole = len(tail)
		carry, err := recall.DistilledCarry(artifacts.store, artifacts.preview, ended)
		carry.Text = counted + ".\n" + carry.Text
		return slices.Concat(opening, []llm.Message{{Role: llm.RoleUser, Content: carry.Text, Origin: llm.Origin{Source: sourceForkCarry, TakenAt: started}}}, tail), carry, err
	}
	begun, carry, err := carried(nil)
	if err != nil {
		return nil, nil, err
	}
	room := (budget.Bands.Target() - budget.Tokens(artifacts.preview, historyOf(begun))) * konst.ForkTailRoomPercent / 100
	if tail := messages[tailStart(artifacts.preview, messages, room):]; len(tail) > 0 {
		kept, keptCarry, err := carried(tail)
		if err != nil {
			return nil, nil, err
		}
		if !budget.Crossed(artifacts.preview, historyOf(kept)) {
			begun, carry = kept, keptCarry
		}
	}
	return &Fork{
		Kind:          kind,
		TokensBefore:  budget.Tokens(artifacts.preview, ended),
		TokensAfter:   budget.Tokens(artifacts.preview, historyOf(begun)),
		BlockedMicros: time.Since(started).Microseconds(),
		TailMessages:  len(begun) - len(opening) - 1,
		Carry:         carry,
	}, begun, nil
}

func tailStart(preview recall.Config, messages []llm.Message, room int) int {
	start := len(messages)
	floor := slices.IndexFunc(messages, func(message llm.Message) bool { return message.Role == llm.RoleAssistant })
	if floor < 0 {
		return start
	}
	for i := len(messages) - 1; i >= floor && len(messages[i].Images) == 0; i-- {
		if messages[i].Role == llm.RoleTool {
			continue
		}
		if HistoryTokens(preview, messages[i:]) > room {
			break
		}
		start = i
		if messages[i].Role == llm.RoleAssistant {
			room = min(room, konst.ForkTailTokens)
		}
	}
	return start
}

func compactHistory(artifacts Artifacts, budget recall.Budget, step int, messages []llm.Message) (*Compaction, error) {
	before := historyOf(messages)
	tokensBefore := recall.Measure(artifacts.preview, budget.Bands, before).Total()
	after, drops, err := recall.Compact(artifacts.store, artifacts.preview, budget.Bands, before)
	if err != nil {
		return nil, err
	}
	if len(drops) == 0 {
		return nil, nil
	}
	offset := len(messages) - len(after.Entries)
	for i, entry := range after.Entries {
		if entry.Handle != "" {
			messages[offset+i].Content = entry.Text
		}
	}
	return &Compaction{
		Step:         step,
		TokensBefore: tokensBefore,
		TokensAfter:  recall.Measure(artifacts.preview, budget.Bands, after).Total(),
		Drops:        drops,
	}, nil
}
