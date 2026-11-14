package main

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	cred "tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/outside"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
)

type startupClock struct {
	mu        sync.Mutex
	began     time.Time
	last      time.Time
	asked     time.Time
	steps     []walls.Wall
	alongside []walls.Wall
}

func clockFromNow() *startupClock {
	now := time.Now()
	return &startupClock{began: now, last: now}
}

func clockFromProcessStart() *startupClock {
	clock := clockFromNow()
	if started, known := processStarted(); known && started.Before(clock.last) {
		clock.began, clock.last = started, started
		clock.add("process start to tofu run", 0)
	}
	return clock
}

func (c *startupClock) add(name string, waited time.Duration) {
	now, outsideMS := time.Now(), waited.Milliseconds()
	c.steps = append(c.steps, walls.Wall{Op: walls.OpStartupStep, Name: name, MS: now.Sub(c.last).Milliseconds(), At: c.last.UnixMilli(), OK: true, OutsideMS: &outsideMS})
	c.last = now
}

func (c *startupClock) step(name string, waited time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked.IsZero() {
		c.add(name, waited)
	}
}

func (c *startupClock) picking(accounts loop.Accounts, store *cred.Store) loop.Accounts {
	pick := accounts.Pick
	if pick == nil {
		return accounts
	}
	accounts.Pick = func(ctx context.Context) (loop.Account, error) {
		c.step("turn opening", 0)
		picking, clock := outside.Open(ctx, time.Now)
		account, err := pick(picking)
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.asked.IsZero() {
			return account, err
		}
		name := "account pick: credentials, token refresh and usage check"
		if store != nil {
			rows, listErr := store.List()
			if listErr == nil && slices.ContainsFunc(rows, func(row cred.Row) bool { return row.ID == account.ID && row.Credential.Refreshed.After(c.began) }) {
				name += ", and the token was refreshed"
			}
		}
		c.add(name, clock.Outside())
		return account, err
	}
	return accounts
}

func (c *startupClock) checkUsageAlongside(a *wiring.Accounts) (wait func()) {
	var checking sync.WaitGroup
	if a.Fixed != nil || a.Store == nil {
		return checking.Wait
	}
	rows, err := a.Store.List()
	poller, pollerErr := a.Quota()
	if err != nil || pollerErr != nil {
		return checking.Wait
	}
	mine := slices.DeleteFunc(rows, func(row cred.Row) bool { return row.Credential.Provider != a.Provider })
	began := time.Now()
	checking.Go(func() {
		_ = safe.Call(platformproject.Crashes("."), "usage check alongside", func() error {
			results, _ := wiring.PollRowsOn(context.Background(), a.Store, mine, a.Now, a.URLs, poller)
			c.mu.Lock()
			defer c.mu.Unlock()
			for _, result := range results {
				if answered := result.Report.FetchedAt.Sub(began).Milliseconds(); answered >= 0 {
					c.alongside = append(c.alongside, walls.Wall{Op: walls.OpStartupStep, Name: "usage check alongside", MS: answered, At: began.UnixMilli(), OK: true, OutsideMS: &answered})
				}
			}
			return nil
		})
	})
	return checking.Wait
}

type firstRequestModel struct {
	inner loop.Model
	clock *startupClock
}

func (m firstRequestModel) Ask(ctx context.Context, request core.Request) (core.Decision, error) {
	m.clock.mu.Lock()
	if m.clock.asked.IsZero() {
		m.clock.add("turn before the first request: session, hooks and prompt", 0)
		m.clock.asked = m.clock.last
	}
	m.clock.mu.Unlock()
	return m.inner.Ask(ctx, request)
}

func (c *startupClock) leading(model loop.Model) loop.Model {
	return firstRequestModel{inner: model, clock: c}
}

type turnOrdinal string

const (
	firstTurn turnOrdinal = ""
	laterTurn turnOrdinal = "later turn "
)

func (c *startupClock) record(store *session.Store, id string, turn turnOrdinal) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked.IsZero() {
		return nil
	}
	var outsideMS int64
	for i, step := range c.steps {
		outsideMS += *step.OutsideMS
		c.steps[i].Name = string(turn) + step.Name
	}
	whole := walls.Wall{Op: walls.OpStartup, Name: string(turn) + "to first request", MS: c.asked.Sub(c.began).Milliseconds(), At: c.began.UnixMilli(), OK: true, OutsideMS: &outsideMS}
	log, err := store.Open(session.Header{ID: id})
	if err != nil {
		return err
	}
	head := log.Header().Head
	for _, wall := range slices.Concat([]walls.Wall{whole}, c.steps, c.alongside) {
		_, appended := log.Append(session.Event{Kind: session.EventWall, At: c.asked}, wall)
		err = errors.Join(err, appended)
	}
	return errors.Join(err, log.Edit(func(header *session.Header) { header.Head = head }), log.Close())
}
