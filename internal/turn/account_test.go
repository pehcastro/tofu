package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/crew"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/quota"
	"tofu/internal/recall"
	"tofu/internal/session"
)

const (
	accountTurnSteps       = 4
	accountTurnResultBytes = 20000
)

var accountNow = time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)

func claudeWindows(used float64) quota.Report {
	return quota.Report{Provider: quota.ClaudeSub, Windows: []quota.Window{{
		ID:       "5h",
		Used:     quota.Used{Fraction: used, Reported: true},
		ResetsAt: accountNow.Add(time.Hour),
	}}}
}

type accountBook struct {
	mu       sync.Mutex
	provider quota.Provider
	reports  map[int64]quota.Report
	models   map[int64]Model
	picks    int
	moves    int
}

func (b *accountBook) candidates() []quota.Candidate {
	held := make([]quota.Candidate, 0, len(b.reports))
	for id, report := range b.reports {
		held = append(held, quota.Candidate{ID: id, Provider: report.Provider, Report: report})
	}
	return held
}

func (b *accountBook) account(id int64) Account {
	room := quota.Left(b.reports[id], accountNow)
	return Account{ID: id, Model: b.models[id], Headroom: room.Fraction, Window: room.Window}
}

func (b *accountBook) Pick(context.Context) (Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.picks++
	choice, found := quota.Pick(b.candidates(), b.provider, accountNow)
	if !found {
		return Account{}, context.Canceled
	}
	return b.account(choice.ID), nil
}

func (b *accountBook) Next(_ context.Context, pinned Account) (Account, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !quota.Spent(b.reports[pinned.ID], accountNow) {
		return Account{}, false, nil
	}
	choice, found := quota.Pick(b.candidates(), b.provider, accountNow)
	if !found || choice.ID == pinned.ID {
		return Account{}, false, nil
	}
	b.moves++
	return b.account(choice.ID), true, nil
}

func (b *accountBook) drain(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reports[id] = claudeWindows(1)
}

type spendingTool struct {
	body  string
	once  sync.Once
	spend func()
}

func (t *spendingTool) Name() string { return "read" }

func (t *spendingTool) Definition() llm.Tool {
	return llm.Tool{Name: "read", Description: "a stub tool", Parameters: map[string]any{"type": "object"}}
}

func (t *spendingTool) Run(context.Context, json.RawMessage) (Result, error) {
	if t.spend != nil {
		t.once.Do(t.spend)
	}
	return Result{Content: t.body, Command: "read"}, nil
}

type accountRun struct {
	config   Config
	book     *accountBook
	sessions *session.Store
	tool     *spendingTool
	ended    chan Row
}

func newAccountRun(t *testing.T, reports map[int64]quota.Report) *accountRun {
	t.Helper()
	book := &accountBook{
		provider: quota.ClaudeSub,
		reports:  reports,
		models:   make(map[int64]Model, len(reports)),
	}
	for id := range reports {
		book.models[id] = queuedModel(accountTurnSteps)
	}
	tool := &spendingTool{body: strings.Repeat("x", accountTurnResultBytes)}
	run := &accountRun{book: book, sessions: session.NewStore(t.TempDir()), tool: tool, ended: make(chan Row, accountTurnSteps)}
	config := baseConfig(t, nil, NewRegistry(tool))
	config.Accounts = Accounts{Pick: book.Pick, Next: book.Next}
	config.Caps = Caps{MaxSteps: accountTurnSteps + 2}
	config.ResultBytesCap = accountTurnResultBytes * 2
	config.Budget = recall.Budget{
		Model:         "a stub with the shipped window",
		CeilingTokens: konst.ContextCeilingTokens,
		Bands:         recall.ShippedBands(),
		Automatic:     true,
		Source:        "the shipped ceiling this build measures against",
	}
	config.Sessions = run.sessions
	config.EndedSession = func(row Row) error {
		run.ended <- row
		return nil
	}
	run.config = config
	return run
}

