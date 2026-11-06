package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	digesttool "tofu/internal/tools/agents/digest"
	"tofu/internal/tools/contract"
)

func newDigestTool(dir string) (contract.Tool, error) {
	sessions, err := session.OpenIn(dir)
	if err != nil {
		return nil, err
	}
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	open := wireOpener()
	cheap := subagents.TierDumb.Setting()
	return digesttool.Digest{Sessions: sessions, Artifacts: recall.NewStore(filepath.Join(places.State, "artifacts")), Bind: func(ctx context.Context) (digesttool.Bound, error) {
		slug := strings.TrimSpace(settingspkg.TextSaying(dir, cheap, nil))
		if slug == "" {
			return digesttool.Bound{}, errors.New("digest asks the cheap model " + cheap + " names, and none is set: tofu settings set " + cheap + " <source/model>")
		}
		library, err := wiring.ModelLibrary(dir)
		if err != nil {
			return digesttool.Bound{}, err
		}
		model, err := library.Select(slug)
		if err != nil {
			return digesttool.Bound{}, err
		}
		opts := wiring.RunOpts{Dir: dir, Wire: library.WireOf(model), Model: slug}
		if len(model.Efforts) > 0 {
			opts.Effort = wiring.DefaultEffort(model.Efforts)
		}
		budget, err := wiring.ContextBudget(opts, model)
		if err != nil {
			return digesttool.Bound{}, err
		}
		wire, err := open(opts)
		if wire.Held != nil && err != nil {
			wire.Held.Close()
		}
		if err != nil {
			return digesttool.Bound{}, err
		}
		account, err := wire.Held.ForTurn().Pick(ctx)
		guarded, guardErr := wiring.Guarded(account.Model, budget)
		if err = errors.Join(err, guardErr); err != nil {
			wire.Held.Close()
			return digesttool.Bound{}, err
		}
		return digesttool.Bound{Model: guarded, Slug: slug, Subscription: wire.Spend == loop.SpendSubscription, Budget: budget, Release: wire.Held.Close}, nil
	}}, nil
}
