package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"tofu/interface/tui/frame"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	"tofu/internal/recall"
	settingspkg "tofu/internal/settings"
	"tofu/internal/turn"
)

type continuing struct {
	dir, from string
	signedIn  []string
	budgetOf  func(models.Model) (recall.Budget, error)
	serving   func(context.Context, models.Model) bool
	open      func(context.Context, models.Model, recall.Budget) (turn.Account, turn.Accounts, error)
}

func (c continuing) move(ctx context.Context, left []string, conversationTokens int) (turn.Moved, error) {
	store, err := openSettings(c.dir)
	if err != nil {
		return turn.Moved{}, fmt.Errorf("continuity could not be read, so the work stops here: %w", err)
	}
	label := func(key string) string {
		table := store.Table()
		return table[slices.IndexFunc(table, func(spec settingspkg.Spec) bool { return spec.Key == key })].Label
	}
	commas := func(key string) []string {
		return strings.FieldsFunc(store.Text(key), func(r rune) bool { return r == ',' || r == ' ' })
	}
	type candidate struct{ slug, setting string }
	var candidates []candidate
	for _, slug := range commas(settingspkg.ContinueOnSubscription) {
		candidates = append(candidates, candidate{slug, settingspkg.ContinueOnSubscription})
	}
	order := commas(settingspkg.SubscriptionOrder)
	rank := func(next candidate) int {
		source, _, _ := strings.Cut(next.slug, "/")
		if at := slices.Index(order, source); at >= 0 {
			return at
		}
		return len(order)
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return cmp.Compare(rank(a), rank(b)) })
	if api := store.Text(settingspkg.ContinueOnAPIModel); api != "" {
		candidates = append(candidates, candidate{api, settingspkg.ContinueOnAPIModel})
	}
	if len(candidates) == 0 {
		fields := label(settingspkg.ContinueOnAPIModel)
		if c.subscriptionProviders() > 1 {
			fields = label(settingspkg.ContinueOnSubscription) + " or " + fields
		}
		return turn.Moved{}, errors.New("continuity is not set, so the work stops here: set " + fields + " in Settings > " + settingspkg.ModelsCategory)
	}
	library, err := modelLibrary(c.dir)
	if err != nil {
		return turn.Moved{}, err
	}
	gone := append(slices.Clone(left), c.from)
	var why []string
	for _, next := range candidates {
		model, err := library.Select(next.slug)
		var budget recall.Budget
		if err == nil {
			budget, err = c.budgetOf(model)
		}
		if err == nil {
			err = budget.RefuseOverWindow(conversationTokens)
		}
		switch {
		case err != nil:
			why = append(why, next.slug+": "+err.Error())
		case (model.Pays() == models.PaysKey) != (next.setting == settingspkg.ContinueOnAPIModel):
			why = append(why, next.slug+" is not paid the way "+label(next.setting)+" asks")
		case slices.Contains(gone, model.Source()):
			why = append(why, model.Source()+" is already at its limit")
		case model.Pays() == models.PaysSubscription && !c.serving(ctx, model):
			why = append(why, next.slug+" has no account with room left")
		default:
			account, accounts, err := c.open(ctx, model, budget)
			if err != nil {
				why = append(why, next.slug+": "+err.Error())
				continue
			}
			wire := library.WireOf(model)
			return turn.Moved{From: c.from, To: next.slug, Setting: label(next.setting), Account: account, Accounts: accounts, Wire: wire, Spend: wireSpend(wire), Budget: budget}, nil
		}
	}
	return turn.Moved{}, errors.New("continuity has nowhere to go, so the work stops here: " + strings.Join(why, "; "))
}

