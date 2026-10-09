package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

func claimedTools(registry Registry, store *session.Store, own string) Registry {
	kept := slices.Clone(registry.tools)
	for i, tool := range kept {
		if tool.Name() == "write" || tool.Name() == "edit" {
			kept[i] = claimedWrite{tool: tool, store: store, own: own}
		}
	}
	return NewRegistry(kept...)
}

type claimedWrite struct {
	tool  Tool
	store *session.Store
	own   string
}

func (t claimedWrite) Name() string { return t.tool.Name() }

func (t claimedWrite) Definition() llm.Tool { return t.tool.Definition() }

func (t claimedWrite) refusal(raw json.RawMessage) error {
	if inner, checks := t.tool.(bounded); checks {
		if err := inner.refusal(raw); err != nil {
			return err
		}
	}
	path, err := pathArg(t.Name(), raw)
	if err != nil {
		return err
	}
	claims, err := t.store.Claims()
	if err != nil {
		return fmt.Errorf("%s: the side chats' claims do not read, so nothing is written: %w", t.Name(), err)
	}
	for _, claim := range claims {
		if held, _ := subagent.Matches(path, claim.Owns); held && claim.Session != t.own {
			return fmt.Errorf("%s refused: %s is held by side chat %s while its turn runs: write it when that turn ends, or leave it to that chat", t.Name(), path, cmp.Or(claim.Name, claim.Session))
		}
	}
	return nil
}

func (t claimedWrite) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := t.refusal(raw); err != nil {
		return Result{}, err
	}
	return t.tool.Run(ctx, raw)
}
