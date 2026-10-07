package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/session"
	"tofu/internal/turn"
)

type Remember struct {
	Ask     turn.Person
	Store   *session.Store
	Session string
	Project string
	Inbox   *turn.Inbox
	Auto    func() bool
}

func (Remember) Name() string { return turn.RememberToolName }

func (Remember) Definition() llm.Tool {
	return llm.Tool{
		Name: turn.RememberToolName,
		Description: "offers the person to keep one rule for every later session, when they corrected you or said how things are done beyond this task. " +
			"said must be their own words, copied exactly from a message they typed in this conversation; anything else is refused. " +
			"statement is the rule itself in plain words, imperative or declarative, and names no one: no name, no the person, the user, he or she, such as: " +
			"Desk UI primitives are widened to fit a new need; never build a parallel copy in a caller. a statement that names or describes the person is refused. " +
			"a message where they said remember already gets tofu's own offer, so do not offer those. " +
			"the person answers yes, no, or always, and picks global or project; nothing is kept before a yes. a person entry is offered for every project, a project or reference entry for this project",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"statement": map[string]any{"type": "string", "description": "the rule itself, one line, naming no one"},
				"said":      map[string]any{"type": "string", "description": "the person's exact words this comes from"},
				"kind":      map[string]any{"type": "string", "enum": []string{string(memory.KindPerson), string(memory.KindProject), string(memory.KindReference)}},
			},
			"required": []string{"statement", "said", "kind"},
		},
	}
}

func (r Remember) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Statement string      `json:"statement"`
		Said      string      `json:"said"`
		Kind      memory.Kind `json:"kind"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("remember: arguments are not the expected shape: %w", err)
	}
	if words, named := memory.PersonIn(args.Statement); named {
		return turn.Result{}, fmt.Errorf("remember: the statement names or describes the person (%q); write the rule itself, naming no one, such as: Replies stay short and plain", words)
	}
	typed, err := r.typedInChain()
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	quote := strings.Join(strings.Fields(args.Said), " ")
	at := slices.IndexFunc(typed, func(words string) bool { return quote != "" && strings.Contains(words, quote) })
	if at < 0 {
		return turn.Result{}, fmt.Errorf("remember: %q is not in any message the person typed in this conversation; copy their words exactly, or do not offer it", args.Said)
	}
	if _, offered := memory.OfferFor(typed[at]); offered {
		return turn.Result{}, errors.New("remember: tofu already offered the person to keep that message, because it says remember; do not offer it again")
	}
	entry := memory.Entry{Scope: memory.Project, Kind: args.Kind, Text: strings.TrimSpace(args.Statement), Said: args.Said, Session: r.Session, At: time.Now(), By: memory.ByLead}
	if args.Kind == memory.KindPerson {
		entry.Scope = memory.Global
	}
	if !r.Auto() {
		if r.Ask == nil {
			return turn.Result{Content: "no person is here to answer, so nothing was kept"}, nil
		}
		shown, err := json.Marshal(map[string]string{"statement": entry.Text, "scope": string(entry.Scope), "said": args.Said})
		if err != nil {
			return turn.Result{}, err
		}
		answer, err := r.Ask(ctx, turn.GateRequest{Tool: turn.RememberToolName, Args: shown}, turn.GateDecision{Verdict: ledger.VerdictAsk})
		if err != nil {
			return turn.Result{Content: "the person could not be asked, so nothing was kept: " + err.Error()}, nil
		}
		scope, err := answeredScope(answer)
		if err != nil {
			return turn.Result{}, fmt.Errorf("remember: %w", err)
		}
		if scope == "" {
			return turn.Result{Content: "the person said no, and nothing was kept"}, nil
		}
		entry.Scope = scope
	}
	shelves, err := memory.Open(r.Project)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	if entry, err = shelves.Add(entry, ""); err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	r.Inbox.Remembered(entry.Saved())
	return turn.Result{Content: entry.Saved(), Command: entry.Ref()}, nil
}

func answeredScope(answer turn.PersonAnswer) (memory.Scope, error) {
	global, err := memory.GlobalDir()
	if err != nil {
		return "", err
	}
	switch answer {
	case turn.PersonDenied:
		return "", memory.Record(global, memory.AnswerNo)
	case turn.PersonAllowedOnce:
		return memory.Project, memory.Record(global, memory.AnswerProject)
	case turn.PersonAlwaysHere:
		return memory.Global, memory.Record(global, memory.AnswerGlobal)
	}
	panic("tools: unknown person answer")
}

func (r Remember) typedInChain() ([]string, error) {
	newest, err := r.Store.Header(r.Session)
	for err == nil && newest.ForkedInto != "" && newest.ForkedInto != newest.ID {
		newest, err = r.Store.Header(newest.ForkedInto)
	}
	if err != nil {
		return nil, err
	}
	ancestors, err := r.Store.Ancestors(newest.ID)
	if err != nil {
		return nil, err
	}
	var typed []string
	for _, header := range append([]session.Header{newest}, ancestors...) {
		reading, err := r.Store.Reading(header.ID)
		if err != nil {
			return nil, err
		}
		for _, message := range reading.Messages {
			if words, said := turn.TaskIn(message.Content); said && message.Role == session.RoleUser && turn.SaidByThePerson(message.Origin) {
				typed = append(typed, strings.Join(strings.Fields(words), " "))
			}
		}
	}
	return typed, nil
}
