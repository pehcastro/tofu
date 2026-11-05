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
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	"tofu/internal/store/memory/entries"
	memtree "tofu/internal/store/memory/tree"
	"tofu/internal/store/session"
	"tofu/internal/tools/human/remember"
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
	Statement string          `json:"statement"`
	Offered   entries.Scope   `json:"offered"`
	Picked    string          `json:"picked"`
	Scopes    []entries.Scope `json:"scopes,omitempty"`
	By        string          `json:"by"`
}

const (
	ScopedByPerson = "person"
	ScopedByAuto   = "auto"
)

func keptMemory(emit func(Event)) func(remember.MemoryKept) {
	return func(kept remember.MemoryKept) {
		emit(Event{Kind: EventMemoryScoped, ID: session.NewEventID(), Scoped: &MemoryScoped{Statement: kept.Statement, Offered: kept.Offered, Picked: string(kept.Kept), By: ScopedByAuto}})
	}
}

type MemoryChanged struct {
	Scope entries.Scope `json:"scope"`
	File  string        `json:"file"`
}

type memoryStamp struct {
	scope    entries.Scope
	size     int64
	modified time.Time
}

func memoryStamps(shelves []entries.Shelf) map[string]memoryStamp {
	stamps := map[string]memoryStamp{}
	for _, shelf := range shelves {
		for _, file := range shelf.EntryFiles() {
			if info, err := os.Stat(file); err == nil {
				stamps[file] = memoryStamp{scope: shelf.Scope, size: info.Size(), modified: info.ModTime()}
			}
		}
	}
	return stamps
}

func (s *server) watchMemory(quit <-chan struct{}) {
	placed, err := entries.Placed(s.Dir)
	if err != nil {
		return
	}
	shelves := placed.Shelves()
	seen := memoryStamps(shelves)
	every := time.NewTicker(konst.ServeShellPollMillis * time.Millisecond)
	defer every.Stop()
	changed := func(file string, stamp memoryStamp) {
		s.box.push(merged("memory.changed", file, &MemoryChanged{Scope: stamp.scope, File: file}))
	}
	for {
		select {
		case <-quit:
			return
		case <-every.C:
		}
		now := memoryStamps(shelves)
		for file, stamp := range now {
			if seen[file] != stamp {
				changed(file, stamp)
			}
		}
		for file, stamp := range seen {
			if _, still := now[file]; !still {
				changed(file, stamp)
			}
		}
		seen = now
	}
}

type MemoryScopedEvent struct {
	Identity
	MemoryScoped
}

func (s *server) memoryRead(named string, read func(*memtree.Store) ([]string, error)) (MemoryView, error) {
	store := named
	if named != entries.EpisodesStore {
		scope, err := entries.ParseScope(named)
		if err != nil {
			return MemoryView{}, &Refusal{Code: CodeBadParams, Message: "a store is " + entries.EpisodesStore + " or a scope: " + err.Error()}
		}
		if _, err := entries.Block(s.Dir); err != nil {
			return MemoryView{}, err
		}
		store = string(scope)
	}
	logs, err := entries.Trees(s.Dir)
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

func (h *Host) AnswerMemory(id string, scope entries.Scope) bool {
	return h.answerMemory(id, scope, nil)
}

func (h *Host) answerMemory(id string, scope entries.Scope, said *Resolution) bool {
	if f := h.following.Load(); f != nil {
		return f.ask(followAsk{Op: followMemory, Asked: id, Scope: scope, Said: said}).OK
	}
	said = cmp.Or(said, h.noteOf(id))
	return h.memoryPicks.answer(id, scope, func() {
		if said != nil {
			h.bind(id, *said)
		}
	})
}

func pickMemory(emit func(Event), book *replies[entries.Scope]) remember.MemoryPick {
	return func(ctx context.Context, offer remember.MemoryOffer) (entries.Scope, error) {
		shown, err := json.Marshal(map[string]string{"statement": offer.Statement, "scope": string(offer.Scope), "said": offer.Said})
		if err != nil {
			return "", err
		}
		id := session.NewEventID()
		reply := book.wait(id)
		defer book.forget(id)
		emit(Event{Kind: EventDecision, ID: id, Decision: &Decision{Tool: loop.RememberToolName, Verdict: Ask, Remembers: offer.Statement, MemoryScope: offer.Scope, MemoryScopes: offer.Scopes}})
		accepts := []ApprovalDecision{}
		for _, scope := range offer.Scopes {
			accepts = append(accepts, RememberedAs(scope))
		}
		emit(Event{Kind: EventAwaitPerson, ID: id, Tool: loop.RememberToolName, Args: shown, Accepts: append(accepts, RejectOnce, RejectAlways, Cancelled)})
		resumed := Event{Kind: EventResumed, ID: id, Resolution: cancelledByTofu()}
		defer func() { emit(resumed) }()
		select {
		case picked := <-reply:
			resumed.Resolution = nil
			emit(Event{Kind: EventMemoryScoped, ID: id, Scoped: &MemoryScoped{Statement: offer.Statement, Offered: offer.Scope, Picked: cmp.Or(string(picked), MemoryNone), Scopes: offer.Scopes, By: ScopedByPerson}})
			return picked, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
