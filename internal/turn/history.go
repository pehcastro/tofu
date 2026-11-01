package turn

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/sys"
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

func WithoutThinking(messages []llm.Message) []llm.Message {
	cleared := slices.Clone(messages)
	for i := range cleared {
		cleared[i].Thinking = llm.Thinking{}
	}
	return cleared
}

func RecordedContext(events []session.Event) (recall.Conversation, error) {
	messages, err := ConversationFrom(events)
	if err != nil {
		return recall.Conversation{}, err
	}
	reading, err := session.ReadEvents(events)
	if err != nil {
		return recall.Conversation{}, err
	}
	conversation := historyOf(messages)
	if prompt := reading.Prompt; prompt != nil {
		sent := Config{System: prompt.System, Instructions: prompt.Instructions}.SystemMessage()
		conversation.Instructions, conversation.ToolSchemas = sent+conversation.Instructions, prompt.ToolSchemas
		for _, part := range prompt.Parts {
			if part.Sent() && !part.PerTurn && part.Kind != string(rule.ConcernEnvironment) {
				conversation.Parts = append(conversation.Parts, recall.PromptPart{Name: identityPartName(part), Tokens: part.Tokens})
			}
		}
	}
	return conversation, nil
}

func identityPartName(part session.PromptPart) string {
	switch {
	case part.Kind == partInstructions:
		return recall.PartInstructionFiles
	case part.Kind == string(ConcernSkills):
		return recall.PartSkills
	case part.Agent != "":
		return recall.PartSubAgent
	case part.Rule != "":
		return recall.PartRules
	}
	return recall.PartTofu
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
		entry := recall.Entry{Step: step, Text: WithoutToolRef(message.Content), Images: len(message.Images), Memory: message.Origin.Source == llm.SourceMemoryView}
		if task, said := personTask(message); said {
			entry.Said = task
		}
		for _, call := range message.ToolCalls {
			calls[call.ID] = call
			entry.Calls += "\n" + call.Name + " " + string(call.Arguments)
		}
		if call, answered := calls[message.ToolCallID]; answered {
			entry.Tool, entry.Call, entry.SupersedeKey, entry.Handle = call.Name, call.ID, call.Name+" "+string(call.Arguments), shrunkHandle(message.Content)
		}
		conversation.Entries = append(conversation.Entries, entry)
	}
	return conversation
}

func (b *Inbox) PostFired(prompt string, fired llm.Origin) {
	fired.PostedAt = time.Now()
	b.post(inboxItem{text: prompt, origin: fired})
}

func (b *Inbox) Holds(text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.ContainsFunc(b.items, func(item inboxItem) bool { return item.text == text })
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
			text := WithoutToolRef(messages[page.at].Content)
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
		if name := calls[message.ToolCallID].Name; message.Role == llm.RoleTool && (name == "browser_observe" || name == "browser_act") && !strings.HasPrefix(WithoutToolRef(message.Content), "error: ") {
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
		text, call := WithoutToolRef(messages[i].Content), calls[messages[i].ToolCallID]
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
		ref := toolRefLine(messages[page.at].Content)
		text := strings.TrimPrefix(messages[page.at].Content, ref)
		handle, err := heldHandle(store, text)
		if err != nil {
			return fmt.Errorf("the %s result that a newer page replaced could not be held whole: %w", page.tool, err)
		}
		messages[page.at].Content = ref + shrunkPage(text, page.header, page.note, page.after, handle)
	}
	return nil
}

func heldHandle(store *recall.Store, text string) (string, error) {
	if held, rendered := strings.CutPrefix(text, "artifact "); rendered {
		handle, _, _ := strings.Cut(held, " ")
		return handle, nil
	}
	if store == nil {
		return strings.Repeat("0", konst.ArtifactHandleBytesAtMost), nil
	}
	elided, err := recall.Elide(store, recall.Config{}, []byte(text), true)
	return elided.Reference.ID, err
}

type overflowShrink struct {
	results     int
	bytesBefore int
	bytesAfter  int
	drops       []recall.Drop
}

func (s overflowShrink) String() string {
	judged := 0
	for _, drop := range s.drops {
		if drop.Reason == recall.DroppedJudged {
			judged++
		}
	}
	return fmt.Sprintf("shrank %d old tool result(s) to an artifact handle each, %d bytes of messages to %d; %s chose %d of the %d dropped and the age rule the rest",
		s.results, s.bytesBefore, s.bytesAfter, keepResultPoint, judged, len(s.drops))
}

