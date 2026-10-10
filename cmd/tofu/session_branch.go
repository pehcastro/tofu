package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/llm"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/turn"
)

const sessionBranchUsage = "tofu session branch <name|id> --side [--preset read|notes|files | --owns GLOB,GLOB] [--seed summary|none] [--name NAME] [--model ID] [--effort LEVEL] [--at EVENT] [--json]"

type sessionBranchAsk struct {
	handle, preset, owns, seed, name, model, effort, at string
	side                                                bool
}

type sessionBranchReport struct {
	Session string          `json:"session"`
	Handle  string          `json:"handle"`
	Kind    session.Kind    `json:"kind"`
	Parent  session.Carried `json:"parent"`
	Owns    []string        `json:"owns"`
	Preset  string          `json:"preset,omitempty"`
	Seed    turn.Seed       `json:"seed"`
	Carried int             `json:"carried_messages"`
	Model   string          `json:"model,omitempty"`
	Effort  llm.Effort      `json:"effort,omitempty"`

	parentHandle string
}

func sessionBranchArgs(args []string) (sessionBranchAsk, error) {
	ask := sessionBranchAsk{seed: string(turn.SeedSummary)}
	valued := map[string]*string{"--preset": &ask.preset, "--owns": &ask.owns, "--seed": &ask.seed, "--name": &ask.name, "--model": &ask.model, "--effort": &ask.effort, "--at": &ask.at}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch target, takes := valued[arg]; {
		case arg == jsonFlag:
		case arg == "--side":
			ask.side = true
		case takes && index+1 < len(args):
			index++
			*target = args[index]
		case takes:
			return ask, fmt.Errorf("%s wants a value", arg)
		case strings.HasPrefix(arg, "-") || ask.handle != "":
			return ask, fmt.Errorf("unknown argument %q", arg)
		default:
			ask.handle = arg
		}
	}
	switch {
	case ask.handle == "":
		return ask, errors.New("which session? an id or a name names the one to branch from")
	case !ask.side:
		return ask, errors.New("only --side is built: a full branch for the Forks screen is not")
	case ask.preset != "" && ask.owns != "":
		return ask, errors.New("name --preset or --owns, not both")
	}
	return ask, nil
}

func sessionBranchAccess(ask sessionBranchAsk) (turn.Access, error) {
	switch {
	case ask.owns != "":
		return turn.OwnsAccess(strings.Split(ask.owns, ","))
	case ask.preset == "":
		return turn.AccessOf(settingText(".", settingspkg.SideChatAccess, nil))
	}
	access, err := turn.AccessOf(ask.preset)
	if err == nil && access.Preset == "" {
		err = fmt.Errorf("preset %q is none of %s, %s and %s", ask.preset, turn.PresetRead, turn.PresetNotes, turn.PresetFiles)
	}
	return access, err
}

func sessionBranch(store *session.Store, ask sessionBranchAsk) (sessionBranchReport, error) {
	parent, err := sessionHeader(store, ask.handle)
	if err != nil {
		return sessionBranchReport{}, err
	}
	access, err := sessionBranchAccess(ask)
	if err != nil {
		return sessionBranchReport{}, err
	}
	var effort llm.Effort
	if ask.effort != "" {
		if effort, err = llm.ParseEffort(ask.effort); err != nil {
			return sessionBranchReport{}, err
		}
	}
	header, carried, err := turn.BranchSide(store, parent, turn.SideBranch{Access: access, Seed: turn.Seed(ask.seed), Model: ask.model, Effort: effort, From: ask.at}, time.Now())
	if err != nil {
		return sessionBranchReport{}, err
	}
	if ask.name != "" {
		if _, err := store.SetName(header.ID, ask.name); err != nil {
			return sessionBranchReport{}, fmt.Errorf("side chat %s was made and keeps its own name, as %q is not one: %w", handleOf(store, header.ID), ask.name, err)
		}
	}
	return sessionBranchReport{Session: header.ID, Handle: handleOf(store, header.ID), Kind: header.Kind, Parent: *header.BranchedFrom, Owns: append([]string{}, header.Owns...),
		Preset: header.Preset, Seed: turn.Seed(ask.seed), Carried: carried, Model: header.Model, Effort: llm.Effort(header.Effort), parentHandle: handleOf(store, parent.ID)}, nil
}

func sessionBranchLines(page cli.Page, report sessionBranchReport) []string {
	access := "writes " + strings.Join(report.Owns, ", ")
	if len(report.Owns) == 0 {
		access = "writes nothing"
	}
	if report.Preset != "" {
		access = report.Preset + ": " + access
	}
	lines := append(page.Title("Side chat", []string{report.Handle}, cli.Verdict{Mark: cli.Changed, Text: "branched"}), "")
	facts := []cli.Fact{
		{Label: "id", Text: report.Session},
		{Label: "parent", Text: strings.TrimSuffix(report.parentHandle+" at "+report.Parent.Event, " at ")},
		{Label: "access", Text: access},
		{Label: "seed", Text: string(report.Seed) + ", " + plural(report.Carried, "message")},
	}
	if report.Model != "" {
		facts = append(facts, cli.Fact{Label: "model", Text: report.Model})
	}
	if report.Effort != "" {
		facts = append(facts, cli.Fact{Label: "effort", Text: string(report.Effort)})
	}
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(append(lines, ""), cli.Indent(page.Hint("tofu session resume "+report.Handle))...)
}
