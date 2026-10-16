package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type startupStep struct {
	name string
	took time.Duration
}

type startupClock struct {
	mu    sync.Mutex
	began time.Time
	last  time.Time
	asked time.Time
	steps []startupStep

	usageAskedAlongside []time.Duration
}

func clockFromNow() *startupClock {
	now := time.Now()
	return &startupClock{began: now, last: now}
}

func clockFromProcessStart() *startupClock {
	clock := clockFromNow()
	if started, known := processStarted(); known && started.Before(clock.last) {
		clock.began, clock.steps = started, []startupStep{{name: "process start to tofu run", took: clock.last.Sub(started)}}
	}
	return clock
}

func (c *startupClock) add(name string) {
	now := time.Now()
	c.steps, c.last = append(c.steps, startupStep{name: name, took: now.Sub(c.last)}), now
}

func (c *startupClock) step(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked.IsZero() {
		c.add(name)
	}
}

func (c *startupClock) picking(accounts turn.Accounts, store *cred.Store) turn.Accounts {
	pick := accounts.Pick
	if pick == nil {
		return accounts
	}
	accounts.Pick = func(ctx context.Context) (turn.Account, error) {
		c.step("turn opening")
		account, err := pick(ctx)
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
		c.add(name)
		return account, err
	}
	return accounts
}

func (a *accounts) checkUsageAlongside(clock *startupClock) (wait func()) {
	var checking sync.WaitGroup
	if a.fixed != nil || a.store == nil {
		return checking.Wait
	}
	rows, err := a.store.List()
	poller, pollerErr := a.quota()
	if err != nil || pollerErr != nil {
		return checking.Wait
	}
	mine := slices.DeleteFunc(rows, func(row cred.Row) bool { return row.Credential.Provider != a.provider })
	began := time.Now()
	checking.Go(func() {
		_ = safe.Call(sys.Crashes("."), "usage check alongside", func() error {
			results, _ := pollRowsOn(context.Background(), a.store, mine, a.now, a.urls, poller)
			clock.mu.Lock()
			defer clock.mu.Unlock()
			for _, result := range results {
				if !result.report.FetchedAt.Before(began) {
					clock.usageAskedAlongside = append(clock.usageAskedAlongside, result.report.FetchedAt.Sub(began))
				}
			}
			return nil
		})
	})
	return checking.Wait
}

type firstRequestModel struct {
	inner turn.Model
	clock *startupClock
}

func (m firstRequestModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.clock.mu.Lock()
	if m.clock.asked.IsZero() {
		m.clock.add("turn before the first request: session, hooks and prompt")
		m.clock.asked = m.clock.last
	}
	m.clock.mu.Unlock()
	return m.inner.Ask(ctx, request)
}

func (c *startupClock) leading(model turn.Model) turn.Model {
	return firstRequestModel{inner: model, clock: c}
}

func (c *startupClock) record(store *session.Store, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked.IsZero() {
		return nil
	}
	said := make([]string, len(c.steps))
	for i, step := range c.steps {
		said[i] = step.name + " " + step.took.Round(time.Millisecond).String()
	}
	for _, took := range c.usageAskedAlongside {
		said = append(said, "alongside, a usage check asked the endpoint and answered in "+took.Round(time.Millisecond).String())
	}
	log, err := store.Open(session.Header{ID: id})
	if err != nil {
		return err
	}
	head := log.Header().Head
	_, err = log.Append(session.Event{Kind: session.EventNotice, At: c.asked},
		session.NoticeBody{Text: "startup took " + c.asked.Sub(c.began).Round(time.Millisecond).String() + " before the first request: " + strings.Join(said, " · ")})
	return errors.Join(err, log.Edit(func(header *session.Header) { header.Head = head }), log.Close())
}