func (r *accountRun) sessionsAfterRunning(t *testing.T) (Row, []Row) {
	t.Helper()
	row, err := Run(context.Background(), r.config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	close(r.ended)
	var ended []Row
	for session := range r.ended {
		ended = append(ended, session)
	}
	return row, ended
}

func queuedModel(steps int) *stubModel {
	decisions := make([]llm.Decision, 0, steps+1)
	for i := 1; i <= steps; i++ {
		decisions = append(decisions, toolCallDecision(llm.ToolCall{
			ID:        "call-" + strconv.Itoa(i),
			Name:      "read",
			Arguments: json.RawMessage(`{"path":"src/file` + strconv.Itoa(i) + `.ts"}`),
		}))
	}
	return &stubModel{decisions: append(decisions, messageDecision())}
}

func headerOf(t *testing.T, sessions *session.Store, id string) session.Header {
	t.Helper()
	listing, err := sessions.Listing()
	if err != nil {
		t.Fatalf("listing the sessions: %v", err)
	}
	for _, header := range listing.Sessions {
		if header.ID == id {
			return header
		}
	}
	t.Fatalf("no session %s among %d written", id, len(listing.Sessions))
	return session.Header{}
}

func TestAPinnedAccountRunsOutAndTheSessionForksOntoTheNextOne(t *testing.T) {
	run := newAccountRun(t, map[int64]quota.Report{1: claudeWindows(0.1), 2: claudeWindows(0.5)})
	run.tool.spend = func() { run.book.drain(1) }

	row, ended := run.sessionsAfterRunning(t)
	if len(ended) != 1 {
		t.Fatalf("%d sessions ended, want the one fork the drained account forces", len(ended))
	}
	first := ended[0]
	if first.Account != 1 || row.Account != 2 {
		t.Fatalf("the session that ended was on account %d and the one that began on %d, want 1 then 2", first.Account, row.Account)
	}
	if row.ForkKind != ForkAccountSpent || row.ForkedFrom != first.ID {
		t.Fatalf("the session that began says fork kind %q from %q", row.ForkKind, row.ForkedFrom)
	}
	before := headerOf(t, run.sessions, first.ID)
	after := headerOf(t, run.sessions, row.ID)
	if before.Account != 1 || before.ForkedInto != row.ID || before.ForkTokensBefore == 0 {
		t.Fatalf("the header that ended says account %d into %q at %d tokens", before.Account, before.ForkedInto, before.ForkTokensBefore)
	}
	if after.Account != 2 || after.ForkKind != string(ForkAccountSpent) || after.Parent != first.ID {
		t.Fatalf("the header that began says account %d, fork kind %q, parent %q", after.Account, after.ForkKind, after.Parent)
	}
	moved := strings.Join(row.Warnings, " ")
	if !strings.Contains(moved, "account #1") || !strings.Contains(moved, "account #2") || !strings.Contains(moved, "fresh prefix") {
		t.Fatalf("the move is not said out loud: %q", moved)
	}
	t.Logf("%s", moved)
}

func TestTheForkOntoAnotherAccountCarriesHandlesAndAsksNoModel(t *testing.T) {
	run := newAccountRun(t, map[int64]quota.Report{1: claudeWindows(0.1), 2: claudeWindows(0.5)})
	run.tool.spend = func() { run.book.drain(1) }

	row, ended := run.sessionsAfterRunning(t)
	var fork *Fork
	steps := len(row.Steps)
	for _, session := range ended {
		steps += len(session.Steps)
		for _, step := range session.Steps {
			if step.Fork != nil {
				fork = step.Fork
			}
		}
	}
	if fork == nil {
		t.Fatal("no step recorded a fork")
	}
	if fork.Kind != ForkAccountSpent {
		t.Fatalf("the fork is a %s, want the one a spent account forces", fork.Kind)
	}
	if len(fork.Carry.Results) == 0 {
		t.Fatalf("the carry names no handle, so the new account starts blind: %q", fork.Carry.Text)
	}
	asked := 0
	for _, model := range run.book.models {
		asked += model.(*stubModel).calls
	}
	if asked != steps {
		t.Fatalf("%d model calls against %d steps, so the move itself asked a model", asked, steps)
	}
	store := recall.NewStore(run.config.ArtifactDir)
	for _, result := range fork.Carry.Results {
		if _, err := store.Fetch(result.Handle); err != nil {
			t.Fatalf("the handle the fork carried does not fetch: %v", err)
		}
	}
	t.Logf("%d handles carried onto the next account across %d steps and %d model calls", len(fork.Carry.Results), steps, asked)
}

func TestAPinnedAccountDoesNotMoveForABetterNumber(t *testing.T) {
	run := newAccountRun(t, map[int64]quota.Report{1: claudeWindows(0.9), 2: claudeWindows(0.05)})

	row, ended := run.sessionsAfterRunning(t)
	if row.Account != 2 {
		t.Fatalf("the session started on account %d, want the one with more headroom", row.Account)
	}
	run.book.mu.Lock()
	picks, moves := run.book.picks, run.book.moves
	run.book.mu.Unlock()
	if picks != 1 {
		t.Fatalf("the account was picked %d times, want once for the session", picks)
	}
	if len(ended) != 0 || moves != 0 {
		t.Fatalf("%d forks and %d moves on a session whose account never ran out", len(ended), moves)
	}
	t.Logf("%d steps on one account, picked once, no fork while account 1 sits at %.0f%% used", len(row.Steps), 90.0)
}

func TestFortyStepsOnOneSessionPickTheAccountOnceAndNeverFork(t *testing.T) {
	const steps = 40
	run := newAccountRun(t, map[int64]quota.Report{1: claudeWindows(0.5), 2: claudeWindows(0)})
	run.book.models[1] = queuedModel(steps)
	run.config.Accounts.Pick = func(context.Context) (Account, error) {
		run.book.mu.Lock()
		defer run.book.mu.Unlock()
		run.book.picks++
		return run.book.account(1), nil
	}
	run.config.Caps = Caps{MaxSteps: steps + 2}
	run.config.Tools = NewRegistry(&spendingTool{body: "a short result"})

	row, ended := run.sessionsAfterRunning(t)
	if len(row.Steps) <= steps {
		t.Fatalf("the session took %d steps, want the forty this test is about", len(row.Steps))
	}
	run.book.mu.Lock()
	picks := run.book.picks
	run.book.mu.Unlock()
	if picks != 1 || len(ended) != 0 {
		t.Fatalf("%d picks and %d forks over %d steps, want one pick and no fork", picks, len(ended), len(row.Steps))
	}
	t.Logf("%d steps, %d pick, %d forks while account 2 sits at full headroom", len(row.Steps), picks, len(ended))
}

func TestTheStepAfterAnAccountMoveBeginsBeforeTheEndedSessionIsWritten(t *testing.T) {
	run := newAccountRun(t, map[int64]quota.Report{1: claudeWindows(0.1), 2: claudeWindows(0.5)})
	next := &measuringModel{decisions: queuedModel(accountTurnSteps).decisions}
	run.book.models[2] = next
	run.tool.spend = func() { run.book.drain(1) }
	budget := noDeadlineForkWait
	if deadline, bounded := t.Deadline(); bounded {
		budget = time.Until(deadline) / 2
	}
	var moves, blocked atomic.Int64
	run.config.EndedSession = func(Row) error {
		moves.Add(1)
		giveUp := time.NewTimer(budget)
		defer giveUp.Stop()
		if !next.waitForAnAskAfter(0, giveUp.C) {
			blocked.Add(1)
		}
		return nil
	}

	row, err := Run(context.Background(), run.config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if moves.Load() != 1 {
		t.Fatalf("%d sessions ended, want the one the drained account forces", moves.Load())
	}
	if blocked.Load() > 0 {
		t.Fatalf("the ended session was written before the account it moved to was asked, so the write sits in front of the turn")
	}
	asks, _ := next.asksAndTheNextWakeUp()
	t.Logf("the account it moved to was asked %d times while the ended session was still being written, and %s finished", asks, row.ID)
}

func accountSpawn(t *testing.T, book *accountBook, tool Tool, parentPick func(context.Context) (Account, error)) (Config, *SpawnTool, chan Row) {
	t.Helper()
	ended := make(chan Row, accountTurnSteps)
	base := Config{
		Accounts:       Accounts{Pick: book.Pick, Next: book.Next},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(tool),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    t.TempDir(),
		NewID:          func() string { return "turn-parent" },
		EndedSession:   func(row Row) error { ended <- row; return nil },
	}
	spawn := NewSpawnTool("turn-parent", base, &crew.Roster{})
	parent := base
	parent.Accounts = Accounts{Pick: parentPick, Next: book.Next}
	parent.Task = "hand the work to a child"
	parent.Tools = NewRegistry(tool, spawn)
	return parent, spawn, ended
}

func TestAChildPicksItsOwnAccountAndLeavesTheParentsDrainedOneAlone(t *testing.T) {
	book := &accountBook{
		provider: quota.ClaudeSub,
		reports:  map[int64]quota.Report{1: claudeWindows(0.1), 2: claudeWindows(0.5)},
		models: map[int64]Model{
			1: &stubModel{decisions: []llm.Decision{
				spawnCall("call-1", "read the file and say what is in it", "src/**"),
				claimDecision("the child answered"),
			}},
			2: &stubModel{decisions: []llm.Decision{claimDecision("I read it and it says pong")}},
		},
	}
	taken := false
	parent, spawn, _ := accountSpawn(t, book, &spendingTool{body: "pong"}, func(ctx context.Context) (Account, error) {
		account, err := book.Pick(ctx)
		if !taken {
			taken = true
			book.drain(account.ID)
		}
		return account, err
	})
	parent.Accounts.Next = nil

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if row.Account != 1 {
		t.Fatalf("the parent ran on account %d, want the one it pinned at the start", row.Account)
	}
	children := spawn.Children()
	if len(children) != 1 {
		t.Fatalf("%d children ran, want the one the parent spawned", len(children))
	}
	if children[0].Account != 2 {
		t.Fatalf("the child ran on account %d, want the one with room while its parent's is drained", children[0].Account)
	}
	t.Logf("parent on account %d, child %s on account %d", row.Account, children[0].ID, children[0].Account)
}

func TestAChildWhoseAccountRunsOutForksItselfAndReportsAsUsual(t *testing.T) {
	book := &accountBook{
		provider: quota.ClaudeSub,
		reports:  map[int64]quota.Report{1: claudeWindows(0.3), 2: claudeWindows(0.1), 3: claudeWindows(0.2)},
		models: map[int64]Model{
			1: &stubModel{decisions: []llm.Decision{
				spawnCall("call-1", "read the file and say what is in it", "src/**"),
				claimDecision("the child answered"),
			}},
			2: &stubModel{decisions: []llm.Decision{
				toolCallDecision(llm.ToolCall{ID: "child-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
			}},
			3: &stubModel{decisions: []llm.Decision{claimDecision("I read it and it says pong")}},
		},
	}
	spends := &spendingTool{body: "pong", spend: func() { book.drain(2) }}
	parent, spawn, ended := accountSpawn(t, book, spends, func(context.Context) (Account, error) {
		book.mu.Lock()
		defer book.mu.Unlock()
		return book.account(1), nil
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("the parent saw an error from a child that only changed accounts: %v", err)
	}
	close(ended)
	var childSessions []Row
	for session := range ended {
		childSessions = append(childSessions, session)
	}
	if len(childSessions) != 1 || childSessions[0].Account != 2 {
		t.Fatalf("%d sessions ended, want one on the child's drained account", len(childSessions))
	}
	children := spawn.Children()
	last := children[len(children)-1]
	if last.Account != 3 || last.ForkKind != ForkAccountSpent {
		t.Fatalf("the child finished on account %d after a %q fork", last.Account, last.ForkKind)
	}
	if last.Outcome != OutcomeStopped {
		t.Fatalf("the child ended %s, want the ordinary end of a finished mission", last.Outcome)
	}
	if spawned := firstToolCall(t, row); spawned.Error != "" {
		t.Fatalf("the parent's spawn call reports an error: %q", spawned.Error)
	}
	t.Logf("the child moved from account 2 to %d and reported %s to a parent that saw no error", last.Account, last.Outcome)
}
