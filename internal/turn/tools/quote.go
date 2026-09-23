package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	quoteStepScope = "step:"
	quoteOpenMark  = "[quote"
	quoteCloseMark = "]"
)

type Quote struct {
	store   *session.Store
	session string
}

func NewQuote(store *session.Store, recorded string) Quote {
	return Quote{store: store, session: recorded}
}

func (q Quote) Name() string { return "quote" }

func (q Quote) Definition() llm.Tool {
	return llm.Tool{
		Name: "quote",
		Description: "returns the words of one recorded turn of this session, named by the short id the person put in their message as [quote#abcd]. " +
			"read the turn before answering and cite its words rather than paraphrase, because a paraphrase of what was said is a new claim rather than a citation. " +
			"an id no turn ends with, or one that two turns end with, comes back as an error naming which of the two happened: ask for the reference again rather than quoting the nearest turn. " +
			"it returns what was said and the names of the tools that ran, never a tool's arguments and never its output, " +
			"and a turn larger than the result cap comes back cut, with a note saying how much of it is there.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "the reference as it was written, for instance [quote#39cl], or the short id inside it"},
			},
			"required": []string{"id"},
		},
	}
}

type quoteArgs struct {
	ID string `json:"id"`
}

func (q Quote) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args quoteArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("quote: arguments are not the expected shape: %w", err)
	}
	hash := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(args.ID), quoteOpenMark), quoteCloseMark)
	if strings.Trim(hash, "#") == "" {
		return turn.Result{}, errors.New("quote: id is required, and it is the reference the person wrote or the short id inside it")
	}
	if q.store == nil || q.session == "" {
		return turn.Result{}, errors.New("quote: this turn is recording no session, so there is no turn to quote")
	}
	talk, err := q.store.Conversation(q.session)
	if err != nil {
		return turn.Result{}, fmt.Errorf("quote: %w", err)
	}
	said, at, err := quotedTurn(talk, hash)
	if err != nil {
		return turn.Result{}, err
	}
	body, note := quoteCut(quoteWords(said))
	header := fmt.Sprintf("quote %s, %s, turn %d of %d recorded in %s",
		hash, quoteSpeaker(said.Role), at+1, len(talk.Said), talk.Session)
	return turn.Result{
		Content: withNote(header+"\n"+body+"\n", note),
		Command: "quote " + hash,
	}, nil
}

func quotedTurn(talk session.Conversation, hash string) (session.Utterance, int, error) {
	events := make([]session.Event, len(talk.Said))
	at := make(map[string]int, len(talk.Said))
	for index, one := range talk.Said {
		id := one.Event
		if id == "" {
			id = session.EventIDFor(talk.Session, quoteStepScope+strconv.Itoa(index))
		}
		events[index], at[id] = session.Event{ID: id}, index
	}
	found, err := session.FindByHash(events, hash)
	if err != nil {
		return session.Utterance{}, 0, fmt.Errorf("quote: %w: ask for the reference again rather than quoting the nearest turn", err)
	}
	return talk.Said[at[found.ID]], at[found.ID], nil
}

func quoteSpeaker(role string) string {
	switch role {
	case session.RoleUser:
		return "you"
	case session.RoleAssistant:
		return "the agent"
	case session.RoleTool:
		return "a tool result"
	}
	return role
}

func quoteWords(said session.Utterance) string {
	names := make([]string, 0, len(said.Calls))
	for _, call := range said.Calls {
		names = append(names, call.Name)
	}
	words := strings.TrimSpace(said.Text)
	if len(names) > 0 {
		words = strings.TrimSpace(words + "\nit called " + strings.Join(names, ", "))
	}
	if words == "" {
		return "this turn recorded no words and ran no tool"
	}
	return words
}

func quoteCut(words string) (string, string) {
	if len(words) <= konst.TurnResultBytesCap {
		return words, ""
	}
	kept := konst.TurnResultBytesCap
	for kept > 0 && !utf8.RuneStart(words[kept]) {
		kept--
	}
	return words[:kept], search.Note(search.Truncated, fmt.Sprintf(
		"the turn is %d bytes and the first %d are here: the rest of it stays in the session record, which the person can open",
		len(words), kept))
}
