package cred

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"
)

const (
	refreshSkew  = 60 * time.Second
	leaseTTL     = 45 * time.Second
	leasePollMin = 50 * time.Millisecond
	leasePollMax = 250 * time.Millisecond
	ownerBytes   = 16
)

const (
	definitivePattern = `(?i)invalid_grant|invalid_token|unauthorized_client|\brevoked\b|refresh[\s_]?token.*expired`
	transientPattern  = `(?i)timeout|network|fetch failed|ECONN(REFUSED|RESET)|ETIMEDOUT|EAI_AGAIN|socket hang up|` +
		`\b(408|425|429|5\d{2})\b|rate.?limit|too many requests|temporar|unavailable|forbidden|permission_denied|` +
		`cloudflare|captcha`
	authStatusPattern = `\b401\b`
)

type flight struct {
	done   chan struct{}
	access string
	err    error
}

type Manager struct {
	store    *Store
	spec     Spec
	find     func(time.Time) (Row, bool, error)
	client   *http.Client
	now      func() time.Time
	mu       sync.Mutex
	inflight map[int64]*flight
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
		inflight: make(map[int64]*flight),
	}
}

func (m *Manager) Access(ctx context.Context) (string, error) {
	row, found, err := m.find(m.now())
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("cred: no %s credential, run tofu login %s", m.spec.Provider, m.spec.Provider)
	}
	if cause := row.Unusable(m.now()); cause != "" {
		return "", fmt.Errorf("cred: the %s credential is %s", m.spec.Provider, cause)
	}
	if m.fresh(row.Credential) {
		return row.Credential.Access, nil
	}
	return m.refreshOnce(ctx, row)
}

func (m *Manager) fresh(credential Credential) bool {
	return m.now().Add(refreshSkew).Before(credential.Expires)
}

func (m *Manager) refreshOnce(ctx context.Context, row Row) (string, error) {
	m.mu.Lock()
	if running, ok := m.inflight[row.ID]; ok {
		m.mu.Unlock()
		<-running.done
		return running.access, running.err
	}
	mine := &flight{done: make(chan struct{})}
	m.inflight[row.ID] = mine
	m.mu.Unlock()

	mine.access, mine.err = m.refreshLeased(ctx, row)

	m.mu.Lock()
	delete(m.inflight, row.ID)
	m.mu.Unlock()
	close(mine.done)
	return mine.access, mine.err
}

func (m *Manager) refreshLeased(ctx context.Context, row Row) (string, error) {
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
	if m.fresh(current.Credential) {
		return current.Credential.Access, nil
	}
	return m.mint(ctx, current)
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

func (m *Manager) mint(ctx context.Context, row Row) (string, error) {
	minted, err := refreshGrant(ctx, m.client, m.spec, row.Credential, m.now())
	if err != nil {
		if definitiveFailure(err.Error()) {
			if disableErr := m.store.Disable(row.ID, "oauth refresh failed: "+err.Error(), m.now()); disableErr != nil {
				return "", disableErr
			}
		}
		return "", err
	}
	replaced, err := m.store.UpdateIfRefreshMatches(row.ID, minted, row.Credential.Refresh, m.now())
	if err != nil {
		return "", err
	}
	if !replaced {
		peer, found, err := m.find(m.now())
		if err != nil {
			return "", err
		}
		if found && m.fresh(peer.Credential) {
			return peer.Credential.Access, nil
		}
	}
	return minted.Access, nil
}

func definitiveFailure(message string) bool {
	if matches(definitivePattern, message) {
		return true
	}
	return matches(authStatusPattern, message) && !matches(transientPattern, message)
}

func matches(pattern, text string) bool {
	found, _ := regexp.MatchString(pattern, text)
	return found
}