type shrinkCause string

const (
	causeOverflow shrinkCause = "the context window overflowed"
	causeCompact  shrinkCause = "/compact ran"
	causeBudget   shrinkCause = "the conversation crossed its context budget"
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
	Kept         int
	Drops        []recall.Drop
	Before       []llm.Message
	Messages     []llm.Message
}

func CompactCarried(project string, places sys.Places, wire, task string, carried []llm.Message) (Compacted, error) {
	artifacts, err := NewArtifacts(filepath.Join(places.State, "artifacts"), true)
	if err != nil {
		return Compacted{}, err
	}
	artifacts.preview, artifacts.keep = artifacts.preview.OnWire(wire), newKeepJudge(project, places.Log, task, nil)
	if NewestTask(carried) == "" {
		artifacts.keep.unfound = "no task was found in a message from the person, so the task is the one the session opened with"
		if task == "" {
			artifacts.keep.unfound = "no task was found: no message from the person is carried, and the session opened with none"
		}
	}
	shrunk := slices.Clone(carried)
	before := HistoryTokens(artifacts.preview, shrunk)
	kept := warmFrom(artifacts.preview, shrunk)
	shrink, err := shrinkFrom(artifacts, shrunk, before, 0, kept, causeCompact)
	if err != nil {
		return Compacted{}, err
	}
	unchanged := 0
	for unchanged < len(shrunk) && shrunk[unchanged].Content == carried[unchanged].Content && len(shrunk[unchanged].Images) == len(carried[unchanged].Images) {
		unchanged++
	}
	for unchanged > 0 && unchanged < len(shrunk) && shrunk[unchanged].Role != llm.RoleAssistant {
		unchanged--
	}
	for i := range shrunk {
		shrunk[i].KeptHead = unchanged < len(shrunk) && i == unchanged-1
	}
	return Compacted{Results: shrink.results, TokensBefore: before, TokensAfter: HistoryTokens(artifacts.preview, shrunk), Kept: kept, Drops: shrink.drops, Before: carried, Messages: shrunk}, nil
}

func RecordCarried(store *session.Store, from string, compacted Compacted, by string, at time.Time) (string, error) {
	if _, err := store.Header(from); err != nil {
		return "", err
	}
	events, err := store.Events(from)
	if err != nil {
		return "", err
	}
	blobs, err := store.Blobs(from)
	if err != nil {
		return "", err
	}
	ended, err := store.Open(session.Header{ID: from})
	if err != nil {
		return "", err
	}
	was, into := ended.Header(), session.NewEventID()
	log, err := store.Open(session.Header{ID: into, At: at, ForkKind: string(ForkCompact), Root: cmp.Or(was.Root, was.ID), Task: was.Task,
		Wire: was.Wire, Model: was.Model, Chosen: was.Chosen, CarriedFrom: &session.Carried{Session: was.ID, Event: was.Head}})
	if err != nil {
		return "", errors.Join(err, ended.Close())
	}
	written := &record{store: store, log: log, scope: into, turn: into, said: map[string]string{}}
	for _, event := range events {
		var prompt session.PromptBody
		if event.Kind != session.EventPrompt || event.Agent != "" {
			continue
		}
		written.note(json.Unmarshal(event.Body, &prompt))
		for i, part := range prompt.Parts {
			if part.Blob != "" {
				prompt.Parts[i].Blob, err = log.Keep(blobs[part.Blob])
				written.note(err)
			}
		}
		written.add(session.Event{Kind: session.EventPrompt}, prompt)
	}
	before, _ := written.keptList(compacted.Before)
	after, _ := written.keptList(compacted.Messages)
	_, err = log.Append(session.Event{Kind: session.EventListChange}, session.ListChangeBody{Kind: session.ListChangeFork, Before: before, After: after,
		Why: string(ForkCompact) + " fork from " + was.ID + ", " + strconv.Itoa(compacted.TokensBefore) + " to " + strconv.Itoa(compacted.TokensAfter) + " tokens, asked by " + by})
	written.note(err)
	request, steps := "", 0
	for _, message := range compacted.Messages {
		if message.Role == llm.RoleAssistant {
			request, steps = session.NewEventID(), steps+1
		}
		written.message(message, request, nil)
	}
	for _, drop := range compacted.Drops {
		_, err := log.Append(session.Event{Kind: session.EventCompaction}, compactionRow{Compaction: &Compaction{Step: drop.Step,
			TokensBefore: compacted.TokensBefore, TokensAfter: compacted.TokensAfter, Drops: []recall.Drop{drop}}})
		written.note(err)
	}
	if len(written.failed) > 0 {
		return "", errors.Join(errors.New(strings.Join(written.failed, "; ")), log.Close(), ended.Close())
	}
	_, forked := ended.Append(session.Event{Kind: session.EventCompaction}, compactionRow{Fork: &Fork{Step: steps, Kind: ForkCompact, Into: into,
		TokensBefore: compacted.TokensBefore, TokensAfter: compacted.TokensAfter}})
	edited := ended.Edit(func(header *session.Header) {
		header.ForkedInto, header.EndedAt, header.EndReason = into, &at, session.EndedByFork
		header.ForkTokensBefore, header.ForkTokensAfter = compacted.TokensBefore, compacted.TokensAfter
	})
	return into, errors.Join(forked, edited, log.Close(), ended.Close())
}

