package main

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

	"tofu/interface/tui/frame"
	"tofu/internal/host"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/recall"
	"tofu/internal/safe"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
)

const claudeBaseURLVariable = "TOFU_CLAUDE_BASE_URL"

type accounts struct {
	dir      string
	provider cred.Provider
	spec     cred.Spec
	store    *cred.Store
	modelID  string
	spends   []string
	effort   llm.Effort
	urls     map[quota.Provider]string
	now      func() time.Time
	wrap     func(turn.Model, recall.Budget) (turn.Model, error)
	budget   recall.Budget
	budgetOf func(models.Model) (recall.Budget, error)
	fixed    turn.Model

	verbosity        models.Verbosity
	reasoningContext models.ReasoningContext

	polling   sync.Once
	poller    *quota.Poller
	pollerErr error
}

func (a *accounts) forTurn() turn.Accounts {
	if a.fixed != nil {
		return turn.Accounts{Pick: a.pick, Next: a.next, Resets: a.resets}
	}
	moveOn := func(ctx context.Context, left []string, conversationTokens int) (turn.Moved, error) {
		return a.continuing().move(ctx, left, conversationTokens)
	}
	return turn.Accounts{Pick: a.pick, Next: a.next, Resets: a.resets, Elsewhere: moveOn}
}

func (a *accounts) resets(ctx context.Context, pinned turn.Account) (time.Time, bool) {
	if a.fixed != nil {
		return time.Time{}, false
	}
	candidates, _, err := a.read(ctx)
	if at := slices.IndexFunc(candidates, func(candidate quota.Candidate) bool { return candidate.ID == pinned.ID }); err == nil && at >= 0 {
		return candidates[at].Report.WaitUntil(a.now())
	}
	return time.Time{}, false
}

type standIns struct {
	held     *accounts
	source   string
	readings []frame.Quota
}

func readingSource(reading frame.Quota) string { return reading.Source() }

func (s standIns) mine() []frame.Quota {
	return slices.DeleteFunc(slices.Clone(s.readings), func(reading frame.Quota) bool { return reading.Source() != s.source })
}

func (s standIns) account(ctx context.Context, reading frame.Quota) (turn.Account, error) {
	account, err := s.held.pick(ctx)
	_, window, _ := strings.Cut(reading.Label, " ")
	account.ID, account.Headroom, account.Window = reading.AccountID(), 1-reading.Fraction, window
	return account, err
}

func (s standIns) forTurn() turn.Accounts {
	mine := s.mine()
	at := func(pinned turn.Account) int {
		return slices.IndexFunc(mine, func(reading frame.Quota) bool { return reading.AccountID() == pinned.ID })
	}
	return turn.Accounts{
		Pick: func(ctx context.Context) (turn.Account, error) { return s.account(ctx, mine[0]) },
		Next: func(ctx context.Context, pinned turn.Account) (turn.Account, bool, error) {
			pinnedAt := at(pinned)
			if pinnedAt < 0 || mine[pinnedAt].Fraction < 1 || len(mine) == 1 {
				return turn.Account{}, false, nil
			}
			next, err := s.account(ctx, mine[(pinnedAt+1)%len(mine)])
			return next, err == nil, err
		},
		Resets: func(_ context.Context, pinned turn.Account) (time.Time, bool) {
			if pinnedAt := at(pinned); pinnedAt >= 0 {
				return mine[pinnedAt].ResetsAt, !mine[pinnedAt].ResetsAt.IsZero()
			}
			return time.Time{}, false
		},
		Elsewhere: s.continuing().move,
	}
}

func (s standIns) spent(id int64) host.TurnAccount {
	return host.TurnAccount{Source: s.source, AccountID: id, Model: cassetteBuild}
}

func (a *accounts) close() {
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *accounts) wrapped(model turn.Model) (turn.Model, error) {
	if a.wrap == nil {
		return model, nil
	}
	return a.wrap(model, a.budget)
}

func (a *accounts) pick(ctx context.Context) (turn.Account, error) {
	if a.fixed != nil {
		model, err := a.wrapped(a.fixed)
		return turn.Account{Model: model}, err
	}
	candidates, rows, err := a.read(ctx)
	if err != nil {
		return turn.Account{}, err
	}
	choice, found := quota.Pick(candidates, quota.Provider(a.provider), a.spends, a.now())
	if !found {
		return turn.Account{}, fmt.Errorf("no %s subscription credential, run %s", a.provider, loginHint(string(a.provider)))
	}
	return a.account(choice, rows)
}

