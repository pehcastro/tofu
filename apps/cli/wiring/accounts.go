package wiring

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	llm "tofu/internal/models/llm/core"
	"tofu/internal/models/llm/quota"
	"tofu/internal/models/llm/wire/anthropic"
	"tofu/internal/models/llm/wire/codex"
	"tofu/internal/models/transport"
	"tofu/internal/platform/safe"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/ui/tui/frame/frame"
)

const (
	CassetteBuild         = "cassette"
	claudeBaseURLVariable = "TOFU_CLAUDE_BASE_URL"
	keeperLockSuffix      = ".keeper.lock"
)

type Accounts struct {
	Dir      string
	Provider accounts.Provider
	Spec     accounts.Spec
	Store    *accounts.Store
	ModelID  string
	URLs     map[quota.Provider]string
	Now      func() time.Time
	Fixed    loop.Model

	spends   []string
	effort   llm.Effort
	wrap     func(loop.Model, recall.Budget) (loop.Model, error)
	budget   recall.Budget
	budgetOf func(catalog.Model) (recall.Budget, error)

	verbosity        catalog.Verbosity
	reasoningContext catalog.ReasoningContext

	polling   sync.Once
	poller    *quota.Poller
	pollerErr error
}

func FixedAccounts(model loop.Model) *Accounts {
	return &Accounts{Now: time.Now, Fixed: model}
}

func (a *Accounts) ForTurn() loop.Accounts {
	if a.Fixed != nil {
		return loop.Accounts{Pick: a.Pick, Next: a.Next, Resets: a.resets}
	}
	moveOn := func(ctx context.Context, left []string, conversationTokens int) (loop.Moved, error) {
		return a.continuing().move(ctx, left, conversationTokens)
	}
	return loop.Accounts{Pick: a.Pick, Next: a.Next, Resets: a.resets, Elsewhere: moveOn, Keep: a.keep}
}

func (a *Accounts) keep(ctx context.Context, held loop.Held) (loop.Account, string, error) {
	candidates, rows, err := a.read(ctx)
	if err != nil {
		return loop.Account{}, "", err
	}
	return keeping(candidates, held, a.Dir, a.spends, a.Now(), func(choice quota.Choice) (loop.Account, error) { return a.account(choice, rows) })
}

func keeping(candidates []quota.Candidate, held loop.Held, dir string, spends []string, now time.Time, on func(quota.Choice) (loop.Account, error)) (loop.Account, string, error) {
	ttl := settingspkg.TextSaying(cmp.Or(dir, "."), settingspkg.LeadCache, nil)
	if ttl == konst.SubscriptionCacheHistory5m {
		ttl = konst.SubscriptionCacheTTL
	}
	lifetime, _ := time.ParseDuration(ttl)
	at := slices.IndexFunc(candidates, func(candidate quota.Candidate) bool { return candidate.ID == held.ID })
	switch {
	case held.Idle >= lifetime:
		return loop.Account{}, fmt.Sprintf("account #%d last answered %s ago, past its %s cache, so this turn takes the account with the most headroom", held.ID, held.Idle.Round(time.Minute), ttl), nil
	case at < 0:
		return loop.Account{}, fmt.Sprintf("account #%d is no longer signed in, so this turn moves to another account and writes its cache there once", held.ID), nil
	case quota.Spent(candidates[at].Report, spends, now):
		return loop.Account{}, fmt.Sprintf("account #%d is at its limit, so this turn moves to the account with the most headroom and writes its cache there once", held.ID), nil
	}
	kept, err := on(quota.Choice{ID: held.ID, Headroom: quota.Left(candidates[at].Report, spends, now)})
	return kept, "", err
}

func (a *Accounts) resets(ctx context.Context, pinned loop.Account) (time.Time, bool) {
	if a.Fixed != nil {
		return time.Time{}, false
	}
	candidates, _, err := a.read(ctx)
	if at := slices.IndexFunc(candidates, func(candidate quota.Candidate) bool { return candidate.ID == pinned.ID }); err == nil && at >= 0 {
		return candidates[at].Report.WaitUntil(a.Now())
	}
	return time.Time{}, false
}

