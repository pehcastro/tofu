package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

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
	wrap     func(turn.Model) (turn.Model, error)
	fixed    turn.Model

	polling   sync.Once
	poller    *quota.Poller
	pollerErr error
}

func (a *accounts) forTurn() turn.Accounts {
	return turn.Accounts{Pick: a.pick, Next: a.next}
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
	return a.wrap(model)
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
	onHeaders := func(header http.Header) { _ = poller.Heard(heard, header) }
	if a.provider == cred.CodexSub {
		wire, err := codex.New(codex.Config{
			Model:          a.modelID,
			Token:          token,
			InstallationID: session,
			SessionID:      session,
			Transport:      turnTransportConfig(),
			ClientVersion:  versions.Codex,
			OnHeaders:      onHeaders,
		})
		if err != nil {
			return nil, err
		}
		return codexTurn{wire: wire, effort: a.effort}, nil
	}
	wire, err := anthropic.New(anthropic.Config{
		Model:     a.modelID,
		Token:     token,
		SessionID: session,
		AccountID: row.Credential.Identity.AccountID,
		Transport: turnTransportConfig(),

		ClaudeCodeVersion: versions.ClaudeCode,
		AdoptVersion:      adopt,
		OnHeaders:         onHeaders,
	})
	if err != nil {
		return nil, err
	}
	return turn.Subscription{Wire: wire, Effort: a.effort}, nil
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

func metaModel(modelID string, effort llm.Effort) (turn.Model, error) {
	key, err := jev.KeyFor(sys.CredentialFileName, sys.MetaMuseKeyName)
	if err != nil {
		return nil, fmt.Errorf("%w: run %s", err, loginHint(metaName))
	}
	wire, err := codex.New(codex.Config{
		BaseURL:   models.MetaBaseURL() + codex.KeyPath,
		Model:     modelID,
		Token:     func(context.Context, string) (string, error) { return key, nil },
		Transport: turnTransportConfig(),
	})
	return codexTurn{wire: wire, effort: effort}, err
}

func openAccounts(opts runOpts, selected models.Model) (*accounts, turn.Spend, error) {
	modelID := selected.ID
	spend := wireSpend(opts.wire)
	if opts.wire == wireKey {
		model, err := keyModel(modelID)
		return &accounts{modelID: modelID, now: time.Now, fixed: model}, spend, err
	}
	if opts.wire == wireMeta {
		model, err := metaModel(modelID, opts.effort)
		return &accounts{modelID: modelID, now: time.Now, fixed: model}, spend, err
	}
	provider := cred.ClaudeSub
	if opts.wire == wireCodex {
		provider = cred.CodexSub
	}
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
	return &accounts{dir: opts.dir, provider: provider, spec: spec, store: store, modelID: modelID, spends: selected.Windows, effort: opts.effort, now: time.Now}, spend, nil
}
