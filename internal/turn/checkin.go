package turn

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

type checkIn struct {
	mu        sync.Mutex
	id        string
	started   time.Time
	missed    func(Row) []string
	steps     []StepRow
	cost      float64
	steppedAt time.Time
	tool      string
	toolAt    time.Time
	from      int
	sent      int
	posted    string
}

func (c *checkIn) stepped(step StepRow, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, StepRow{ToolCalls: step.ToolCalls})
	c.cost, c.steppedAt = c.cost+step.CostUSD, at
	if called := len(step.ToolCalls); called > 0 {
		c.tool, c.toolAt = step.ToolCalls[called-1].Tool, at
	}
}

func (c *checkIn) gate() []string {
	c.mu.Lock()
	run := Row{Steps: slices.Clone(c.steps)}
	c.mu.Unlock()
	return c.missed(run)
}

func (c *checkIn) compose(lastRead bool, gate []string, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lastRead {
		c.from = c.sent
	}
	recent := c.steps[c.from:]
	c.sent = len(c.steps)
	text := fmt.Sprintf("check on %s, running for %s. steps since the last check: ", c.id, now.Sub(c.started).Round(time.Second))
	if len(recent) == 0 {
		text += fmt.Sprintf("none, and no step for %s, so it may be stuck in a tool or a model call", now.Sub(cmp.Or(c.steppedAt, c.started)).Round(time.Second))
	} else {
		text += fmt.Sprintf("%d, of %d in all", len(recent), len(c.steps))
	}
	var changed []string
	for _, step := range recent {
		for _, call := range step.ToolCalls {
			if path := writtenPath(call); path != "" && !slices.Contains(changed, path) {
				changed = append(changed, path)
			}
		}
	}
	text += ". files changed since then: " + cmp.Or(strings.Join(changed, ", "), "none")
	if len(gate) > 0 {
		text += ". its gate has not passed since its last edit: " + strings.Join(gate, "; ")
	}
	if c.tool != "" {
		text += fmt.Sprintf(". last tool: %s, %s ago", c.tool, now.Sub(c.toolAt).Round(time.Second))
	}
	c.posted = text + fmt.Sprintf(". cost so far: %.4f USD. it goes on unless you correct it with message, or stop it with message and stop true.", c.cost)
	return c.posted
}

func (t *SpawnTool) watch(held *heldSubAgent, check *checkIn) func() {
	every := t.limits().CheckIn
	if every <= 0 {
		return func() {}
	}
	t.Inbox.attach(held, check)
	ticker, stop := time.NewTicker(every), make(chan struct{})
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				t.Inbox.postCheck(held, check, check.gate(), t.clock())
			}
		}
	}()
	return func() { close(stop) }
}