type StandIns struct {
	Held     *Accounts
	Source   string
	Readings []frame.Quota
}

func ReadingSource(reading frame.Quota) string { return reading.Source() }

func (s StandIns) Mine() []frame.Quota {
	return slices.DeleteFunc(slices.Clone(s.Readings), func(reading frame.Quota) bool { return reading.Source() != s.Source })
}

func (s StandIns) account(ctx context.Context, reading frame.Quota) (loop.Account, error) {
	_, window, _ := strings.Cut(reading.Label, " ")
	return s.on(ctx, quota.Choice{ID: reading.AccountID(), Headroom: quota.Headroom{Fraction: 1 - reading.Fraction, Window: window}})
}

func (s StandIns) on(ctx context.Context, choice quota.Choice) (loop.Account, error) {
	account, err := s.Held.Pick(ctx)
	account.ID, account.Headroom, account.Window = choice.ID, choice.Headroom.Fraction, choice.Headroom.Window
	return account, err
}

func (s StandIns) candidates() []quota.Candidate {
	var candidates []quota.Candidate
	for _, reading := range s.Mine() {
		at := slices.IndexFunc(candidates, func(candidate quota.Candidate) bool { return candidate.ID == reading.AccountID() })
		if at < 0 {
			candidates, at = append(candidates, quota.Candidate{ID: reading.AccountID(), Provider: quota.Provider(s.Source)}), len(candidates)
		}
		_, window, _ := strings.Cut(reading.Label, " ")
		candidates[at].Report.Windows = append(candidates[at].Report.Windows, quota.Window{ID: window, Used: quota.Used{Fraction: reading.Fraction, Reported: reading.Reported}, ResetsAt: reading.ResetsAt})
	}
	return candidates
}

func (s StandIns) ForTurn() loop.Accounts {
	return loop.Accounts{
		Pick: func(ctx context.Context) (loop.Account, error) {
			choice, _ := quota.Pick(s.candidates(), quota.Provider(s.Source), s.Held.spends, s.Held.Now())
			return s.on(ctx, choice)
		},
		Keep: func(ctx context.Context, held loop.Held) (loop.Account, string, error) {
			return keeping(s.candidates(), held, s.Held.Dir, s.Held.spends, s.Held.Now(), func(choice quota.Choice) (loop.Account, error) { return s.on(ctx, choice) })
		},
		Next: func(ctx context.Context, pinned loop.Account) (loop.Account, bool, error) {
			candidates, now := s.candidates(), s.Held.Now()
			pinnedAt := slices.IndexFunc(candidates, func(candidate quota.Candidate) bool { return candidate.ID == pinned.ID })
			if pinnedAt < 0 || !quota.Spent(candidates[pinnedAt].Report, s.Held.spends, now) || len(candidates) == 1 {
				return loop.Account{}, false, nil
			}
			if err := everySpent(s.Source, candidates, s.Held.spends, now); err != nil {
				return loop.Account{}, false, err
			}
			choice, _ := quota.Pick(candidates, quota.Provider(s.Source), s.Held.spends, now)
			next, err := s.on(ctx, choice)
			return next, err == nil, err
		},
		Resets: func(_ context.Context, pinned loop.Account) (time.Time, bool) {
			mine := s.Mine()
			if pinnedAt := slices.IndexFunc(mine, func(reading frame.Quota) bool { return reading.AccountID() == pinned.ID }); pinnedAt >= 0 {
				return mine[pinnedAt].ResetsAt, !mine[pinnedAt].ResetsAt.IsZero()
			}
			return time.Time{}, false
		},
		Elsewhere: s.continuing().move,
	}
}

func (s StandIns) Spent(id int64) host.TurnAccount {
	return host.TurnAccount{Source: s.Source, AccountID: id, Model: CassetteBuild}
}

func (a *Accounts) Close() {
	if a.Store != nil {
		_ = a.Store.Close()
	}
}