func shrinkTo(artifacts Artifacts, messages []llm.Message, estimate, target int, cause shrinkCause) (overflowShrink, error) {
	return shrinkFrom(artifacts, messages, estimate, target, 0, cause)
}

func lastAssistant(messages []llm.Message) int {
	last := len(messages) - 1
	for last >= 0 && messages[last].Role != llm.RoleAssistant {
		last--
	}
	return last
}

func shrinkFrom(artifacts Artifacts, messages []llm.Message, estimate, target, from int, cause shrinkCause) (overflowShrink, error) {
	lastStep := lastAssistant(messages)
	shrink := overflowShrink{bytesBefore: messagesBytes(messages)}
	calls := make(map[string]llm.ToolCall)
	var candidates []dropCandidate
	step := 0
	for i, message := range messages[:max(lastStep, 0)] {
		if message.Role == llm.RoleAssistant {
			step++
		}
		for _, call := range message.ToolCalls {
			calls[call.ID] = call
		}
		if i < from || message.Role != llm.RoleTool {
			continue
		}
		if len(message.Images) > 0 && estimate > target {
			pictureBytes := 0
			for _, image := range message.Images {
				pictureBytes += len(image.Data)
			}
			call := calls[message.ToolCallID]
			var read readArgs
			_ = json.Unmarshal(call.Arguments, &read)
			freed := dropPicture(&messages[i]) - artifacts.preview.Tokens("\n"+pictureDropped)
			estimate -= freed
			shrink.drops = append(shrink.drops, recall.Drop{Step: step, Tool: call.Name, Path: read.Path, Bytes: pictureBytes, TokensFreed: freed, Reason: recall.DroppedAged})
			shrink.results++
		}
		if content := messages[i].Content; len(content) >= artifacts.preview.CompactFloorBytes && !recall.AlreadyDropped(content) && shrunkHandle(content) == "" {
			candidates = append(candidates, dropCandidate{at: i, step: step, call: calls[message.ToolCallID], reason: recall.DroppedAged})
		}
	}
	if artifacts.keep != nil && estimate > target {
		candidates = artifacts.keep.ordered(artifacts.preview, messages, candidates, cause != causeCompact)
	}
	for _, candidate := range candidates {
		if estimate <= target {
			break
		}
		message := messages[candidate.at]
		note, handle, err := shrunkNote(artifacts, candidate.call.Name, WithoutToolRef(message.Content), cause)
		if err != nil {
			return overflowShrink{}, err
		}
		note = toolRefLine(message.Content) + note
		freed := artifacts.preview.MessageTokens(message.Content) - artifacts.preview.MessageTokens(note)
		estimate -= freed
		shrink.drops = append(shrink.drops, recall.Drop{Step: candidate.step, Tool: candidate.call.Name, Handle: handle, Bytes: len(message.Content), TokensFreed: freed,
			Reason: candidate.reason, Decision: candidate.decision})
		messages[candidate.at].Content = note
		shrink.results++
	}
	if artifacts.keep != nil {
		for _, drop := range shrink.drops {
			if key := artifacts.keep.keyOf(drop); key != (dropKey{}) {
				artifacts.keep.dropped[key] = &drop
			}
		}
	}
	shrink.bytesAfter = messagesBytes(messages)
	return shrink, nil
}

