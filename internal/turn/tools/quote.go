package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	quoteOpenMark   = "[quote"
	quoteCloseMark  = "]"
	quoteShortRunes = 6
)

type QuoteOutcome string

const (
	QuoteItem      QuoteOutcome = "item"
	QuoteNotFound  QuoteOutcome = "not_found"
	QuoteAmbiguous QuoteOutcome = "ambiguous"
)

var ErrQuoteNoID = errors.New("quote: id is required, and it is the reference the person wrote or the short id inside it")

type Quoted struct {
	Outcome QuoteOutcome
	Hash    string
	Session string
	Event   string
	Speaker string
	Noun    string
	Words   string
}

func ShortID(id string) string {
	if id == "" {
		return ""
	}
	runes := []rune(id)
	return "#" + string(runes[max(len(runes)-quoteShortRunes, 0):])
}

func SaidEventID(talk session.Conversation, index int) string {
	return cmp.Or(talk.Said[index].Event, session.EventIDFor(talk.Session, "step:"+strconv.Itoa(index)))
}

func QuoteRef(id string) string {
	if id == "" {
		return ""
	}
	return quoteOpenMark + ShortID(id) + quoteCloseMark
}

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
		Description: "returns what one recorded item of this session holds, named by the short id the person put in their message as [quote#abcd]. " +
			"the item is a turn, said by the person, by the agent or returned by a tool, or a tool call: an edit, a shell command, a question asked of the person, a sub-agent spawned, or a call a sub-agent made. " +
			"read it before answering and cite its words rather than paraphrase, because a paraphrase of what was said is a new claim rather than a citation. " +
			"an id nothing ends with, or one that two items end with, comes back as an error naming which of the two happened: ask for the reference again rather than quoting the nearest item. " +
			"a turn comes back as what was said and the names of the tools it ran, never their arguments; a tool call comes back as its arguments and what it returned, with known keys masked. " +
			"an item larger than the result cap comes back cut, with a note saying how much of it is there.",
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
	found, err := ResolveQuote(q.store, q.session, args.ID)
	if err != nil {
		return turn.Result{}, err
	}
	switch found.Outcome {
	case QuoteNotFound:
		return turn.Result{}, fmt.Errorf("quote: %w in this session or any it was forked from: %q: ask for the reference again rather than quoting the nearest item", session.ErrEventHashNotFound, found.Hash)
	case QuoteAmbiguous:
		return turn.Result{}, fmt.Errorf("quote: %w in %s: %q: ask for the reference again rather than quoting the nearest item", session.ErrEventHashAmbiguous, found.Session, found.Hash)
	case QuoteItem:
		body, note := quoteCut(found.Noun, found.Words)
		header := fmt.Sprintf("quote %s, %s, recorded in %s", found.Hash, found.Speaker, found.Session)
		return turn.Result{Content: withNote(header+"\n"+body+"\n", note), Command: found.Hash}, nil
	}
	panic("quote: unknown outcome " + string(found.Outcome))
}

func ResolveQuote(store *session.Store, recorded, ref string) (Quoted, error) {
	hash := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ref), quoteOpenMark), quoteCloseMark)
	if strings.Trim(hash, "#") == "" {
		return Quoted{}, ErrQuoteNoID
	}
	if store == nil || recorded == "" {
		return Quoted{}, errors.New("quote: this turn is recording no session, so there is nothing to quote")
	}
	newest, err := store.Header(recorded)
	if errors.Is(err, fs.ErrNotExist) {
		return Quoted{Outcome: QuoteNotFound, Hash: hash}, nil
	}
	for err == nil && newest.ForkedInto != "" && newest.ForkedInto != newest.ID {
		newest, err = store.Header(newest.ForkedInto)
	}
	if err != nil {
		return Quoted{}, fmt.Errorf("quote: %w", err)
	}
	ancestors, err := store.Ancestors(newest.ID)
	if err != nil {
		return Quoted{}, fmt.Errorf("quote: %w", err)
	}
	for _, header := range append([]session.Header{newest}, ancestors...) {
		hits, err := quotedIn(store, header.ID, hash)
		switch {
		case err != nil:
			return Quoted{}, err
		case len(hits) == 1:
			hits[0].Outcome, hits[0].Hash, hits[0].Session = QuoteItem, hash, header.ID
			return hits[0], nil
		case len(hits) > 1:
			return Quoted{Outcome: QuoteAmbiguous, Hash: hash, Session: header.ID}, nil
		}
	}
	return Quoted{Outcome: QuoteNotFound, Hash: hash}, nil
}

func quotedIn(store *session.Store, recorded, hash string) ([]Quoted, error) {
	talk, err := store.Conversation(recorded)
	if err != nil {
		return nil, fmt.Errorf("quote: %w", err)
	}
	events, err := store.Events(recorded)
	if err != nil {
		return nil, fmt.Errorf("quote: %w", err)
	}
	var hits []Quoted
	for index, said := range talk.Said {
		event := SaidEventID(talk, index)
		if session.DrawnAs(event, hash) {
			speaker := fmt.Sprintf("%s, turn %d of %d", quoteSpeaker(said.Role), index+1, len(talk.Said))
			hits = append(hits, Quoted{Event: event, Speaker: speaker, Noun: "turn", Words: quoteWords(said)})
		}
	}
	for _, event := range events {
		if event.Kind != session.EventToolCall || !session.DrawnAs(event.ID, hash) {
			continue
		}
		call, err := quotedCall(event, events)
		if err != nil {
			return nil, err
		}
		hits = append(hits, call)
	}
	return hits, nil
}

func quotedCall(call session.Event, events []session.Event) (Quoted, error) {
	var asked session.CallBody
	if err := json.Unmarshal(call.Body, &asked); err != nil {
		return Quoted{}, fmt.Errorf("quote: the call %s is recorded in a shape this build cannot read: %w", call.ID, err)
	}
	speaker := "a call to " + asked.Tool
	if call.Agent != "" {
		speaker += " by the sub-agent " + call.Agent
	}
	words := "it called " + asked.Tool + " with " + string(asked.Args) + "\n"
	answered := slices.IndexFunc(events, func(event session.Event) bool {
		return event.Kind == session.EventToolResult && event.Call == call.Call && event.Agent == call.Agent && event.Turn == call.Turn
	})
	if answered < 0 {
		words += "no result is recorded for it yet"
	} else {
		var result session.ResultBody
		if err := json.Unmarshal(events[answered].Body, &result); err != nil {
			return Quoted{}, fmt.Errorf("quote: the result of call %s is recorded in a shape this build cannot read: %w", call.ID, err)
		}
		words += "it " + cmp.Or(result.ToolOutcome, session.ToolOutcomeRan) + " and returned:\n" + cmp.Or(result.Content, result.Error)
	}
	return Quoted{Event: call.ID, Speaker: speaker, Noun: "call", Words: sys.LoadKeyRedactor().Redact(words)}, nil
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

func quoteCut(noun, words string) (string, string) {
	if len(words) <= konst.TurnResultBytesCap {
		return words, ""
	}
	kept := konst.TurnResultBytesCap
	for kept > 0 && !utf8.RuneStart(words[kept]) {
		kept--
	}
	return words[:kept], search.Note(search.Truncated, fmt.Sprintf(
		"the %s is %d bytes and the first %d are here: the rest of it stays in the session record, which the person can open",
		noun, len(words), kept))
}