func (a *Accounts) wrapped(model loop.Model) (loop.Model, error) {
	if a.wrap == nil {
		return model, nil
	}
	return a.wrap(model, a.budget)
}

func (a *Accounts) Pick(ctx context.Context) (loop.Account, error) {
	if a.Fixed != nil {
		model, err := a.wrapped(a.Fixed)
		return loop.Account{Model: model}, err
	}
	candidates, rows, err := a.read(ctx)
	if err != nil {
		return loop.Account{}, err
	}
	choice, found := quota.Pick(candidates, quota.Provider(a.Provider), a.spends, a.Now())
	if !found {
		return loop.Account{}, fmt.Errorf("no %s subscription credential, run %s", a.Provider, LoginHint(string(a.Provider)))
	}
	return a.account(choice, rows)
}

func (a *Accounts) Next(ctx context.Context, pinned loop.Account) (loop.Account, bool, error) {
	if a.Fixed != nil || pinned.ID == 0 {
		return loop.Account{}, false, nil
	}
	candidates, rows, err := a.read(ctx)
	if err != nil {
		return loop.Account{}, false, err
	}
	serving := slices.ContainsFunc(candidates, func(candidate quota.Candidate) bool {
		return candidate.ID == pinned.ID && !quota.Spent(candidate.Report, a.spends, a.Now())
	})
	if serving || len(candidates) < 2 {
		return loop.Account{}, false, nil
	}
	if err := everySpent(string(a.Provider), candidates, a.spends, a.Now()); err != nil {
		return loop.Account{}, false, err
	}
	choice, found := quota.Pick(candidates, quota.Provider(a.Provider), a.spends, a.Now())
	if !found || choice.ID == pinned.ID {
		return loop.Account{}, false, nil
	}
	moved, err := a.account(choice, rows)
	return moved, err == nil, err
}

func everySpent(source string, candidates []quota.Candidate, spends []string, now time.Time) error {
	spent := &loop.EveryAccountSpent{Source: source}
	for _, candidate := range candidates {
		if !quota.Spent(candidate.Report, spends, now) {
			return nil
		}
		spent.Spent = append(spent.Spent, candidate.ID)
		if at, known := candidate.Report.WaitUntil(now); known && (spent.ResetAt.IsZero() || at.Before(spent.ResetAt)) {
			spent.ResetAt = at
		}
	}
	return spent
}

func (a *Accounts) read(ctx context.Context) ([]quota.Candidate, map[int64]accounts.Row, error) {
	stored, err := a.Store.List()
	if err != nil {
		return nil, nil, err
	}
	now := a.Now()
	var mine []accounts.Row
	for _, row := range stored {
		if row.Credential.Provider == a.Provider && row.Unusable(now) == "" {
			mine = append(mine, row)
		}
	}
	poller, err := a.Quota()
	if err != nil {
		return nil, nil, err
	}
	results, err := PollRowsOn(ctx, a.Store, mine, a.Now, a.URLs, poller)
	if err != nil {
		return nil, nil, err
	}
	candidates := make([]quota.Candidate, 0, len(mine))
	rows := make(map[int64]accounts.Row, len(mine))
	for i, row := range mine {
		rows[row.ID] = row
		candidates = append(candidates, quota.Candidate{
			ID:       row.ID,
			Provider: quota.Provider(a.Provider),
			Report:   results[i].Report,
			LastUsed: row.LastUsed,
		})
	}
	return candidates, rows, nil
}

func (a *Accounts) Quota() (*quota.Poller, error) {
	a.polling.Do(func() { a.poller, a.pollerErr = quota.NewPoller(nil, a.Now, a.URLs, RecordQuotaReading) })
	return a.poller, a.pollerErr
}

func (a *Accounts) account(choice quota.Choice, rows map[int64]accounts.Row) (loop.Account, error) {
	row := rows[choice.ID]
	model, err := a.modelOn(row)
	if err != nil {
		return loop.Account{}, err
	}
	wrapped, err := a.wrapped(model)
	if err != nil {
		return loop.Account{}, err
	}
	_ = a.Store.MarkUsed(choice.ID, a.Now())
	return loop.Account{
		ID:       choice.ID,
		Model:    wrapped,
		Headroom: choice.Headroom.Fraction,
		Window:   choice.Headroom.Window,
	}, nil
}

