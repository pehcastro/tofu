package turn

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/recall"
)

type ForkKind string

const (
	ForkContinuation ForkKind = "continuation"
	ForkAccountSpent ForkKind = "account-spent"
)

type Fork struct {
	Step          int          `json:"step"`
	Kind          ForkKind     `json:"kind"`
	Into          string       `json:"into"`
	TokensBefore  int          `json:"tokens_before"`
	TokensAfter   int          `json:"tokens_after"`
	BlockedMicros int64        `json:"blocked_micros"`
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

func shrinkPages(store *recall.Store, messages []llm.Message) error {
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
	for _, i := range pages {
		text, call := messages[i].Content, calls[messages[i].ToolCallID]
		header = cmp.Or(pageHeader(text), header)
		if shrunkHandle(text) != "" {
			continue
		}
		if whole == wholePagesKept {
			return nil
		}
		whole--
		handle, rendered := strings.CutPrefix(text, "artifact ")
		handle, _, _ = strings.Cut(handle, " ")
		if !rendered {
			elided, err := recall.Elide(store, recall.Config{}, []byte(text), true)
			if err != nil {
				return fmt.Errorf("the %s result that a newer page replaced could not be held whole: %w", call.Name, err)
			}
			handle = elided.Reference.ID
		}
		messages[i].Content = shrunkPage(text, header, noteOf(call), noteAfter(messages[i+1:]), handle)
	}
	return nil
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
	if err := shrinkPages(artifacts.store, messages); err != nil {
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
	carry, err := recall.DistilledCarry(artifacts.store, artifacts.preview, ended)
	if err != nil {
		return nil, nil, err
	}
	counted := "this is fork " + strconv.Itoa(number)
	if most > 0 {
		counted += " of at most " + strconv.Itoa(most) + ", and the turn stops at the cap"
	}
	carry.Text = counted + ".\n" + carry.Text
	var begun []llm.Message
	for _, message := range messages {
		if message.Role == llm.RoleSystem {
			begun = append(begun, message)
		}
	}
	begun = append(begun,
		llm.Message{Role: llm.RoleUser, Content: task},
		llm.Message{Role: llm.RoleUser, Content: carry.Text})
	return &Fork{
		Kind:          kind,
		TokensBefore:  recall.Measure(artifacts.preview, budget.Bands, ended).Total(),
		TokensAfter:   recall.Measure(artifacts.preview, budget.Bands, historyOf(begun)).Total(),
		BlockedMicros: time.Since(started).Microseconds(),
		Carry:         carry,
	}, begun, nil
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
