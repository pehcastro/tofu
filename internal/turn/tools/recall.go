package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/memtree"
	"tofu/internal/turn"
)

type Recall struct {
	Zoom *Zoom
}

func (Recall) Name() string { return "recall" }

func (Recall) Definition() llm.Tool {
	return llm.Tool{
		Name: "recall",
		Description: "searches the raw items of a memory store with a regular expression and returns every item that matches, word for word, as store id+1|kind: text. " +
			"store is episodes for the conversation, or a memory scope; left out, every store is searched. " + wholeBeforeYouAct,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"store": map[string]any{"type": "string", "enum": memoryStores()},
				"regex": map[string]any{"type": "string"},
			},
			"required": []string{"regex"},
		},
	}
}

func (r Recall) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Store string `json:"store"`
		Regex string `json:"regex"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("recall: arguments are not the expected shape: %w", err)
	}
	logs, err := memory.Trees(r.Zoom.Project)
	if err != nil {
		return turn.Result{}, fmt.Errorf("recall: %w", err)
	}
	searched := memoryStores()
	if args.Store != "" {
		searched = []string{args.Store}
	}
	var found []string
	for _, name := range searched {
		log, kept := logs[name]
		if !kept {
			continue
		}
		lines, err := r.Zoom.open(log, func(store *memtree.Store) ([]string, error) { return store.Recall(args.Regex) })
		if err != nil {
			return turn.Result{}, fmt.Errorf("recall: %s: %w", name, err)
		}
		for _, line := range lines {
			found = append(found, name+" "+line)
		}
	}
	command := "recall " + strconv.Quote(args.Regex)
	if len(found) == 0 {
		return turn.Result{Content: "nothing in " + strings.Join(searched, ", ") + " matches " + strconv.Quote(args.Regex), Command: command}, nil
	}
	return turn.Result{Content: strings.Join(found, "\n"), Command: command}, nil
}
