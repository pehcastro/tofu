package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/subagent"
	"tofu/internal/sys"
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

const (
	sourceLeadCheckIn    = "lead check-in"
	leadCheckInOpening   = "lead check-in: the last turn ended"
	leadBoardReadFile    = "lead-board.json"
	leadNotifiedFile     = "lead-notified.json"
	boardNotifiesOpening = "board hooks notify you: "
)

func boardNotifies(board *boardy.Managed) string {
	if board == nil {
		return ""
	}
	path := filepath.Join(filepath.Dir(board.Store.Dir), leadNotifiedFile)
	var sizes map[string]int64
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &sizes)
	}
	notes, read := board.Store.NotifiesSince(sizes)
	if raw, err := json.Marshal(read); err == nil && !maps.Equal(read, sizes) {
		_ = sys.WriteFile(path, raw, 0o600)
	}
	if len(notes) == 0 {
		return ""
	}
	return NotesAfterTheTask + boardNotifiesOpening + strings.Join(notes, "; ")
}

type leadWork struct {
	running, limit int
	status         map[string]boardy.Status
	free, review   []string
}

type keptFront struct {
	Modified time.Time    `json:"modified"`
	Front    boardy.Front `json:"front"`
	Accepted bool         `json:"accepted"`
}

func keptFronts(store boardy.Store, boards []boardy.Board) map[string][]keptFront {
	path := filepath.Join(filepath.Dir(store.Dir), leadBoardReadFile)
	var kept map[string]keptFront
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &kept)
	}
	fresh, byBoard, reread := map[string]keptFront{}, map[string][]keptFront{}, false
	for _, board := range boards {
		dir := filepath.Join(store.BoardDir(board.Key), boardy.TicketsDirName)
		_ = filepath.WalkDir(dir, func(file string, entry fs.DirEntry, err error) error {
			if err != nil || file == dir {
				return err
			}
			hidden := strings.HasPrefix(entry.Name(), ".")
			if entry.IsDir() && (hidden || filepath.Dir(file) != dir) {
				return fs.SkipDir
			}
			info, err := entry.Info()
			if entry.IsDir() || hidden || filepath.Ext(file) != ".md" || err != nil {
				return nil
			}
			one, known := kept[file]
			if !known || !one.Modified.Equal(info.ModTime()) {
				ticket, err := boardy.ReadTicket(file)
				if err != nil {
					return nil
				}
				one, reread = keptFront{Modified: info.ModTime(), Front: ticket.Front, Accepted: slices.ContainsFunc(ticket.Acceptance, func(lines string) bool { return strings.TrimSpace(lines) != "" })}, true
			}
			fresh[file], byBoard[board.Key] = one, append(byBoard[board.Key], one)
			return nil
		})
	}
	if reread || len(fresh) != len(kept) {
		if raw, err := json.Marshal(fresh); err == nil {
			_ = sys.WriteFile(path, raw, 0o600)
		}
	}
	return byBoard
}

func (t *SpawnTool) work() (leadWork, bool) {
	if t == nil || t.base.Board == nil || t.Inbox == nil {
		return leadWork{}, false
	}
	work := leadWork{limit: t.limits().Running, status: map[string]boardy.Status{}}
	t.Inbox.mu.Lock()
	work.running = t.Inbox.running
	for _, held := range t.Inbox.held {
		if held.agent.Ticket != "" {
			work.status[held.agent.Ticket] = ""
		}
	}
	t.Inbox.mu.Unlock()
	store := t.base.Board.Store
	var keys []string
	for id := range work.status {
		ticket, err := store.Get(id)
		if err != nil {
			delete(work.status, id)
			continue
		}
		work.status[id] = ticket.Status
		if ticket.Status == boardy.Review {
			work.review = append(work.review, id)
		}
		keys = append(keys, ticket.Board())
	}
	if len(work.status) == 0 {
		return leadWork{}, false
	}
	slices.Sort(work.review)
	work.free = freeTodo(store, keys)
	return work, true
}

func freeTodo(store boardy.Store, keys []string) []string {
	boards, _ := store.Boards()
	fronts := keptFronts(store, boards)
	var held, todo []boardy.Front
	for _, board := range boards {
		for _, kept := range fronts[board.Key] {
			switch front := kept.Front; {
			case board.Holding(front.Status):
				held = append(held, front)
			case front.Status == boardy.Todo && len(front.Owns) > 0 && kept.Accepted && slices.Contains(keys, board.Key):
				todo = append(todo, front)
			}
		}
	}
	slices.SortStableFunc(todo, func(a, b boardy.Front) int { return cmp.Compare(a.Priority, b.Priority) })
	var free []string
	for _, ticket := range todo {
		if slices.ContainsFunc(held, func(other boardy.Front) bool {
			return ticket.Parent != other.ID && other.Parent != ticket.ID && slices.ContainsFunc(ticket.Owns, func(glob string) bool {
				return slices.ContainsFunc(other.Owns, func(theirs string) bool { return subagent.Overlap(glob, theirs) })
			})
		}) {
			continue
		}
		free, held = append(free, ticket.ID), append(held, ticket)
	}
	return free
}

func (w leadWork) open() bool {
	return w.running < w.limit && len(w.free) > 0 || len(w.review) > 0
}

func (w leadWork) asks(opening string) string {
	return fmt.Sprintf("%s with %d of %d sub-agents running, free todo %s and review %s: close each review, done or back to doing with the failing lines, "+
		"and spawn free todo up to the limit in this reply, or say in one line why not.", opening, w.running, w.limit, listed(w.free), listed(w.review))
}

func listed(ids []string) string {
	return cmp.Or(strings.Join(ids, ", "), "none")
}

type LeadCheckIn struct {
	Every time.Duration
	Due   func() (string, bool)
}

type leadCheckInKey struct{}

func WithLeadCheckIn(ctx context.Context, arm func(*LeadCheckIn)) context.Context {
	return context.WithValue(ctx, leadCheckInKey{}, arm)
}

func (t *SpawnTool) leadCheckIn(work leadWork) *LeadCheckIn {
	every := t.limits().CheckIn
	if every <= 0 || work.running > 0 || !work.open() {
		return nil
	}
	return &LeadCheckIn{Every: every, Due: func() (string, bool) {
		due, held := t.work()
		if !held || due.running > 0 || !due.open() {
			return "", false
		}
		return due.asks(leadCheckInOpening), true
	}}
}
