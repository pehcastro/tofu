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
	"unicode"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/safe"
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
	stop, report := make(chan struct{}), sys.Crashes(t.Project)
	safe.Go(report, "check-in watch", func() {
		safe.Loop(report, "check-in watch", stop, func() {
			ticker := time.NewTicker(every)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					t.Inbox.postCheck(held, check, check.gate(), t.clock())
				}
			}
		})
	})
	return func() { close(stop) }
}

const (
	leadCheckInOpening   = "lead check-in: the last turn ended"
	leadEndedOpening     = "you ended the turn"
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
		return work, false
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

func (w leadWork) checkInDue(held bool, reviews []boardy.Front) bool {
	return w.running == 0 && (held && w.open() || len(reviews) > 0)
}

func (w leadWork) asks(opening, review string) string {
	return fmt.Sprintf("%s with %d of %d sub-agents running, free todo %s and review %s: close each review, done or back to doing with the failing lines, "+
		"and spawn free todo up to the limit in this reply, or say in one line why not.", opening, w.running, w.limit, listed(w.free), review)
}

func (w leadWork) quietBecause(messages []llm.Message, byThePerson bool, said string) string {
	words := strings.FieldsFunc(said, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' })
	ids := append(slices.Collect(maps.Keys(w.status)), w.free...)
	if byThePerson && !slices.ContainsFunc(words, func(word string) bool { return slices.Contains(ids, strings.TrimRight(word, "-")) }) {
		return "the person's message names none of the board work"
	}
	asked := w.asks(leadEndedOpening, listed(w.review))
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Origin.Source != llm.SourceSlots || !strings.HasPrefix(messages[i].Content, leadEndedOpening+" with ") {
			continue
		}
		if messages[i].Content == asked && slices.ContainsFunc(messages[i+1:], func(reply llm.Message) bool {
			return reply.Role == llm.RoleAssistant && !llm.BlankText(reply.Content)
		}) {
			return "the running, free and review sets are as they were at its last ask, and the lead gave its reason then"
		}
		return ""
	}
	return ""
}

func (t *SpawnTool) boardReviews() []boardy.Front {
	if t == nil || t.base.Board == nil {
		return nil
	}
	return BoardTickets(t.base.Board.Store, boardy.Review)
}

func BoardTickets(store boardy.Store, status boardy.Status) []boardy.Front {
	boards, _ := store.Boards()
	var tickets []boardy.Front
	for _, fronts := range keptFronts(store, boards) {
		for _, kept := range fronts {
			if kept.Front.Status == status {
				tickets = append(tickets, kept.Front)
			}
		}
	}
	slices.SortFunc(tickets, func(a, b boardy.Front) int { return cmp.Or(a.Updated.Compare(b.Updated), cmp.Compare(a.ID, b.ID)) })
	return tickets
}

func Waiting(tickets []boardy.Front, now time.Time) (lines []string, more int) {
	shown := tickets[:min(len(tickets), konst.LeadCheckInReviewsMost)]
	lines = make([]string, len(shown))
	for i, one := range shown {
		lines[i] = fmt.Sprintf("%s (%s since last change)", one.ID, now.Sub(one.Updated).Round(time.Minute))
	}
	return lines, len(tickets) - len(shown)
}

func waitingReviews(reviews []boardy.Front, now time.Time) string {
	waiting, more := Waiting(reviews, now)
	said := listed(waiting)
	if more > 0 {
		said += fmt.Sprintf(", and %d more in review", more)
	}
	return said
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

func (t *SpawnTool) leadCheckIn(work leadWork, held bool) *LeadCheckIn {
	every := t.limits().CheckIn
	if every <= 0 || !work.checkInDue(held, t.boardReviews()) {
		return nil
	}
	return &LeadCheckIn{Every: every, Due: func() (string, bool) {
		due, held := t.work()
		reviews := t.boardReviews()
		if !due.checkInDue(held, reviews) {
			return "", false
		}
		return due.asks(leadCheckInOpening, waitingReviews(reviews, t.clock())), true
	}}
}
