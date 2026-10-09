package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
)

const (
	LookupToolName  = "lookup"
	sourceForkState = "fork state"
	forkStateLead   = "where the work stood when the last session ended, as the agent wrote it there:\n"
	forkLookupLine  = "to see any call named below again, call lookup with its call id instead of reading or running it again: lookup returns that call and its result whole, from session %s or any earlier session of its line.\n"
)

const (
	sourceEpisodeView = "episode view"
	episodeOfPerson   = "user"
	episodeOfLead     = "lead"
	episodeOfReport   = "work"
)

type episodeKeeper interface {
	Episodes() (string, error)
	Keep(kind, text string) error
	Compact() error
}

func standsAtTheHead(message llm.Message) bool {
	return message.Role == llm.RoleSystem || message.Origin.Source == sourceMemoryView || message.Origin.Source == sourceEpisodeView
}

func episodeKind(source string) string {
	switch {
	case strings.Contains(source, sourceReport):
		return episodeOfReport
	case source == sourceTask || source == sourceTyped || source == sourceSteer:
		return episodeOfPerson
	}
	return ""
}

func withEpisodes(messages []llm.Message, view string) []llm.Message {
	if held := slices.IndexFunc(messages, func(message llm.Message) bool { return message.Origin.Source == sourceEpisodeView }); held >= 0 {
		messages[held].Content = view
		return messages
	}
	at := slices.IndexFunc(messages, func(message llm.Message) bool { return !standsAtTheHead(message) })
	if at < 0 {
		at = len(messages)
	}
	return slices.Insert(messages, at, llm.Message{Role: llm.RoleUser, Content: view, Origin: llm.Origin{Source: sourceEpisodeView}})
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

func carryState(fork *Fork, begun []llm.Message, state, session string) {
	at := slices.IndexFunc(begun, func(message llm.Message) bool {
		return message.Origin.Source == sourceForkCarry && message.Content == fork.Carry.Text
	})
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

type lookupTool struct {
	sessions *session.Store
	current  func() string
}

func (lookupTool) Name() string { return LookupToolName }

func (lookupTool) Definition() llm.Tool {
	return llm.Tool{
		Name: LookupToolName,
		Description: "returns one earlier tool call and its result whole, by the call id a fork carry names. " +
			"session defaults to this one, and a call from an earlier session of the same line is found from the newer one too",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session": map[string]any{"type": "string"},
				"call":    map[string]any{"type": "string"},
			},
			"required": []string{"call"},
		},
	}
}

func (t lookupTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Session string `json:"session"`
		Call    string `json:"call"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("lookup: arguments are not the expected shape: %w", err)
	}
	if args.Session == "" {
		args.Session = t.current()
	}
	seen := map[string]bool{}
	for id := args.Session; id != "" && !seen[id]; {
		seen[id] = true
		events, err := t.sessions.Events(id)
		if err != nil {
			return Result{}, fmt.Errorf("lookup: session %s: %w", id, err)
		}
		var call session.CallBody
		var result session.ResultBody
		for _, event := range events {
			switch {
			case event.Call != args.Call:
			case event.Kind == session.EventToolCall:
				err = json.Unmarshal(event.Body, &call)
			case event.Kind == session.EventToolResult:
				err = json.Unmarshal(event.Body, &result)
			}
			if err != nil {
				return Result{}, fmt.Errorf("lookup: call %s in session %s does not parse: %w", args.Call, id, err)
			}
		}
		if call.Tool != "" {
			return Result{Content: "the call: " + call.Tool + " " + string(call.Args) + "\nits result:\n" + result.Content,
				Command: "call " + args.Call + " in session " + id}, nil
		}
		header, err := t.sessions.Header(id)
		if err != nil {
			return Result{}, fmt.Errorf("lookup: session %s: %w", id, err)
		}
		id = ""
		if header.CarriedFrom != nil {
			id = header.CarriedFrom.Session
		}
	}
	return Result{}, fmt.Errorf("lookup: no call %s in session %s or any session it was carried from", args.Call, args.Session)
}