func shrunkNote(artifacts Artifacts, tool, result string, cause shrinkCause) (note, handle string, err error) {
	if !artifacts.handles {
		return "the " + tool + " result that stood here was dropped when " + string(cause) + ", and nothing holds it: run the call again if it is still needed.", "", nil
	}
	handle, err = heldHandle(artifacts.store, result)
	if err != nil {
		return "", "", fmt.Errorf("the %s result to shrink after %s could not be held whole: %w", tool, cause, err)
	}
	return "the " + tool + " result that stood here is held whole in artifact " + handle +
		": it was shrunk when " + string(cause) + ". call artifact_fetch with that handle, an offset and a length to read any range of it.", handle, nil
}

const pictureDropped = "the picture read here was seen when this result came back and was dropped later to save room; read the file again to see it"

func dropPicture(message *llm.Message) int {
	freed := len(message.Images) * konst.ImageTokens
	message.Images, message.Content = nil, message.Content+"\n"+pictureDropped
	return freed
}

func keepNewestPictures(messages []llm.Message) (dropped int) {
	var pictured []int
	for i, message := range messages {
		if message.Role == llm.RoleTool && len(message.Images) > 0 {
			pictured = append(pictured, i)
		}
	}
	if len(pictured) <= konst.ImageResultsCeiling {
		return 0
	}
	for _, i := range pictured[:len(pictured)-konst.ImageResultsKept] {
		dropPicture(&messages[i])
	}
	return len(pictured) - konst.ImageResultsKept
}

