package host

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/memory"
	"tofu/internal/memtree"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const MemoryNone = "none"

type MemoryViewParams struct {
	Scope string `json:"scope"`
}

type MemoryZoomParams struct {
	Store string `json:"store"`
	ID    int    `json:"id"`
	N     int    `json:"n"`
}

type MemoryRecallParams struct {
	Store string `json:"store"`
	Regex string `json:"regex"`
}

type MemoryLine struct {
	ID   int    `json:"id"`
	N    int    `json:"n"`
	Text string `json:"text"`
}

type MemoryView struct {
	Store string       `json:"store"`
	Lines []MemoryLine `json:"lines"`
}

type MemoryScoped struct {
	Statement string         `json:"statement"`
	Offered   memory.Scope   `json:"offered"`
	Picked    string         `json:"picked"`
	Scopes    []memory.Scope `json:"scopes,omitempty"`
	By        string         `json:"by"`
}

const (
	ScopedByPerson = "person"
	ScopedByAuto   = "auto"
)

func keptMemory(emit func(Event)) func(tools.MemoryKept) {
	return func(kept tools.MemoryKept) {
		emit(Event{Kind: EventMemoryScoped, ID: session.NewEventID(), Scoped: &MemoryScoped{Statement: kept.Statement, Offered: kept.Offered, Picked: string(kept.Kept), By: ScopedByAuto}})
	}
}

type MemoryScopedEvent struct {
	Identity
	MemoryScoped
}

func (s *server) memoryRead(named string, read func(*memtree.Store) ([]string, error)) (MemoryView, error) {
	store := named
	if named != memory.EpisodesStore {
		scope, err := memory.ParseScope(named)
		if err != nil {
			return MemoryView{}, &Refusal{Code: CodeBadParams, Message: "a store is " + memory.EpisodesStore + " or a scope: " + err.Error()}
		}
		if _, err := memory.Block(s.Dir); err != nil {
			return MemoryView{}, err
		}
		store = string(scope)
	}
	logs, err := memory.Trees(s.Dir)
	if err != nil {
		return MemoryView{}, err
	}
	view := MemoryView{Store: store, Lines: []MemoryLine{}}
	log, kept := logs[store]
	if _, missing := os.Stat(log); missing != nil {
		kept = false
	}
	switch {
	case !kept && read == nil:
		return view, nil
	case !kept:
		return MemoryView{}, &Refusal{Code: CodeRefused, Message: store + " holds nothing in this project yet"}
	case read == nil:
		read = func(tree *memtree.Store) ([]string, error) { return strings.Split(tree.View(), "\n"), nil }
	}
	tree, err := memtree.Open(log)
	if err != nil {
		return MemoryView{}, &Refusal{Code: CodeRefused, Message: store + " is being written: " + err.Error()}
	}
	lines, err := read(tree)
	if closed := tree.Close(); err == nil && closed != nil {
		return MemoryView{}, closed
	}
	if err != nil {
		return MemoryView{}, &Refusal{Code: CodeBadParams, Message: err.Error()}
	}
	for _, line := range slices.DeleteFunc(lines, func(line string) bool { return line == "" }) {
		head, text, _ := strings.Cut(line, "|")
		id, n, _ := strings.Cut(head, "+")
		parsed := MemoryLine{Text: text}
		var idErr, nErr error
		parsed.ID, idErr = strconv.Atoi(id)
		parsed.N, nErr = strconv.Atoi(n)
		if idErr != nil || nErr != nil {
			return MemoryView{}, fmt.Errorf("%s: the line %q is not id+n|text", store, line)
		}
		view.Lines = append(view.Lines, parsed)
	}
	return view, nil
}

func (h *Host) AnswerMemory(id string, scope memory.Scope) bool {
	return h.memoryPicks.answer(id, scope)
}

func pickMemory(emit func(Event), book *replies[memory.Scope]) tools.MemoryPick {
	return func(ctx context.Context, offer tools.MemoryOffer) (memory.Scope, error) {
		shown, err := json.Marshal(map[string]string{"statement": offer.Statement, "scope": string(offer.Scope), "said": offer.Said})
		if err != nil {
			return "", err
		}
		id := session.NewEventID()
		reply := book.wait(id)
		defer book.forget(id)
		emit(Event{Kind: EventDecision, ID: id, Decision: &Decision{Tool: turn.RememberToolName, Verdict: Ask, Remembers: offer.Statement, MemoryScope: offer.Scope, MemoryScopes: offer.Scopes}})
		accepts := []ApprovalDecision{}
		for _, scope := range offer.Scopes {
			accepts = append(accepts, rememberedAs(scope))
		}
		emit(Event{Kind: EventAwaitPerson, ID: id, Tool: turn.RememberToolName, Args: shown, Accepts: append(accepts, RejectOnce, RejectAlways, Cancelled)})
		defer emit(Event{Kind: EventResumed, ID: id})
		select {
		case picked := <-reply:
			emit(Event{Kind: EventMemoryScoped, ID: id, Scoped: &MemoryScoped{Statement: offer.Statement, Offered: offer.Scope, Picked: cmp.Or(string(picked), MemoryNone), Scopes: offer.Scopes, By: ScopedByPerson}})
			return picked, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