func (a *Accounts) modelOn(row accounts.Row) (loop.Model, error) {
	token := accounts.NewAccountManager(a.Store, a.Spec, row.ID).Token
	session, err := SessionID()
	if err != nil {
		return nil, err
	}
	versions, adopt := SubFingerprint(a.Dir)
	poller, err := a.Quota()
	if err != nil {
		return nil, err
	}
	heard := quota.Account{Provider: quota.Provider(a.Provider), AccountID: row.Credential.Identity.AccountID, Row: row.ID}
	named := transport.Account{Source: string(a.Provider), Label: strings.TrimSpace("#" + strconv.FormatInt(row.ID, 10) + " " + row.Credential.Identity.Email), SignIn: LoginHint(string(a.Provider))}
	onHeaders := func(header http.Header) { _ = poller.Heard(heard, header) }
	if a.Provider == accounts.CodexSub {
		wire, err := codex.New(codex.Config{
			Model:          a.ModelID,
			Token:          token,
			InstallationID: session,
			SessionID:      session,
			Transport:      TurnTransport(),
			Account:        named,
			ClientVersion:  versions.Codex,
			OnHeaders:      onHeaders,
		})
		if err != nil {
			return nil, err
		}
		return CodexTurn{Wire: wire, Effort: a.effort, Verbosity: a.verbosity, ReasoningContext: a.reasoningContext}, nil
	}
	stand := os.Getenv(claudeBaseURLVariable)
	wire, err := anthropic.New(anthropic.Config{
		BaseURL:   stand,
		Proxy:     stand != "",
		Model:     a.ModelID,
		Token:     token,
		SessionID: session,
		AccountID: row.Credential.Identity.AccountID,
		Transport: TurnTransport(),
		Account:   named,

		ClaudeCodeVersion: versions.ClaudeCode,
		AdoptVersion:      adopt,
		OnHeaders:         onHeaders,
	})
	if err != nil {
		return nil, err
	}
	return loop.Subscription{Wire: wire, Effort: a.effort, CacheTTL: settingspkg.TextSaying(a.Dir, settingspkg.LeadCache, nil)}, nil
}

