package session

import (
	"encoding/json"
	"strings"
	"time"
)

type Query struct {
	Tool    string    `json:"tool,omitempty"`
	Command string    `json:"command,omitempty"`
	File    string    `json:"file,omitempty"`
	Text    string    `json:"text,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	Since   time.Time `json:"since,omitzero"`
	Until   time.Time `json:"until,omitzero"`
}

type Ran struct {
	Outcome  string `json:"outcome,omitempty"`
	Bytes    int    `json:"bytes"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

type Hit struct {
	Generation int             `json:"generation"`
	Session    string          `json:"session"`
	Event      string          `json:"event"`
	At         time.Time       `json:"at"`
	Agent      string          `json:"agent,omitempty"`
	Turn       string          `json:"turn,omitempty"`
	Call       string          `json:"call,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Command    string          `json:"command,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Ran        *Ran            `json:"ran,omitempty"`
	Role       string          `json:"role,omitempty"`
	Text       string          `json:"text,omitempty"`
}

func holds(text, part string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(part))
}

func (q Query) keeps(event Event) bool {
	agent := q.Agent == "" || event.Agent == q.Agent || strings.HasPrefix(event.Agent, q.Agent+"-f")
	return agent && (q.Since.IsZero() || !event.At.Before(q.Since)) && (q.Until.IsZero() || !event.At.After(q.Until))
}

func (q Query) asksForCalls() bool { return q.Tool != "" || q.Command != "" || q.File != "" }

func (q Query) calls(call CallBody, command string) bool {
	if q.Text != "" || (q.Tool != "" && !strings.EqualFold(call.Tool, q.Tool)) || (q.Command != "" && !holds(command, q.Command)) {
		return false
	}
	if q.File == "" {
		return true
	}
	var args map[string]any
	_ = json.Unmarshal(call.Args, &args)
	for _, value := range args {
		if text, isText := value.(string); isText && holds(text, q.File) {
			return true
		}
	}
	return false
}

func (s *Store) Find(family Family, query Query) ([]Hit, error) {
	var hits []Hit
	for at, generation := range family.Generations {
		events, err := s.Events(generation.ID)
		if err != nil {
			return nil, err
		}
		placed := map[string]int{}
		for _, event := range events {
			if !query.keeps(event) {
				continue
			}
			hit := Hit{Generation: at + 1, Session: generation.ID, Event: event.ID, At: event.At, Agent: event.Agent, Turn: event.Turn, Call: event.Call}
			switch event.Kind {
			case EventToolCall:
				var call CallBody
				var args struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(event.Body, &call) != nil {
					continue
				}
				_ = json.Unmarshal(call.Args, &args)
				if query.calls(call, args.Command) {
					hit.Tool, hit.Command, hit.Args = call.Tool, args.Command, call.Args
					placed[event.Call] = len(hits)
					hits = append(hits, hit)
				}
			case EventToolResult:
				var result ResultBody
				if found, known := placed[event.Call]; known && json.Unmarshal(event.Body, &result) == nil {
					hits[found].Ran = &Ran{Outcome: result.ToolOutcome, Bytes: result.ResultBytes, ExitCode: result.ExitCode, Error: result.Error}
				}
			case EventMessage:
				var said MessageBody
				if query.asksForCalls() || json.Unmarshal(event.Body, &said) != nil || (said.Role != RoleUser && said.Role != RoleAssistant) || strings.TrimSpace(said.Content) == "" || !holds(said.Content, query.Text) {
					continue
				}
				hit.Call, hit.Role, hit.Text = "", said.Role, said.Content
				hits = append(hits, hit)
			}
		}
	}
	return hits, nil
}