func (a *accounts) next(ctx context.Context, pinned turn.Account) (turn.Account, bool, error) {
	if a.fixed != nil || pinned.ID == 0 {
		return turn.Account{}, false, nil
	}
	candidates, rows, err := a.read(ctx)
	if err != nil {
		return turn.Account{}, false, err
	}
	serving := slices.ContainsFunc(candidates, func(candidate quota.Candidate) bool {
		return candidate.ID == pinned.ID && !quota.Spent(candidate.Report, a.spends, a.now())
	})
	choice, found := quota.Pick(candidates, quota.Provider(a.provider), a.spends, a.now())
	if serving || !found || choice.ID == pinned.ID {
		return turn.Account{}, false, nil
	}
	moved, err := a.account(choice, rows)
	return moved, err == nil, err
}

func (a *accounts) read(ctx context.Context) ([]quota.Candidate, map[int64]cred.Row, error) {
	stored, err := a.store.List()
	if err != nil {
		return nil, nil, err
	}
	now := a.now()
	var mine []cred.Row
	for _, row := range stored {
		if row.Credential.Provider == a.provider && row.Unusable(now) == "" {
			mine = append(mine, row)
		}
	}
	poller, err := a.quota()
	if err != nil {
		return nil, nil, err
	}
	results, err := pollRowsOn(ctx, a.store, mine, a.now, a.urls, poller)
	if err != nil {
		return nil, nil, err
	}
	candidates := make([]quota.Candidate, 0, len(mine))
	rows := make(map[int64]cred.Row, len(mine))
	for i, row := range mine {
		rows[row.ID] = row
		candidates = append(candidates, quota.Candidate{
			ID:       row.ID,
			Provider: quota.Provider(a.provider),
			Report:   results[i].report,
			LastUsed: row.LastUsed,
		})
	}
	return candidates, rows, nil
}

func (a *accounts) quota() (*quota.Poller, error) {
	a.polling.Do(func() { a.poller, a.pollerErr = quota.NewPoller(nil, a.now, a.urls, recordQuotaReading) })
	return a.poller, a.pollerErr
}

func (a *accounts) account(choice quota.Choice, rows map[int64]cred.Row) (turn.Account, error) {
	row := rows[choice.ID]
	model, err := a.modelOn(row)
	if err != nil {
		return turn.Account{}, err
	}
	wrapped, err := a.wrapped(model)
	if err != nil {
		return turn.Account{}, err
	}
	_ = a.store.MarkUsed(choice.ID, a.now())
	return turn.Account{
		ID:       choice.ID,
		Model:    wrapped,
		Headroom: choice.Headroom.Fraction,
		Window:   choice.Headroom.Window,
	}, nil
}

func (a *accounts) modelOn(row cred.Row) (turn.Model, error) {
	token := cred.NewAccountManager(a.store, a.spec, row.ID).Token
	session, err := sessionID()
	if err != nil {
		return nil, err
	}
	versions, adopt := subFingerprint(a.dir)
	poller, err := a.quota()
	if err != nil {
		return nil, err
	}
	heard := quota.Account{Provider: quota.Provider(a.provider), AccountID: row.Credential.Identity.AccountID, Row: row.ID}
	named := transport.Account{Source: string(a.provider), Label: strings.TrimSpace("#" + strconv.FormatInt(row.ID, 10) + " " + row.Credential.Identity.Email), SignIn: loginHint(string(a.provider))}
	onHeaders := func(header http.Header) { _ = poller.Heard(heard, header) }
	if a.provider == cred.CodexSub {
		wire, err := codex.New(codex.Config{
			Model:          a.modelID,
			Token:          token,
			InstallationID: session,
			SessionID:      session,
			Transport:      turnTransportConfig(),
			Account:        named,
			ClientVersion:  versions.Codex,
			OnHeaders:      onHeaders,
		})
		if err != nil {
			return nil, err
		}
		return codexTurn{wire: wire, effort: a.effort, verbosity: a.verbosity, reasoningContext: a.reasoningContext}, nil
	}
	stand := os.Getenv(claudeBaseURLVariable)
	wire, err := anthropic.New(anthropic.Config{
		BaseURL:   stand,
		Proxy:     stand != "",
		Model:     a.modelID,
		Token:     token,
		SessionID: session,
		AccountID: row.Credential.Identity.AccountID,
		Transport: turnTransportConfig(),
		Account:   named,

		ClaudeCodeVersion: versions.ClaudeCode,
		AdoptVersion:      adopt,
		OnHeaders:         onHeaders,
	})
	if err != nil {
		return nil, err
	}
	return turn.Subscription{Wire: wire, Effort: a.effort, CacheTTL: settingText(a.dir, settingspkg.LeadCache, nil)}, nil
}

