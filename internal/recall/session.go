package recall

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"boji/internal/sys"
)

type SessionCall struct {
	Tool          string          `json:"tool"`
	Args          json.RawMessage `json:"args,omitempty"`
	Command       string          `json:"command,omitempty"`
	RenderedBytes int             `json:"rendered_bytes"`
}

type SessionStep struct {
	Index           int           `json:"index"`
	AssistantText   string        `json:"assistant_text,omitempty"`
	PromptTokens    int           `json:"prompt_tokens"`
	CacheReadTokens int           `json:"cache_read_tokens"`
	ToolCalls       []SessionCall `json:"tool_calls,omitempty"`
}

type Session struct {
	ID    string        `json:"id"`
	Task  string        `json:"task"`
	Steps []SessionStep `json:"steps,omitempty"`
}

func ReadSession(path string) (Session, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("recall: %s is not a recorded turn: %w", path, err)
	}
	if session.Task == "" || len(session.Steps) == 0 {
		return Session{}, fmt.Errorf("recall: %s carries no task and no steps", path)
	}
	return session, nil
}

type ReplayStep struct {
	Index          int
	InputTokens    int
	RecordedTokens int
	Dropped        int
}

type ReplayResult struct {
	Steps []ReplayStep
	Drops []Drop
	Peak  Occupancy
	Final Occupancy
}

func (r ReplayResult) InputTokens() int {
	total := 0
	for _, step := range r.Steps {
		total += step.InputTokens
	}
	return total
}

func ReplaySession(store *Store, cfg Config, bands Bands, session Session, compacting bool) (ReplayResult, error) {
	if len(session.Steps) == 0 {
		return ReplayResult{}, errors.New("recall: a session with no steps has nothing to replay")
	}
	unrecordedPrefixBytes := session.Steps[0].CacheReadTokens * cfg.BytesPerThousandTokens / 1000
	conversation := Conversation{Instructions: session.Task + strings.Repeat(".", unrecordedPrefixBytes)}

	var result ReplayResult
	for _, step := range session.Steps {
		occupancy := Measure(cfg, bands, conversation)
		if occupancy.Total() > result.Peak.Total() {
			result.Peak = occupancy
		}
		result.Steps = append(result.Steps, ReplayStep{
			Index:          step.Index,
			InputTokens:    occupancy.Total(),
			RecordedTokens: step.PromptTokens + step.CacheReadTokens,
		})
		assistant := step.AssistantText
		for _, call := range step.ToolCalls {
			assistant += "\n" + call.Tool + " " + string(call.Args)
		}
		if assistant != "" {
			conversation.Entries = append(conversation.Entries, Entry{Step: step.Index, Text: assistant})
		}
		for _, call := range step.ToolCalls {
			conversation.Entries = append(conversation.Entries, Entry{
				Step:         step.Index,
				Tool:         call.Tool,
				SupersedeKey: call.Tool + " " + string(call.Args),
				Text:         recordedBody(call),
			})
		}
		if !compacting {
			continue
		}
		compacted, drops, err := Compact(store, cfg, bands, conversation)
		if err != nil {
			return ReplayResult{}, err
		}
		conversation = compacted
		result.Drops = append(result.Drops, drops...)
		result.Steps[len(result.Steps)-1].Dropped = len(drops)
	}
	result.Final = Measure(cfg, bands, conversation)
	return result, nil
}

func recordedBody(call SessionCall) string {
	stand := call.Tool + " result of " + call.Command + ", length recorded, body not\n"
	if len(stand) >= call.RenderedBytes {
		return stand[:call.RenderedBytes]
	}
	return stand + strings.Repeat(".", call.RenderedBytes-len(stand))
}