func (c continuing) subscriptionProviders() int {
	var sources []string
	for _, source := range c.signedIn {
		if slices.Contains(models.AllSubscriptions(), models.Subscription(source)) && !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	return len(sources)
}

func (a *accounts) wrapping(wrap func(turn.Model, recall.Budget) (turn.Model, error), budget recall.Budget, opts runOpts) {
	a.wrap, a.budget = wrap, budget
	a.budgetOf = func(model models.Model) (recall.Budget, error) { return contextBudget(opts, model) }
}

func (a *accounts) budgetFor(model models.Model) (recall.Budget, error) {
	if a.budgetOf == nil {
		return a.budget, nil
	}
	return a.budgetOf(model)
}

func (a *accounts) on(model models.Model, budget recall.Budget) (*accounts, error) {
	spec, err := cred.Lookup(string(model.Subscription))
	if err != nil {
		return nil, err
	}
	effort := a.effort
	if !slices.Contains(model.Efforts, effort) {
		effort = ""
	}
	return &accounts{dir: a.dir, provider: cred.Provider(model.Subscription), spec: spec, store: a.store, modelID: model.ID, spends: model.Windows, effort: effort,
		urls: a.urls, now: a.now, wrap: a.wrap, budget: budget, budgetOf: a.budgetOf, verbosity: model.Verbosity, reasoningContext: model.ReasoningContext}, nil
}

func (a *accounts) continuing() continuing {
	var signedIn []string
	if rows, err := a.store.List(); err == nil {
		for _, row := range rows {
			if row.Unusable(a.now()) == "" {
				signedIn = append(signedIn, string(row.Credential.Provider))
			}
		}
	}
	return continuing{dir: a.dir, from: string(a.provider), signedIn: signedIn, budgetOf: a.budgetFor,
		serving: func(ctx context.Context, model models.Model) bool {
			other, err := a.on(model, a.budget)
			if err != nil {
				return false
			}
			candidates, _, err := other.read(ctx)
			return err == nil && slices.ContainsFunc(candidates, func(candidate quota.Candidate) bool { return !quota.Spent(candidate.Report, model.Windows, a.now()) })
		},
		open: func(ctx context.Context, model models.Model, budget recall.Budget) (turn.Account, turn.Accounts, error) {
			if model.Pays() == models.PaysKey {
				account, err := a.keyed(model, budget)
				return account, turn.Accounts{}, err
			}
			other, err := a.on(model, budget)
			if err != nil {
				return turn.Account{}, turn.Accounts{}, err
			}
			account, err := other.pick(ctx)
			return account, other.forTurn(), err
		},
	}
}

func (a *accounts) keyed(model models.Model, budget recall.Budget) (turn.Account, error) {
	var opened turn.Model
	var err error
	switch model.Provider {
	case models.OpenRouter:
		opened, err = keyModel(model.ID)
	case models.Meta:
		opened, err = metaModel(model, model.EffortTaken(a.effort))
	default:
		err = fmt.Errorf("tofu has no wire that runs %s on a key", model.Slug())
	}
	if err != nil {
		return turn.Account{}, err
	}
	keyed := &accounts{wrap: a.wrap, budget: budget}
	wrapped, err := keyed.wrapped(opened)
	return turn.Account{Model: wrapped}, err
}

func (s standIns) continuing() continuing {
	signedIn := make([]string, len(s.readings))
	for i, reading := range s.readings {
		signedIn[i] = reading.Source()
	}
	return continuing{dir: s.held.dir, from: s.source, signedIn: signedIn, budgetOf: s.held.budgetFor,
		serving: func(_ context.Context, model models.Model) bool {
			return slices.ContainsFunc(s.readings, func(reading frame.Quota) bool { return reading.Source() == model.Source() && reading.Fraction < 1 })
		},
		open: func(ctx context.Context, model models.Model, budget recall.Budget) (turn.Account, turn.Accounts, error) {
			held := &accounts{dir: s.held.dir, fixed: s.held.fixed, now: s.held.now, wrap: s.held.wrap, budget: budget, budgetOf: s.held.budgetOf}
			moved := standIns{held: held, source: model.Source(), readings: s.readings}
			if model.Pays() == models.PaysKey {
				account, err := held.pick(ctx)
				return account, turn.Accounts{}, err
			}
			account, err := moved.account(ctx, moved.mine()[0])
			return account, moved.forTurn(), err
		},
	}
}