func keepAccountsAlive(held heldStore, note, told func(string)) (stop func()) {
	if held.err != nil {
		note("signed-in accounts are not kept alive: " + held.err.Error())
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	report := crashTold(".", told)
	safe.Go(report, "credential keeper", func() {
		defer close(done)
		safe.Loop(report, "credential keeper", ctx.Done(), func() { cred.NewKeeper(held.store, note).Run(ctx) })
	})
	return func() {
		cancel()
		<-done
	}
}

func clientVersion(versions settingspkg.Fingerprint, provider cred.Provider) string {
	if provider == cred.CodexSub {
		return versions.Codex
	}
	return versions.ClaudeCode
}

func subFingerprint(dir string) (settingspkg.Fingerprint, func(version string) error) {
	store, err := openSettings(dir)
	if err != nil {
		return settingspkg.PinnedFingerprint(), nil
	}
	return store.Fingerprint(), func(version string) error {
		return store.Save(settingspkg.Global, settingspkg.Write{Kind: settingspkg.WriteFingerprint, Key: settingspkg.FingerprintClaudeCode, Value: version})
	}
}

func metaModel(selected models.Model, effort llm.Effort) (turn.Model, error) {
	key, err := jev.KeyFor(sys.CredentialFileName, sys.MetaMuseKeyName)
	if err != nil {
		return nil, fmt.Errorf("%w: run %s", err, loginHint(metaName))
	}
	registry, _ := modelRegistry()
	price := registry.Prices[selected.Slug()]
	wire, err := codex.New(codex.Config{
		BaseURL:   models.MetaBaseURL() + codex.KeyPath,
		Model:     selected.ID,
		Token:     func(context.Context, string) (string, error) { return key, nil },
		Transport: turnTransportConfig(),
		OnCall: func(result codex.Result, header http.Header, failed error) {
			tallyMetaCall(selected.ID, price, result, header, failed)
		},
	})
	return codexTurn{wire: wire, effort: effort, verbosity: selected.Verbosity, reasoningContext: selected.ReasoningContext}, err
}

func listUSD(price models.Price, input, cached, output int) float64 {
	rates := price.Rates
	for _, tier := range price.Tiers {
		if input > tier.AboveTokens {
			rates = tier.Rates
		}
	}
	return (float64(input-cached)*rates.Input + float64(cached)*rates.CacheRead + float64(output)*rates.Output) / tokensPerMillion
}

func tallyMetaCall(model string, price models.Price, result codex.Result, header http.Header, failed error) {
	usage := result.Usage
	use := ledger.KeyUse{Key: sys.MetaMuseKeyName, Provider: metaName, Model: cmp.Or(result.Model, model),
		Usage:      ledger.Usage{InputTokens: usage.Input, OutputTokens: usage.Output, CacheReadInputTokens: usage.CacheRead, ReasoningOutputTokens: usage.Reasoning},
		Charge:     ledger.ChargeOf(0, listUSD(price, usage.Input, usage.CacheRead, usage.Output)),
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

func openAccounts(opts runOpts, selected models.Model) (*accounts, turn.Spend, error) {
	modelID := selected.ID
	spend := wireSpend(opts.wire)
	if opts.wire == wireKey {
		model, err := keyModel(modelID)
		return &accounts{modelID: modelID, now: time.Now, fixed: model}, spend, err
	}
	if opts.wire == wireMeta {
		model, err := metaModel(selected, opts.effort)
		return &accounts{modelID: modelID, now: time.Now, fixed: model}, spend, err
	}
	provider := cred.Provider(selected.Subscription)
	spec, err := cred.Lookup(string(provider))
	if err != nil {
		return nil, spend, err
	}
	path, err := cred.Path()
	if err != nil {
		return nil, spend, err
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil, spend, err
	}
	return &accounts{dir: opts.dir, provider: provider, spec: spec, store: store, modelID: modelID, spends: selected.Windows, effort: opts.effort, now: time.Now,
		verbosity: selected.Verbosity, reasoningContext: selected.ReasoningContext}, spend, nil
}
