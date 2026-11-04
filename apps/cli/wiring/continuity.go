package wiring

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	cred "tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/quota"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/ui/tui/frame/frame"
)

type Continuing struct {
	dir, from string
	signedIn  []string
	budgetOf  func(catalog.Model) (recall.Budget, error)
	serving   func(context.Context, catalog.Model) bool
	open      func(context.Context, catalog.Model, recall.Budget) (loop.Account, loop.Accounts, error)
}

func (c Continuing) move(ctx context.Context, left []string, conversationTokens int) (loop.Moved, error) {
	store, err := settingspkg.OpenFor(c.dir)
	if err != nil {
		return loop.Moved{}, fmt.Errorf("continuity could not be read, so the work stops here: %w", err)
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
		return loop.Moved{}, errors.New("continuity is not set, so the work stops here: set " + fields + " in Settings > " + settingspkg.ModelsCategory)
	}
	library, err := ModelLibrary(c.dir)
	if err != nil {
		return loop.Moved{}, err
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
		case (model.Pays() == catalog.PaysKey) != (next.setting == settingspkg.ContinueOnAPIModel):
			why = append(why, next.slug+" is not paid the way "+label(next.setting)+" asks")
		case slices.Contains(gone, model.Source()):
			why = append(why, model.Source()+" is already at its limit")
		case model.Pays() == catalog.PaysSubscription && !c.serving(ctx, model):
			why = append(why, next.slug+" has no account with room left")
		default:
			account, accounts, err := c.open(ctx, model, budget)
			if err != nil {
				why = append(why, next.slug+": "+err.Error())
				continue
			}
			wire := library.WireOf(model)
			return loop.Moved{From: c.from, To: next.slug, Setting: label(next.setting), Account: account, Accounts: accounts, Wire: wire, Spend: WireSpend(wire), Budget: budget}, nil
		}
	}
	return loop.Moved{}, errors.New("continuity has nowhere to go, so the work stops here: " + strings.Join(why, "; "))
}

func (c Continuing) subscriptionProviders() int {
	var sources []string
	for _, source := range c.signedIn {
		if slices.Contains(catalog.AllSubscriptions(), catalog.Subscription(source)) && !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	return len(sources)
}

func (a *Accounts) Wrapping(wrap func(loop.Model, recall.Budget) (loop.Model, error), budget recall.Budget, opts RunOpts) {
	a.wrap, a.budget = wrap, budget
	a.budgetOf = func(model catalog.Model) (recall.Budget, error) { return ContextBudget(opts, model) }
}

func (a *Accounts) budgetFor(model catalog.Model) (recall.Budget, error) {
	if a.budgetOf == nil {
		return a.budget, nil
	}
	return a.budgetOf(model)
}

func (a *Accounts) on(model catalog.Model, budget recall.Budget) (*Accounts, error) {
	spec, err := cred.Lookup(string(model.Subscription))
	if err != nil {
		return nil, err
	}
	effort := a.effort
	if !slices.Contains(model.Efforts, effort) {
		effort = ""
	}
	return &Accounts{Dir: a.Dir, Provider: cred.Provider(model.Subscription), Spec: spec, Store: a.Store, ModelID: model.ID, spends: model.Windows, effort: effort,
		URLs: a.URLs, Now: a.Now, wrap: a.wrap, budget: budget, budgetOf: a.budgetOf, verbosity: model.Verbosity, reasoningContext: model.ReasoningContext}, nil
}

func (a *Accounts) continuing() Continuing {
	var signedIn []string
	if rows, err := a.Store.List(); err == nil {
		for _, row := range rows {
			if row.Unusable(a.Now()) == "" {
				signedIn = append(signedIn, string(row.Credential.Provider))
			}
		}
	}
	return Continuing{dir: a.Dir, from: string(a.Provider), signedIn: signedIn, budgetOf: a.budgetFor,
		serving: func(ctx context.Context, model catalog.Model) bool {
			other, err := a.on(model, a.budget)
			if err != nil {
				return false
			}
			candidates, _, err := other.read(ctx)
			return err == nil && slices.ContainsFunc(candidates, func(candidate quota.Candidate) bool { return !quota.Spent(candidate.Report, model.Windows, a.Now()) })
		},
		open: func(ctx context.Context, model catalog.Model, budget recall.Budget) (loop.Account, loop.Accounts, error) {
			if model.Pays() == catalog.PaysKey {
				account, err := a.keyed(model, budget)
				return account, loop.Accounts{}, err
			}
			other, err := a.on(model, budget)
			if err != nil {
				return loop.Account{}, loop.Accounts{}, err
			}
			account, err := other.Pick(ctx)
			return account, other.ForTurn(), err
		},
	}
}

func (a *Accounts) keyed(model catalog.Model, budget recall.Budget) (loop.Account, error) {
	var opened loop.Model
	var err error
	switch model.Provider {
	case catalog.OpenRouter:
		opened, err = KeyModel(model.ID)
	case catalog.Meta:
		opened, err = MetaModel(model, model.EffortTaken(a.effort))
	default:
		err = fmt.Errorf("tofu has no wire that runs %s on a key", model.Slug())
	}
	if err != nil {
		return loop.Account{}, err
	}
	keyed := &Accounts{wrap: a.wrap, budget: budget}
	wrapped, err := keyed.wrapped(opened)
	return loop.Account{Model: wrapped}, err
}

func (s StandIns) continuing() Continuing {
	signedIn := make([]string, len(s.Readings))
	for i, reading := range s.Readings {
		signedIn[i] = reading.Source()
	}
	return Continuing{dir: s.Held.Dir, from: s.Source, signedIn: signedIn, budgetOf: s.Held.budgetFor,
		serving: func(_ context.Context, model catalog.Model) bool {
			return slices.ContainsFunc(s.Readings, func(reading frame.Quota) bool { return reading.Source() == model.Source() && reading.Fraction < 1 })
		},
		open: func(ctx context.Context, model catalog.Model, budget recall.Budget) (loop.Account, loop.Accounts, error) {
			held := &Accounts{Dir: s.Held.Dir, Fixed: s.Held.Fixed, Now: s.Held.Now, wrap: s.Held.wrap, budget: budget, budgetOf: s.Held.budgetOf}
			moved := StandIns{Held: held, Source: model.Source(), Readings: s.Readings}
			if model.Pays() == catalog.PaysKey {
				account, err := held.Pick(ctx)
				return account, loop.Accounts{}, err
			}
			account, err := moved.account(ctx, moved.Mine()[0])
			return account, moved.ForTurn(), err
		},
	}
}