func KeepAccountsAlive(held HeldStore, note, told func(string)) (stop func()) {
	if held.Err != nil {
		note("signed-in accounts are not kept alive: " + held.Err.Error())
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	report := CrashTold(".", told)
	safe.Go(report, "credential keeper", func() {
		defer close(done)
		lock := claimKeeper(ctx, held.Path+keeperLockSuffix, note)
		if lock == nil {
			return
		}
		defer func() { _ = sys.Unlock(lock) }()
		safe.Loop(report, "credential keeper", ctx.Done(), func() { accounts.NewKeeper(held.Store, note).Run(ctx) })
	})
	return func() {
		cancel()
		<-done
	}
}

func claimKeeper(ctx context.Context, path string, note func(string)) *os.File {
	told := false
	for {
		lock, held, err := sys.TryLock(path)
		switch {
		case err != nil:
			note("signed-in accounts are not kept alive: the keeper lock " + path + " does not open: " + err.Error())
			return nil
		case !held:
			_, _ = lock.WriteString(strconv.Itoa(os.Getpid()))
			note("process " + strconv.Itoa(os.Getpid()) + " keeps the signed-in accounts alive for this machine")
			return lock
		case !told:
			holder, _ := os.ReadFile(path)
			note("the credential keeper is held by process " + string(holder) + ", so this host takes it over when that one closes")
			told = true
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(konst.CredSweepSeconds * time.Second):
		}
	}
}

func ClientVersion(versions settingspkg.Fingerprint, provider accounts.Provider) string {
	if provider == accounts.CodexSub {
		return versions.Codex
	}
	return versions.ClaudeCode
}

func SubFingerprint(dir string) (settingspkg.Fingerprint, func(version string) error) {
	store, err := settingspkg.OpenFor(dir)
	if err != nil {
		return settingspkg.PinnedFingerprint(), nil
	}
	return store.Fingerprint(), func(version string) error {
		return store.Save(settingspkg.Global, settingspkg.Write{Kind: settingspkg.WriteFingerprint, Key: settingspkg.FingerprintClaudeCode, Value: version})
	}
}

func MetaModel(selected catalog.Model, effort llm.Effort) (loop.Model, error) {
	key, err := core.KeyFor(sys.CredentialFileName, sys.MetaMuseKeyName)
	if err != nil {
		return nil, fmt.Errorf("%w: run %s", err, LoginHint(MetaName))
	}
	registry, _ := ModelRegistry()
	price := registry.Prices[selected.Slug()]
	wire, err := codex.New(codex.Config{
		BaseURL:   catalog.MetaBaseURL() + codex.KeyPath,
		Model:     selected.ID,
		Token:     func(context.Context, string) (string, error) { return key, nil },
		Transport: TurnTransport(),
		OnCall: func(result codex.Result, header http.Header, failed error) {
			tallyMetaCall(selected.ID, price, result, header, failed)
		},
	})
	return CodexTurn{Wire: wire, Effort: effort, Verbosity: selected.Verbosity, ReasoningContext: selected.ReasoningContext}, err
}

func ListUSD(price catalog.Price, input, cached, output int) float64 {
	rates := price.Rates
	for _, tier := range price.Tiers {
		if input > tier.AboveTokens {
			rates = tier.Rates
		}
	}
	return (float64(input-cached)*rates.Input + float64(cached)*rates.CacheRead + float64(output)*rates.Output) / TokensPerMillion
}

func tallyMetaCall(model string, price catalog.Price, result codex.Result, header http.Header, failed error) {
	usage := result.Usage
	use := ledger.KeyUse{Key: sys.MetaMuseKeyName, Provider: MetaName, Model: cmp.Or(result.Model, model),
		Usage:      ledger.Usage{InputTokens: usage.Input, OutputTokens: usage.Output, CacheReadInputTokens: usage.CacheRead, ReasoningOutputTokens: usage.Reasoning},
		Charge:     ledger.ChargeOf(0, ListUSD(price, usage.Input, usage.CacheRead, usage.Output)),
		Allowances: ledger.PerUnitAllowance(header, ledger.ScopeTeam)}
	var refused *transport.Error
	switch {
	case errors.As(failed, &refused):
		use.Failed = &ledger.KeyError{Status: refused.Status, What: cmp.Or(refused.Said, refused.Code, refused.Kind.String())}
	case failed != nil:
		use.Failed = &ledger.KeyError{What: failed.Error()}
	}
	_ = ledger.Tally(use)
}

func OpenAccounts(opts RunOpts, selected catalog.Model) (*Accounts, loop.Spend, error) {
	modelID := selected.ID
	spend := WireSpend(opts.Wire)
	if opts.Wire == WireKey {
		model, err := KeyModel(modelID)
		return &Accounts{ModelID: modelID, Now: time.Now, Fixed: model}, spend, err
	}
	if opts.Wire == WireMeta {
		model, err := MetaModel(selected, opts.Effort)
		return &Accounts{ModelID: modelID, Now: time.Now, Fixed: model}, spend, err
	}
	provider := accounts.Provider(selected.Subscription)
	spec, err := accounts.Lookup(string(provider))
	if err != nil {
		return nil, spend, err
	}
	path, err := accounts.Path()
	if err != nil {
		return nil, spend, err
	}
	store, err := accounts.Open(path)
	if err != nil {
		return nil, spend, err
	}
	return &Accounts{Dir: opts.Dir, Provider: provider, Spec: spec, Store: store, ModelID: modelID, spends: selected.Windows, effort: opts.Effort, Now: time.Now,
		verbosity: selected.Verbosity, reasoningContext: selected.ReasoningContext}, spend, nil
}
