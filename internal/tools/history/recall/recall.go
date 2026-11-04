package recall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"tofu/internal/models/llm/core"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/memory/tree"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
	"tofu/internal/tools/history/quote"
	"tofu/internal/tools/history/zoom"
)

type Recall struct {
	Zoom     *zoom.Zoom
	Sessions *session.Store
	Session  string
}

func (Recall) Name() string { return "recall" }

func (Recall) Definition() core.Tool {
	return core.Tool{
		Name: "recall",
		Description: "searches the raw items of a memory store with a regular expression and returns every item that matches, word for word, as store id+1|kind: text. " +
			"store is episodes for the conversation, or a memory scope; left out, every store is searched. " +
			"a regex that is a reference, [tool#abc12] or [quote#abc12] or #abc12, or the id of one of your own tool calls, returns that recorded call or turn instead, from this session or any it was forked from. " + zoom.WholeBeforeYouAct,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"store": map[string]any{"type": "string", "enum": zoom.MemoryStores()},
				"regex": map[string]any{"type": "string"},
			},
			"required": []string{"regex"},
		},
	}
}

func (r Recall) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Store string `json:"store"`
		Regex string `json:"regex"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("recall: arguments are not the expected shape: %w", err)
	}
	hash, named := quote.Hash(args.Regex)
	if strings.Trim(hash, "# ") == "" {
		return contract.Result{}, errors.New("recall: give a regex to search the memory stores, or a reference such as [tool#abc12] or a call id to read that call")
	}
	wireID := !named && !strings.ContainsAny(hash, " .*+?()[]{}|^$\\") && strings.Contains(hash, "_")
	noCall := ""
	if (named || wireID) && r.Sessions != nil {
		found, err := quote.ResolveQuote(r.Sessions, r.Session, args.Regex)
		if err != nil {
			return contract.Result{}, fmt.Errorf("recall: %w", err)
		}
		said := contract.Result{Command: "recall " + hash}
		switch found.Outcome {
		case quote.QuoteNotFound:
			noCall = "no call or turn ends with " + hash + " in this session or any it was forked from"
			said.Content = noCall
		case quote.QuoteAmbiguous:
			said.Content = "more than one item in " + found.Session + " ends with " + hash + ": give more of the id"
		case quote.QuoteItem:
			body, note := quote.Cut(found.Noun, found.Words)
			said.Content = glob.WithNote(found.Ref+", "+found.Speaker+", recorded in "+found.Session+"\n"+body+"\n", note)
		default:
			panic("recall: unknown outcome " + string(found.Outcome))
		}
		if !wireID || found.Outcome != quote.QuoteNotFound {
			return said, nil
		}
	}
	logs, err := entries.Trees(r.Zoom.Project)
	if err != nil {
		return contract.Result{}, fmt.Errorf("recall: %w", err)
	}
	searched := zoom.MemoryStores()
	if args.Store != "" {
		searched = []string{args.Store}
	}
	var found []string
	for _, name := range searched {
		log, kept := logs[name]
		if !kept {
			continue
		}
		lines, err := r.Zoom.Open(log, func(store *tree.Store) ([]string, error) { return store.Recall(args.Regex) })
		if err != nil {
			return contract.Result{}, fmt.Errorf("recall: %s: %w", name, err)
		}
		for _, line := range lines {
			found = append(found, name+" "+line)
		}
	}
	command := "recall " + strconv.Quote(args.Regex)
	if len(found) == 0 {
		missing := "nothing in " + strings.Join(searched, ", ") + " matches " + strconv.Quote(args.Regex)
		if noCall != "" {
			missing = noCall + ", and " + missing
		}
		return contract.Result{Content: missing, Command: command}, nil
	}
	return contract.Result{Content: strings.Join(found, "\n"), Command: command}, nil
}
