package main

import (
	"context"
	"os"
	"time"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/tools/agents/race"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/scratch/whatif"
	"tofu/internal/tools/web/browser"
)

func newRaceTool(dir string) (contract.Tool, error) {
	limits := race.Limits{
		Whatif:  whatif.Limits{Check: konst.WhatifCheckMillis * time.Millisecond, StaleOverlay: konst.OverlayStaleMinutes * time.Minute},
		Entrant: konst.SubAgentWallClockSeconds * time.Second,
		Fewest:  konst.RaceBriefsFewest,
		Most:    konst.RaceBriefsMost,
	}
	open := wireOpener()
	return race.New(dir, limits, func(ctx context.Context, tree, brief string) (race.Ran, error) {
		opts, err := parseRunArgs([]string{"--dir", tree, "--no-subagents", brief})
		if err != nil {
			return race.Ran{}, err
		}
		bound := wiring.OnTheBoundWire(wiring.RunOpts{Dir: dir})
		opts.Wire, opts.Model = bound.Wire, bound.Model
		project, err := platformproject.OpenProject(tree)
		if err != nil {
			return race.Ran{}, err
		}
		project.Temporary = true
		if err := project.Register(); err != nil {
			return race.Ran{}, err
		}
		places, err := platformproject.ResolvePlaces(tree)
		if err != nil {
			return race.Ran{}, err
		}
		home, _ := os.UserHomeDir()
		warm := newWarmProcesses(browser.NewBrowserTabs(home))
		defer warm.Close()
		engine := &appEngine{dir: tree, open: open, tabs: warm.tabs, warm: warm, places: places}
		same, quiet := func(model loop.Model) loop.Model { return model }, func(string) {}
		started, err := engine.startTurn(settled(opts, quiet), host.Turn{}, host.Hooks{Lead: same, SubAgent: same, Say: quiet, Reads: contract.NewReadLedger()}, clockFromNow())
		if started.Close != nil {
			defer started.Close()
		}
		if err != nil {
			return race.Ran{}, err
		}
		config := started.Config
		config.TaskOrigin = core.Origin{Source: core.SourceBrief}
		row, err := loop.Run(ctx, config)
		ran := race.Ran{Model: row.Model, USD: row.TotalCostUSD, Subscription: row.Spend == loop.SpendSubscription, Requests: len(row.Steps)}
		for _, step := range row.Steps {
			ran.TokensIn += step.PromptTokens + step.CacheReadTokens + step.CacheWriteTokens
			ran.TokensOut += step.CompletionTokens
		}
		return ran, err
	})
}
