package main

import (
	"cmp"
	"slices"

	"tofu/internal/agent/hooks"
	"tofu/internal/agent/loop"
	"tofu/internal/cli/wiring"
	"tofu/internal/host"
	"tofu/internal/store/session"
)

type runResumed struct {
	carry    sessionResume
	wire     string
	prefix   *turn.Prefix
	pick     host.Pick
	pickFrom string
	release  func() error
}

func resumeRun(dir, handle string, say func(string)) (runResumed, error) {
	if handle == "" {
		return runResumed{}, nil
	}
	dir = cmp.Or(dir, ".")
	store, err := session.OpenIn(dir)
	if err != nil {
		return runResumed{}, err
	}
	carry, err := resumeOf(store, handle)
	if err != nil {
		return runResumed{}, err
	}
	pick, err := host.SessionPick(store, dir, carry.Session)
	if err != nil {
		return runResumed{}, err
	}
	release, err := store.Hold(carry.Session)
	if err != nil {
		return runResumed{}, err
	}
	resumed := runResumed{carry: carry, prefix: &turn.Prefix{}, pick: pick, pickFrom: "the model tofu session set chose", release: release}
	if pick.Model == "" {
		resumed.pick, resumed.pickFrom = lastAsked(store, dir, carry.Session)
	}
	if header, err := store.Header(carry.Session); err == nil {
		resumed.wire = header.Wire
	}
	if prefix, err := turn.HeldPrefix(store, carry.Session, ""); err == nil {
		resumed.prefix = prefix
	} else {
		say("the system prompt and tools " + carry.Session + " first sent were not read back, so this turn sends them anew: " + err.Error())
	}
	return resumed, nil
}

func lastAsked(store *session.Store, dir, id string) (host.Pick, string) {
	exchanges, _ := store.Exchanges(id)
	lead := slices.DeleteFunc(exchanges, func(exchange session.Exchange) bool { return exchange.Agent != "" || exchange.Model == "" })
	if len(lead) == 0 {
		return host.Pick{}, "the orchestrator binding: the session records no pick and no request"
	}
	last := lead[len(lead)-1]
	library, _ := wiring.ModelLibrary(dir)
	if model, known := library.Resolve(last.Model); known && library.WireOf(model) == last.Wire {
		return host.Pick{Wire: last.Wire, Model: model.Slug()}, "the session's last request, which " + last.Model + " answered"
	}
	return host.Pick{}, "the orchestrator binding: the session records no pick and its last request was answered by " + last.Model + ", which is no model in the library"
}

func (r runResumed) onItsModel(opts wiring.RunOpts) (wiring.RunOpts, string) {
	if r.carry.Session == "" {
		return opts, ""
	}
	opts.Session = r.carry.Session
	switch {
	case opts.Model != "":
		return opts, "--model"
	case r.pick.Model == "":
		return opts, r.pickFrom
	case opts.Wire != "" && opts.Wire != r.pick.Wire:
		return opts, "the orchestrator binding on --wire " + opts.Wire + ", which is not " + r.pick.Model + "'s"
	}
	opts.Wire, opts.Model = r.pick.Wire, r.pick.Model
	return opts, r.pickFrom
}

func (r runResumed) into(config *turn.Config) {
	if r.carry.Session == "" {
		return
	}
	config.History, config.Prefix, config.SessionSource = r.carry.messages, r.prefix, hook.SessionResumed
	if r.wire != "" && r.wire != config.Wire {
		config.History = turn.WithoutThinking(config.History)
	}
}

func (r runResumed) letGo() {
	if r.release != nil {
		_ = r.release()
	}
}
