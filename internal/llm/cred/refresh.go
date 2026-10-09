package cred

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"tofu/internal/konst"
)

const (
	refreshSkew          = 60 * time.Second
	leaseTTL             = konst.CredLeaseTTLMillis * time.Millisecond
	leasePollMin         = 50 * time.Millisecond
	leasePollMax         = 250 * time.Millisecond
	refreshTimeout       = konst.CredRefreshTimeoutMillis * time.Millisecond
	leaseOutlivesRefresh = uint(leaseTTL - refreshTimeout - 1)
	writeRetry           = konst.CredWriteRetryMillis * time.Millisecond
	ownerBytes           = 16
)

type flight struct {
	done   chan struct{}
	access string
	err    error
}

type unwritten struct {
	credential Credential
	replaces   string
}

type Manager struct {
	store    *Store
	spec     Spec
	find     func(time.Time) (Row, bool, error)
	client   *http.Client
	now      func() time.Time
	save     func(int64, Credential, string, time.Time) (bool, error)
	note     func(string)
	mu       sync.Mutex
	inflight map[int64]*flight
	held     map[int64]unwritten
}

func NewManager(store *Store, spec Spec) *Manager {
	return newManager(store, spec, func(now time.Time) (Row, bool, error) {
		return store.RowAt(spec.Provider, now)
	})
}

func NewAccountManager(store *Store, spec Spec, id int64) *Manager {
	return newManager(store, spec, func(time.Time) (Row, bool, error) {
		return store.RowByID(id)
	})
}

func newManager(store *Store, spec Spec, find func(time.Time) (Row, bool, error)) *Manager {
	return &Manager{
		store:    store,
		spec:     spec,
		find:     find,
		client:   &http.Client{},
		now:      time.Now,
		save:     store.UpdateIfRefreshMatches,
		note:     func(string) {},
		inflight: make(map[int64]*flight),
		held:     make(map[int64]unwritten),
	}
}

func (m *Manager) Access(ctx context.Context) (string, error) {
	return m.Token(ctx, "")
}

func (m *Manager) Token(ctx context.Context, rejected string) (string, error) {
	row, found, err := m.find(m.now())
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("cred: no %s credential, run tofu login llm %s", m.spec.Provider, m.spec.Provider)
	}
	if cause := row.Unusable(m.now()); cause != "" {
		return "", fmt.Errorf("cred: the %s credential is %s", m.spec.Provider, cause)
	}
	if current, _ := m.flush(row); m.usable(current, rejected) {
		return current.Access, nil
	}
	return m.refreshOnce(ctx, row, func(credential Credential) bool { return !m.usable(credential, rejected) })
}

func (m *Manager) usable(credential Credential, rejected string) bool {
	return m.fresh(credential) && credential.Access != rejected
}

func (m *Manager) fresh(credential Credential) bool {
	return m.now().Add(refreshSkew).Before(credential.Expires)
}

func (m *Manager) flush(row Row) (Credential, string) {
	m.mu.Lock()
	held, holding := m.held[row.ID]
	m.mu.Unlock()
	if !holding {
		return row.Credential, row.Credential.Refresh
	}
	replaced, err := m.save(row.ID, held.credential, held.replaces, m.now())
	if err != nil {
		return held.credential, held.replaces
	}
	m.mu.Lock()
	if m.held[row.ID].credential.Refresh == held.credential.Refresh {
		delete(m.held, row.ID)
	}
	m.mu.Unlock()
	if !replaced {
		return row.Credential, row.Credential.Refresh
	}
	return held.credential, held.credential.Refresh
}

func (m *Manager) refreshOnce(ctx context.Context, row Row, stale func(Credential) bool) (string, error) {
	m.mu.Lock()
	running, joined := m.inflight[row.ID]
	if !joined {
		running = &flight{done: make(chan struct{})}
		m.inflight[row.ID] = running
		go func() {
			detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
			defer cancel()
			running.access, running.err = m.refreshLeased(detached, row, stale)
			m.mu.Lock()
			delete(m.inflight, row.ID)
			m.mu.Unlock()
			close(running.done)
		}()
	}
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-running.done:
		return running.access, running.err
	}
}

func (m *Manager) refreshLeased(ctx context.Context, row Row, stale func(Credential) bool) (string, error) {
	owner, err := randomURLSafe(ownerBytes)
	if err != nil {
		return "", err
	}
	for {
		now := m.now()
		held, err := m.store.AcquireLease(row.ID, owner, now.Add(leaseTTL), now)
		if err != nil {
			return "", err
		}
		if held {
			break
		}
		if err := m.waitForLease(ctx, row.ID); err != nil {
			return "", err
		}
	}
	defer func() { _ = m.store.ReleaseLease(row.ID, owner) }()

	current, found, err := m.find(m.now())
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("cred: the %s credential vanished while refreshing it", m.spec.Provider)
	}
	credential, stored := m.flush(current)
	if !stale(credential) {
		return credential.Access, nil
	}
	return m.mint(ctx, current.ID, credential, stored, stale, konst.CredReloadRetries)
}

func (m *Manager) waitForLease(ctx context.Context, id int64) error {
	wait := leasePollMin
	if expiry, held, err := m.store.LeaseExpiry(id); err == nil && held {
		wait = min(max(time.Until(expiry), leasePollMin), leasePollMax)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *Manager) mint(ctx context.Context, id int64, credential Credential, stored string, stale func(Credential) bool, reloads int) (string, error) {
	minted, err := refreshGrant(ctx, m.client, m.spec, credential, m.now())
	var refusal tokenRefusal
	if !errors.As(err, &refusal) || refusal.reason() == "" {
		if err != nil {
			return "", err
		}
		return m.keep(id, minted, stored)
	}
	latest, found, err := m.find(m.now())
	if err != nil {
		return "", err
	}
	if found && latest.ID == id && latest.Credential.Refresh != stored && reloads > 0 {
		if !stale(latest.Credential) {
			return latest.Credential.Access, nil
		}
		return m.mint(ctx, id, latest.Credential, latest.Credential.Refresh, stale, reloads-1)
	}
	if err := m.store.DisableIfRefresh(id, stored, "oauth refresh failed: "+refusal.reason(), m.now()); err != nil {
		return "", err
	}
	return "", refusal
}

func (m *Manager) keep(id int64, minted Credential, stored string) (string, error) {
	for try := 1; try <= konst.CredWriteTries; try++ {
		replaced, err := m.save(id, minted, stored, m.now())
		switch {
		case err == nil && replaced:
			return minted.Access, nil
		case err == nil:
			if peer, found, err := m.find(m.now()); err == nil && found && m.fresh(peer.Credential) {
				return peer.Credential.Access, nil
			}
			return minted.Access, nil
		}
		m.note(fmt.Sprintf("%s #%d: writing the new token failed, try %d of %d: %v", m.spec.Provider, id, try, konst.CredWriteTries, err))
		if try < konst.CredWriteTries {
			time.Sleep(writeRetry)
		}
	}
	m.mu.Lock()
	m.held[id] = unwritten{credential: minted, replaces: stored}
	m.mu.Unlock()
	m.note(fmt.Sprintf("%s #%d: the new token is held in memory and written on the next use or sweep", m.spec.Provider, id))
	return minted.Access, nil
}
