package turn

import (
	"time"

	"tofu/internal/llm"
	"tofu/internal/recall"
)

type ForkKind string

const (
	ForkContinuation ForkKind = "continuation"
	ForkClean        ForkKind = "clean"
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

type Occupancy struct {
	Identity   int `json:"identity"`
	Facts      int `json:"facts"`
	WorkingSet int `json:"working_set"`
	Recent     int `json:"recent"`
	Target     int `json:"target"`
}

func (o Occupancy) Total() int {
	return o.Identity + o.Facts + o.WorkingSet + o.Recent
}

func occupancyOf(measured recall.Occupancy) Occupancy {
	return Occupancy{
		Identity:   measured.Identity,
		Facts:      measured.Facts,
		WorkingSet: measured.WorkingSet,
		Recent:     measured.Recent,
		Target:     measured.Bands.Target(),
	}
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

func forkHistory(artifacts Artifacts, bands recall.Bands, task string, messages []llm.Message) (*Fork, []llm.Message, Occupancy, error) {
	ended := historyOf(messages)
	occupancy := occupancyOf(recall.Measure(artifacts.preview, bands, ended))
	if occupancy.Total() <= bands.Target() {
		return nil, messages, occupancy, nil
	}
	started := time.Now()
	carry, err := recall.HandleCarry(artifacts.store, artifacts.preview, ended)
	if err != nil {
		return nil, nil, occupancy, err
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
		Kind:          ForkContinuation,
		TokensBefore:  occupancy.Total(),
		TokensAfter:   recall.Measure(artifacts.preview, bands, historyOf(begun)).Total(),
		BlockedMicros: time.Since(started).Microseconds(),
		Carry:         carry,
	}, begun, occupancy, nil
}

func compactHistory(artifacts Artifacts, bands recall.Bands, step int, messages []llm.Message) (*Compaction, error) {
	before := historyOf(messages)
	tokensBefore := recall.Measure(artifacts.preview, bands, before).Total()
	after, drops, err := recall.Compact(artifacts.store, artifacts.preview, bands, before)
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
		TokensAfter:  recall.Measure(artifacts.preview, bands, after).Total(),
		Drops:        drops,
	}, nil
}
