package turn

import (
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
			entry.Tool, entry.SupersedeKey = call.Name, call.Name+" "+string(call.Arguments)
		}
		conversation.Entries = append(conversation.Entries, entry)
	}
	return conversation
}

func forkHistory(artifacts Artifacts, budget recall.Budget, task string, messages []llm.Message, forced ForkKind) (*Fork, []llm.Message, error) {
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