func HistoryTokens(preview recall.Config, messages []llm.Message) int {
	pictures := 0
	for _, message := range messages {
		pictures += len(message.Images)
	}
	return len(messages)*konst.MessageFramingTokens + messagesBytes(messages)*1000/preview.BytesPerThousandTokens + pictures*konst.ImageTokens
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

func arrivedTogether(messages []llm.Message, task string) (string, int, int) {
	for from := len(messages); from >= 0; from-- {
		environment := environmentBefore(messages[:from])
		to, joined := from, ""
		for to < len(messages) && messages[to].Role == llm.RoleUser && strings.HasPrefix(task, joined+messages[to].Content) {
			joined, to = joined+messages[to].Content+taskItemsApart, to+1
		}
		switch {
		case environment != "" && to > from:
			return environment, from, to
		case environment != "":
			return "", 0, 0
		case to > from && joined == task+taskItemsApart:
			return "", from, to
		}
	}
	return "", 0, 0
}

func forkHistory(artifacts Artifacts, budget recall.Budget, opening llm.Message, messages []llm.Message, forced ForkKind, number, most int) (*Fork, []llm.Message, error) {
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
	counted := forkCounted + strconv.Itoa(max(number, forkNumberIn(messages)+1))
	if most > 0 {
		counted += " of at most " + strconv.Itoa(most) + ", and the turn stops at the cap"
	}
	task := opening.Content
	isTask := func(message llm.Message) bool {
		return message.Role == llm.RoleUser && message.Origin.Source != llm.SourceForkCarry && task != "" && strings.Contains(message.Content, task)
	}
	var system []llm.Message
	foldedFrom, foldedTo := -1, -1
	for i, message := range messages {
		switch {
		case standsAtTheHead(message) || promptChange(message):
			system = append(system, message)
		case isTask(message):
			opening.Images = message.Images
			if environment := environmentBefore(messages[:i]); environment != "" {
				opening.Content, foldedFrom, foldedTo = environment+TheTaskFollows+task, i, i+1
			} else if strings.HasSuffix(message.Content, TheTaskFollows+task) {
				opening.Content = message.Content
			}
		}
	}
	_, bare, framedTask := strings.Cut(task, TheTaskFollows)
	if !framedTask {
		bare = task
	}
	if environment, from, to := arrivedTogether(messages, bare); !slices.ContainsFunc(messages, isTask) && to > from {
		if environment != "" {
			opening.Content = environment + TheTaskFollows + bare
		}
		foldedFrom, foldedTo = from, to
		for _, item := range messages[from:to] {
			opening.Images = append(opening.Images, item.Images...)
		}
	}
	opening.Origin.TakenAt = started
	carried := func(tail []llm.Message, fileTokens int) ([]llm.Message, recall.Carry, int, error) {
		lost := 0
		before := len(messages) - len(tail)
		for i, message := range messages[:before] {
			if message.Role == llm.RoleUser && !isTask(message) && (i < foldedFrom || i >= foldedTo) {
				lost += len(message.Images)
			}
		}
		if foldedTo > before {
			tail = messages[foldedTo:]
		} else if at := slices.IndexFunc(tail, isTask); at >= 0 {
			tail = tail[at:]
		}
		tail = slices.DeleteFunc(slices.Clone(tail), promptChange)
		for i := range tail {
			tail[i].Thinking = llm.Thinking{}
		}
		ended.HeldWhole = len(tail)
		carry, err := recall.CarryWithFiles(artifacts.store, artifacts.preview, ended, artifacts.standing, fileTokens)
		carry.Text = counted + ".\n" + carry.Text
		if lost > 0 {
			carry.Text += "\n" + strconv.Itoa(lost) + picturesLost
		}
		said := llm.Message{Role: llm.RoleUser, Content: carry.Text, Origin: llm.Origin{Source: llm.SourceForkCarry, TakenAt: started}}
		if len(tail) > 0 && isTask(tail[0]) {
			return slices.Concat(system, tail[:1], []llm.Message{said}, tail[1:]), carry, len(tail), err
		}
		return slices.Concat(system, []llm.Message{opening, said}, tail), carry, len(tail), err
	}
	begun, carry, held, err := carried(nil, 0)
	if err != nil {
		return nil, nil, err
	}
	stateTokens := konst.ForkStateBytes * 1000 / artifacts.preview.BytesPerThousandTokens
	unreported := budget.Reported(0, recall.Occupancy{})
	room := func() int {
		return (budget.Bands.Target() - unreported.Tokens(artifacts.preview, historyOf(begun))) * konst.ForkTailRoomPercent / 100
	}
	chosen := len(messages)
	take := func(start, fileTokens, spare int) error {
		kept, keptCarry, keptTail, err := carried(messages[start:], fileTokens)
		if after := unreported.Tokens(artifacts.preview, historyOf(kept)); err == nil && after <= budget.Bands.Target() && after+spare <= budget.Ceiling() {
			begun, carry, held, chosen = kept, keptCarry, keptTail, start
		}
		return err
	}
	if start := tailStart(artifacts.preview, messages, room()); start < len(messages) {
		err = take(start, 0, 0)
	}
	if last := lastAssistant(messages); err == nil && last >= 0 && last < chosen {
		err = take(last, 0, stateTokens)
	}
	if fileTokens := min(room(), konst.ForkTailTokens); err == nil && fileTokens > 0 {
		err = take(chosen, fileTokens, 0)
	}
	if err != nil {
		return nil, nil, err
	}
	before, after := budget.Tokens(artifacts.preview, ended), unreported.Tokens(artifacts.preview, historyOf(begun))
	if forced == "" && after >= before {
		return nil, messages, nil
	}
	return &Fork{
		Kind:          kind,
		TokensBefore:  before,
		TokensAfter:   after,
		BlockedMicros: time.Since(started).Microseconds(),
		TailMessages:  held,
		Carry:         carry,
	}, begun, nil
}

const (
	forkCounted  = "this is fork "
	picturesLost = " picture(s) the person pasted before this fork did not fit under the context budget and are not in this session: ask the person to paste them again if they still matter.\n"
)

func forkNumberIn(messages []llm.Message) int {
	newest := 0
	for _, message := range messages {
		number := 0
		if _, err := fmt.Sscanf(message.Content, forkCounted+"%d", &number); err == nil && message.Role == llm.RoleUser {
			newest = max(newest, number)
		}
	}
	return newest
}

func tailStart(preview recall.Config, messages []llm.Message, room int) int {
	start := len(messages)
	floor := slices.IndexFunc(messages, func(message llm.Message) bool { return message.Role == llm.RoleAssistant })
	if floor < 0 {
		return start
	}
	for i := len(messages) - 1; i >= floor; i-- {
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

func warmFrom(preview recall.Config, messages []llm.Message) int {
	after := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if after > konst.TrimWarmSuffixTokens {
			return i + 1
		}
		after += HistoryTokens(preview, messages[i:i+1])
	}
	return 0
}

func trimRead(artifacts Artifacts, budget recall.Budget, step int, messages []llm.Message, from int) (*Compaction, overflowShrink, error) {
	tokens := budget.Tokens(artifacts.preview, historyOf(messages))
	if tokens <= budget.Bands.Target() {
		return nil, overflowShrink{}, nil
	}
	shrink, err := shrinkFrom(artifacts, messages, tokens, budget.Bands.Target()*konst.ContextTrimPercentOfTarget/100, from, causeBudget)
	if err != nil || shrink.results == 0 {
		return nil, shrink, err
	}
	return &Compaction{Step: step, TokensBefore: tokens, TokensAfter: budget.Tokens(artifacts.preview, historyOf(messages)), Drops: shrink.drops}, shrink, nil
}
