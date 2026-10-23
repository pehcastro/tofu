package session

import (
	"cmp"
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
	Failed  bool      `json:"failed,omitempty"`
	Since   time.Time `json:"since,omitzero"`
	Until   time.Time `json:"until,omitzero"`
}

type Ran struct {
	Outcome  string `json:"outcome,omitempty"`
	Refused  bool   `json:"refused,omitempty"`
	Bytes    int    `json:"bytes"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (r *Ran) Failed() bool { return r != nil && r.Outcome == ToolOutcomeFailed }

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

func (q Query) asksForCalls() bool {
	return q.Tool != "" || q.Command != "" || q.File != "" || q.Failed
}

func (q Query) calls(call Hit) bool {
	if (q.Tool != "" && !strings.EqualFold(call.Tool, q.Tool)) || (q.Command != "" && !holds(call.Command, q.Command)) {
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
		placed, called := map[string]int{}, map[string]Hit{}
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
				hit.Role, hit.Tool, hit.Command, hit.Args = RoleTool, call.Tool, args.Command, call.Args
				called[event.Call] = hit
				if !query.Failed && query.calls(hit) && holds(string(call.Args), query.Text) {
					placed[event.Call] = len(hits)
					hits = append(hits, hit)
				}
			case EventToolResult:
				var result struct {
					ResultBody
					Refused bool `json:"refused"`
				}
				if json.Unmarshal(event.Body, &result) != nil {
					continue
				}
				ran := &Ran{Outcome: result.ToolOutcome, Refused: result.Refused, Bytes: result.ResultBytes, ExitCode: result.ExitCode, Error: result.Error}
				text, matched := cmp.Or(result.Content, result.Error), query.Text != "" && (holds(result.Content, query.Text) || holds(result.Error, query.Text))
				if found, known := placed[event.Call]; known {
					hits[found].Ran = ran
					if matched {
						hits[found].Text = text
					}
					continue
				}
				call, wanted := called[event.Call], matched
				if query.Failed {
					wanted = ran.Failed() && (matched || holds(string(call.Args), query.Text))
				}
				if !wanted || !query.calls(call) {
					continue
				}
				hit.Role, hit.Tool, hit.Command, hit.Args, hit.Text, hit.Ran = RoleTool, call.Tool, call.Command, call.Args, text, ran
				hits = append(hits, hit)
			case EventMessage:
				var message MessageBody
				if query.asksForCalls() || json.Unmarshal(event.Body, &message) != nil || (message.Role != RoleUser && message.Role != RoleAssistant) || strings.TrimSpace(message.Content) == "" || !holds(message.Content, query.Text) {
					continue
				}
				hit.Call, hit.Role, hit.Text = "", message.Role, message.Content
				hits = append(hits, hit)
			}
		}
	}
	return hits, nil
}
