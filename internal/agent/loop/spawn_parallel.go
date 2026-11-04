package loop

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/core"
)

type namingOrder struct {
	mu    sync.Mutex
	calls []string
	named map[string]chan struct{}
}

func newNamingOrder(calls []string) *namingOrder {
	order := &namingOrder{calls: calls, named: map[string]chan struct{}{}}
	for _, call := range calls {
		order.named[call] = make(chan struct{})
	}
	return order
}

func (o *namingOrder) waitForEarlier(ctx context.Context, call string) error {
	if o == nil {
		return ctx.Err()
	}
	for _, earlier := range o.calls[:max(slices.Index(o.calls, call), 0)] {
		select {
		case <-o.named[earlier]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func (o *namingOrder) passed(call string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	named, listed := o.named[call]
	if !listed {
		return
	}
	select {
	case <-named:
	default:
		close(named)
	}
}

func (t *SpawnTool) refused(call string) {
	t.mu.Lock()
	naming := t.naming
	t.mu.Unlock()
	naming.passed(call)
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

func (m warmingModel) Ask(ctx context.Context, request core.Request) (core.Decision, error) {
	decision, err := m.model.Ask(ctx, request)
	m.warmup.warmed()
	return decision, err
}

type stagger struct {
	decided string
	why     string
}

func cacheSays(cache catalog.PromptCache) string {
	switch cache {
	case catalog.PromptCachePrefix:
		return "the model caches a shared prompt prefix"
	case catalog.PromptCacheNone:
		return "the model caches no prompt prefix, so a wait would only delay it"
	case catalog.PromptCacheUnknown:
		return "the model does not declare prompt_cache, so cache support is unknown and it staggers as if the prefix were cached"
	}
	panic("turn: no words for prompt cache " + string(cache))
}

func (w *warmup) stagger(ctx context.Context, call string, subAgent Config) (Config, stagger) {
	says := cacheSays(subAgent.PromptCache)
	switch {
	case w == nil:
		return subAgent, stagger{}
	case subAgent.PromptCache == catalog.PromptCacheNone:
		return subAgent, stagger{decided: "runs at once", why: says}
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
		return subAgent, stagger{decided: "leads", why: says + ", so its siblings wait for its first answer"}
	}
	began := time.Now()
	select {
	case <-w.ready:
	case <-time.After(konst.SubAgentWarmMillis * time.Millisecond):
	case <-ctx.Done():
	}
	return subAgent, stagger{decided: "waited " + time.Since(began).Round(time.Millisecond).String(), why: says + ", so it waited for " + w.leader + " to answer first"}
}

func (t *SpawnTool) disjointPrefix(calls []core.ToolCall) int {
	limit := t.limits().Running
	var wave subagents.Roster
	width, firsts, warmups := len(calls), map[string]*warmup{}, map[string]*warmup{}
	for i, call := range calls {
		var args spawnArgs
		if i == limit || call.Name != t.Name() || json.Unmarshal(call.Arguments, &args) != nil ||
			wave.Hold(subagents.SubAgent{ID: call.ID, Owns: args.Owns}) != nil {
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
