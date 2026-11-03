package cred

import (
	"context"
	"fmt"
	"time"

	"tofu/internal/konst"
)

const (
	sweepEvery   = konst.CredSweepSeconds * time.Second
	refreshAhead = konst.CredRefreshAheadMinutes * time.Minute
	refreshAfter = konst.CredRefreshAfterDays * 24 * time.Hour
)

type Keeper struct {
	store    *Store
	note     func(string)
	managers map[int64]*Manager
}

func NewKeeper(store *Store, note func(string)) *Keeper {
	return &Keeper{store: store, note: note, managers: make(map[int64]*Manager)}
}

func (k *Keeper) Run(ctx context.Context) {
	ticker := time.NewTicker(sweepEvery)
	defer ticker.Stop()
	for {
		k.Sweep(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (k *Keeper) Sweep(ctx context.Context, now time.Time) {
	rows, err := k.store.List()
	if err != nil {
		k.note("the credential store is unreadable: " + err.Error())
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		why := dueBecause(row.Credential, now)
		if why == "" || row.Unusable(now) != "" {
			continue
		}
		name := fmt.Sprintf("%s #%d", row.Credential.Provider, row.ID)
		manager, err := k.manager(row)
		if err == nil {
			_, err = manager.refreshOnce(context.WithoutCancel(ctx), row, func(credential Credential) bool {
				return dueBecause(credential, now) != ""
			})
		}
		if err != nil {
			k.note(name + " was due, " + why + ", and did not refresh: " + err.Error())
			continue
		}
		k.note(name + " kept alive, " + why)
	}
}

func (k *Keeper) manager(row Row) (*Manager, error) {
	if manager, made := k.managers[row.ID]; made {
		return manager, nil
	}
	spec, err := Lookup(string(row.Credential.Provider))
	if err != nil {
		return nil, err
	}
	manager := NewAccountManager(k.store, spec, row.ID)
	manager.note = k.note
	k.managers[row.ID] = manager
	return manager, nil
}

func dueBecause(credential Credential, now time.Time) string {
	switch {
	case credential.Refreshed.IsZero():
		return "no refresh on record"
	case credential.Refreshed.Before(now.Add(-refreshAfter)):
		return fmt.Sprintf("last refreshed %d days ago", int(now.Sub(credential.Refreshed)/(24*time.Hour)))
	case credential.Expires.Before(now.Add(refreshAhead)):
		return "the access token expires " + credential.Expires.UTC().Format(time.RFC3339)
	}
	return ""
}
