package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/memtree"
	"tofu/internal/turn"
)

const wholeBeforeYouAct = "zoom until you have the part you need whole before you act on it, guess at it or ask the person about it: a summary line is a pointer, not the thing. "

func memoryStores() []string {
	return []string{memory.EpisodesStore, string(memory.Global), string(memory.Project), string(memory.UserLocal), string(memory.ProjectLocal)}
}

type Zoom struct {
	Project   string
	Compactor func(turn.RecordedAsk) (memtree.Compact, func())
	held      sync.Mutex
}

func (*Zoom) Name() string { return "zoom" }

func (*Zoom) Definition() llm.Tool {
	return llm.Tool{
		Name: "zoom",
		Description: "opens one line of a memory view into the two lines it sums up, and a line of one item into the item itself, word for word. " +
			"store is episodes for the conversation view, or a memory scope; id and n are the line's id+n. " + wholeBeforeYouAct +
			"recall finds the words when no line points at them.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"store": map[string]any{"type": "string", "enum": memoryStores()},
				"id":    map[string]any{"type": "integer"},
				"n":     map[string]any{"type": "integer"},
			},
			"required": []string{"store", "id", "n"},
		},
	}
}

func (z *Zoom) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Store string `json:"store"`
		ID    int    `json:"id"`
		N     int    `json:"n"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("zoom: arguments are not the expected shape: %w", err)
	}
	logs, err := memory.Trees(z.Project)
	if err != nil {
		return turn.Result{}, fmt.Errorf("zoom: %w", err)
	}
	log, kept := logs[args.Store]
	if !kept {
		return turn.Result{}, fmt.Errorf("zoom: %s holds nothing in this project yet", args.Store)
	}
	lines, err := z.open(log, func(store *memtree.Store) ([]string, error) { return store.Zoom(args.ID, args.N) })
	if err != nil {
		return turn.Result{}, fmt.Errorf("zoom: %w", err)
	}
	return turn.Result{Content: strings.Join(lines, "\n"), Command: "zoom " + args.Store + " " + strconv.Itoa(args.ID) + "+" + strconv.Itoa(args.N)}, nil
}

func (z *Zoom) open(log string, reading func(*memtree.Store) ([]string, error)) ([]string, error) {
	z.held.Lock()
	defer z.held.Unlock()
	store, err := memtree.Open(log)
	if err != nil {
		return nil, err
	}
	lines, err := reading(store)
	if closed := store.Close(); err == nil {
		err = closed
	}
	return lines, err
}

func (z *Zoom) Episodes(budget int) (string, error) {
	z.held.Lock()
	defer z.held.Unlock()
	view, _, err := memory.EpisodeView(z.Project, budget, nil)
	return view, err
}

func (z *Zoom) Keep(kind, text string) error {
	z.held.Lock()
	defer z.held.Unlock()
	return memory.KeepEpisode(z.Project, memtree.Item{Kind: kind, Text: text})
}

func (z *Zoom) Compact(ask turn.RecordedAsk) error {
	compact, release := z.Compactor(ask)
	defer release()
	z.held.Lock()
	defer z.held.Unlock()
	_, built, err := memory.EpisodeView(z.Project, konst.MemtreeViewBytes, compact)
	if err == nil && len(built.Failed) > 0 {
		err = fmt.Errorf("%d of %d episode summaries failed and are tried after the next turn, the first: %w", len(built.Failed), built.Calls, built.Failed[0])
	}
	return err
}
