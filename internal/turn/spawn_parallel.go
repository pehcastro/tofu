package turn

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type namingOrder struct {
	mu     sync.Mutex
	turned *sync.Cond
	calls  []string
	named  map[string]bool
}

func newNamingOrder(calls []string) *namingOrder {
	order := &namingOrder{calls: calls, named: map[string]bool{}}
	order.turned = sync.NewCond(&order.mu)
	return order
}

func (o *namingOrder) waitForEarlier(call string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, earlier := range o.calls[:max(slices.Index(o.calls, call), 0)] {
		for !o.named[earlier] {
			o.turned.Wait()
		}
	}
}

func (o *namingOrder) passed(call string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.named[call] = true
	o.mu.Unlock()
	o.turned.Broadcast()
}

type warmup struct {
	leader string
	once   sync.Once
	ready  chan struct{}
}

func (w *warmup) warmed() { w.once.Do(func() { close(w.ready) }) }

func (w *warmup) leaderDone(call string) {
	if w != nil && w.leader == call {
		w.warmed()
	}
}

type warmingModel struct {
	model  Model
	warmup *warmup
}

func (m warmingModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	decision, err := m.model.Ask(ctx, request)
	m.warmup.warmed()
	return decision, err
}

func (w *warmup) stagger(ctx context.Context, call string, subAgent Config) Config {
	switch {
	case w == nil:
	case w.leader == call:
		if subAgent.Model != nil {
			subAgent.Model = warmingModel{model: subAgent.Model, warmup: w}
		}
		if pick := subAgent.Accounts.Pick; pick != nil {
			subAgent.Accounts.Pick = func(ctx context.Context) (Account, error) {
				account, err := pick(ctx)
				if account.Model != nil {
					account.Model = warmingModel{model: account.Model, warmup: w}
				}
				return account, err
			}
		}
	default:
		select {
		case <-w.ready:
		case <-time.After(konst.SubAgentWarmMillis * time.Millisecond):
		case <-ctx.Done():
		}
	}
	return subAgent
}

func (t *SpawnTool) disjointPrefix(calls []llm.ToolCall) int {
	limit := t.limits().Running
	var wave subagent.Roster
	width, firsts, warmups := len(calls), map[string]*warmup{}, map[string]*warmup{}
	for i, call := range calls {
		var args spawnArgs
		if i == limit || call.Name != t.Name() || json.Unmarshal(call.Arguments, &args) != nil ||
			wave.Hold(subagent.SubAgent{ID: call.ID, Owns: args.Owns}) != nil {
			width = i
			break
		}
		first, sibling := firsts[args.Agent]
		switch {
		case args.Agent == "":
		case sibling:
			warmups[first.leader], warmups[call.ID] = first, first
		default:
			firsts[args.Agent] = &warmup{leader: call.ID, ready: make(chan struct{})}
		}
	}
	var called []string
	for _, call := range calls[:width] {
		called = append(called, call.ID)
	}
	t.mu.Lock()
	t.warmups, t.naming = warmups, newNamingOrder(called)
	t.mu.Unlock()
	return width
}
