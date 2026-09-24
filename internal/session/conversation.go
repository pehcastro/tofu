package session

import "encoding/json"

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type MessageToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type MessageBody struct {
	Role            string            `json:"role"`
	Content         string            `json:"content,omitempty"`
	ToolCallID      string            `json:"tool_call_id,omitempty"`
	ToolCalls       []MessageToolCall `json:"tool_calls,omitempty"`
	ToolOutcome     string            `json:"tool_outcome,omitempty"`
	ToolResultBytes int               `json:"tool_result_bytes,omitempty"`
}

type StepToolCall struct {
	Tool         string          `json:"tool"`
	Args         json.RawMessage `json:"args,omitempty"`
	ResultBytes  int             `json:"result_bytes"`
	ResultHash   string          `json:"result_hash,omitempty"`
	ResultHandle string          `json:"result_handle,omitempty"`
	Error        string          `json:"error,omitempty"`
}

type StepBody struct {
	Index         int            `json:"index"`
	AssistantText string         `json:"assistant_text,omitempty"`
	ToolCalls     []StepToolCall `json:"tool_calls,omitempty"`
}

type Call struct {
	ID   string
	Name string
	Args json.RawMessage
}

type Utterance struct {
	Event string
	Role  string
	Text  string
	Calls []Call
}

type Conversation struct {
	Session   string
	Said      []Utterance
	FromSteps bool
}

func (s *Store) Conversation(id string) (Conversation, error) {
	reading, err := s.Reading(id)
	if err != nil {
		return Conversation{}, err
	}
	talk := Conversation{Session: id}
	for _, message := range reading.Messages {
		said := Utterance{Event: message.Event, Role: message.Role, Text: message.Content}
		for _, call := range message.ToolCalls {
			said.Calls = append(said.Calls, Call{ID: call.ID, Name: call.Name, Args: call.Arguments})
		}
		talk.Said = append(talk.Said, said)
	}
	if len(talk.Said) > 0 {
		return talk, nil
	}
	for _, step := range reading.Steps {
		said := Utterance{Event: step.Event, Role: RoleAssistant, Text: step.AssistantText}
		for _, call := range step.ToolCalls {
			said.Calls = append(said.Calls, Call{Name: call.Tool, Args: call.Args})
		}
		talk.Said = append(talk.Said, said)
	}
	talk.FromSteps = len(talk.Said) > 0
	return talk, nil
}
