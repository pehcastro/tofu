package host

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/transport"
	"tofu/internal/turn"
)

type subAccount struct {
	mu           sync.Mutex
	id           int64
	before       []llm.Decision
	limitedUntil time.Time
	spent        bool
	asked        []llm.Request
}

func (a *subAccount) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, request)
	if len(a.before) > 0 {
		reply := a.before[0]
		a.before = a.before[1:]
		return reply, nil
	}
	if time.Now().Before(a.limitedUntil) {
		a.spent = true
		return llm.Decision{}, &transport.Error{Kind: transport.KindRateLimit, Op: "anthropic.Ask", Status: 429, Detail: "rate_limit_error"}
	}
	return llm.Decision{Build: "stub", Outcome: llm.OutcomeMessage, Content: "done on account " + strconv.FormatInt(a.id, 10)}, nil
}

func (a *subAccount) requests() []llm.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.asked)
}

type limitedSubscriptions struct {
	dir      string
	accounts []*subAccount
	codex    *subAccount
	mu       sync.Mutex
	prepared []string
}

func (e *limitedSubscriptions) wires() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.prepared)
}

func (e *limitedSubscriptions) Prepare(start Turn, hooks Hooks) (Prepared, error) {
	wire := "anthropic"
	if start.Pick.Wire == "codex" {
		wire = "codex"
	}
	e.mu.Lock()
	e.prepared = append(e.prepared, wire)
	e.mu.Unlock()
	store, err := session.OpenIn(e.dir)
	config := turn.Config{Task: start.Task, Caps: turn.Caps{MaxSteps: 6}, Inbox: hooks.Inbox, ResultBytesCap: konst.TurnResultBytesCap, Spend: turn.SpendSubscription, Wire: wire}
	prepared := Prepared{Config: config, Sessions: store}
	if wire == "codex" {
		prepared.Config.Model = e.codex
		return prepared, err
	}
	held := func(id int64) *subAccount { return e.accounts[id-1] }
	account := func(held *subAccount) turn.Account {
		return turn.Account{ID: held.id, Model: held, Headroom: 0.4, Window: "5h"}
	}
	prepared.Config.Accounts = turn.Accounts{
		Pick: func(context.Context) (turn.Account, error) { return account(e.accounts[0]), nil },
		Next: func(_ context.Context, pinned turn.Account) (turn.Account, bool, error) {
			pinnedAt := held(pinned.ID)
			pinnedAt.mu.Lock()
			spent := pinnedAt.spent
			pinnedAt.mu.Unlock()
			for _, other := range e.accounts {
				if spent && other.id != pinned.ID {
					return account(other), true, nil
				}
			}
			return turn.Account{}, false, nil
		},
		Resets: func(_ context.Context, pinned turn.Account) (time.Time, bool) {
			return held(pinned.ID).limitedUntil, true
		},
	}
	prepared.Account = func(id int64) TurnAccount {
		return TurnAccount{Source: "claude-sub", AccountID: id, Model: "claude-opus-5"}
	}
	if e.codex != nil {
		prepared.Others = func() []OtherSource {
			return []OtherSource{{Name: "codex-sub", Pick: Pick{Wire: "codex", Model: "gpt-5.6-sol"}}}
		}
	}
	return prepared, err
}

func (*limitedSubscriptions) Renew() {}

func (*limitedSubscriptions) OneTurnPerProject() bool { return false }

type limitRun struct {
	t       *testing.T
	c       *wireClient
	h       *Host
	engine  *limitedSubscriptions
	session string
	turn    string
	seen    []string
}

func startLimited(t *testing.T, engine *limitedSubscriptions) *limitRun {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	engine.dir = project
	h, _ := New(Config{Dir: project, Engine: engine})
	t.Cleanup(h.Close)
	c := servingHost(t, h, project, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch","capabilities":["questions"]}`)
	c.answer("1", &InitializeResult{})
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	return &limitRun{t: t, c: c, h: h, engine: engine, session: opened.Session}
}

func (r *limitRun) send() {
	r.c.ask("3", "turn.send", `{"session":"`+r.session+`","text":"read the note"}`)
	var running TurnResult
	r.c.answer("3", &running)
	r.turn = running.Turn
}

func (r *limitRun) until(method, holding string) wireLine {
	r.t.Helper()
	return r.c.until(func(line wireLine) bool {
		switch line.Method {
		case "turn.account", "note", "failure", "turn.completed", QuestionMethod, "turn.steered":
			r.seen = append(r.seen, line.Method+" "+string(line.Params))
			r.t.Logf("frame %s %s", line.Method, line.Params)
		}
		return line.Method == method && strings.Contains(string(line.Params), holding)
	}, method+" holding "+holding)
}

func (r *limitRun) answerTheQuestion(chosen string) QuestionRequest {
	r.t.Helper()
	line := r.until(QuestionMethod, "")
	var asked QuestionRequest
	if err := json.Unmarshal(line.Params, &asked); err != nil {
		r.t.Fatal(err)
	}
	if _, err := io.WriteString(r.c.in, `{"jsonrpc":"2.0","id":`+string(line.ID)+`,"result":{"outcome":"submitted","answers":[{"id":"limit","chosen":["`+chosen+`"]}]}}`+"\n"); err != nil {
		r.t.Fatal(err)
	}
	return asked
}

func (r *limitRun) saw(method string) bool {
	return slices.ContainsFunc(r.seen, func(frame string) bool { return strings.HasPrefix(frame, method+" ") })
}

func limitedFor(id int64, limit time.Duration, before ...llm.Decision) *subAccount {
	return &subAccount{id: id, limitedUntil: time.Now().Add(limit), before: before}
}

func TestRotateToTheSecondAccountWithoutAsking(t *testing.T) {
	r := startLimited(t, &limitedSubscriptions{accounts: []*subAccount{limitedFor(1, time.Hour), limitedFor(2, 0)}, codex: &subAccount{id: 9}})
	r.send()
	r.until("turn.account", `"reason":"moved"`)
	r.until("turn.completed", `"status":"finished"`)
	if r.saw(QuestionMethod) || r.saw("failure") || len(r.engine.accounts[1].requests()) != 1 {
		t.Fatalf("frames %q: want no question, no failure, and one request on account 2", r.seen)
	}
}

func TestLimitOnEveryAccountAsksAndContinuesOnCodexFromTheSameConversation(t *testing.T) {
	thought := llm.Decision{Build: "stub", Outcome: llm.OutcomeToolCalls, Thinking: llm.Thinking{Text: "the note first", Signature: "sig-anthropic"},
		ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "nothing", Arguments: json.RawMessage(`{}`)}}}
	codex := &subAccount{id: 9}
	r := startLimited(t, &limitedSubscriptions{accounts: []*subAccount{limitedFor(1, time.Hour, thought), limitedFor(2, time.Hour)}, codex: codex})
	r.send()
	asked := r.answerTheQuestion("continue on codex-sub")
	r.until("turn.completed", `"status":"finished"`)
	var labels []string
	for _, option := range asked.Questions[0].Options {
		labels = append(labels, option.Label)
	}
	if len(asked.Questions) != 1 || !slices.Equal(labels, []string{"continue on codex-sub", "I'll wait for the limit to reset"}) {
		t.Fatalf("the question offered %q", labels)
	}
	requests := codex.requests()
	if len(requests) != 1 || !slices.Equal(r.engine.wires(), []string{"anthropic", "codex"}) {
		t.Fatalf("codex was asked %d times and the wires prepared were %v, want once and anthropic then codex", len(requests), r.engine.wires())
	}
	var task, call bool
	for _, message := range requests[0].Messages {
		task = task || message.Role == llm.RoleUser && strings.Contains(message.Content, "read the note")
		call = call || len(message.ToolCalls) == 1 && message.ToolCalls[0].ID == "call-1"
		if !message.Thinking.Empty() {
			t.Fatalf("codex was sent anthropic's thinking block %+v", message.Thinking)
		}
	}
	if !task || !call {
		t.Fatalf("codex was sent %+v, want the first task and the tool call from the anthropic steps", requests[0].Messages)
	}
}

func TestResumeAtTheResetWithNoInputAndEscCancelsTheWait(t *testing.T) {
	t.Run("the turn resumes by itself", func(t *testing.T) {
		r := startLimited(t, &limitedSubscriptions{accounts: []*subAccount{limitedFor(1, 1500*time.Millisecond), limitedFor(2, time.Hour)}, codex: &subAccount{id: 9}})
		r.send()
		r.answerTheQuestion("I'll wait for the limit to reset")
		r.until("note", "resumes by itself at")
		r.until("note", "limit has reset")
		r.until("turn.completed", `"status":"finished"`)
		asked := r.engine.accounts[0].requests()
		last := asked[len(asked)-1].Messages
		if !slices.Equal(r.engine.wires(), []string{"anthropic", "anthropic"}) || last[len(last)-1].Content != resumedTask {
			t.Fatalf("wires %v, last message %q: want anthropic twice and the resume sent as %q", r.engine.wires(), last[len(last)-1].Content, resumedTask)
		}
	})
	t.Run("esc cancels the wait", func(t *testing.T) {
		r := startLimited(t, &limitedSubscriptions{accounts: []*subAccount{limitedFor(1, time.Hour), limitedFor(2, time.Hour)}, codex: &subAccount{id: 9}})
		r.send()
		r.answerTheQuestion("I'll wait for the limit to reset")
		r.until("note", "resumes by itself at")
		r.c.ask("4", "turn.stop", `{"session":"`+r.session+`","turn":"`+r.turn+`"}`)
		r.until("note", "was cancelled")
		r.until("turn.completed", "")
		if wires := r.engine.wires(); len(wires) != 1 {
			t.Fatalf("the turn prepared %v after esc, want nothing more", wires)
		}
	})
	t.Run("new input ends the wait and becomes the task", func(t *testing.T) {
		r := startLimited(t, &limitedSubscriptions{accounts: []*subAccount{limitedFor(1, time.Hour), limitedFor(2, time.Hour)}})
		r.send()
		r.until("note", "resumes by itself at")
		r.engine.accounts[0].mu.Lock()
		r.engine.accounts[0].limitedUntil = time.Time{}
		r.engine.accounts[0].mu.Unlock()
		r.c.ask("4", "turn.steer", `{"session":"`+r.session+`","expectedTurnId":"`+r.turn+`","text":"try again now"}`)
		r.until("turn.completed", `"status":"finished"`)
		asked := r.engine.accounts[0].requests()
		last := asked[len(asked)-1].Messages
		if r.saw(QuestionMethod) || !strings.Contains(last[len(last)-1].Content, "try again now") {
			t.Fatalf("frames %q, last message %q: want no question with no other source, and the typed text as the task", r.seen, last[len(last)-1].Content)
		}
	})
	t.Run("a cron turn waits and never asks", func(t *testing.T) {
		r := startLimited(t, &limitedSubscriptions{accounts: []*subAccount{limitedFor(1, time.Second), limitedFor(2, time.Hour)}, codex: &subAccount{id: 9}})
		r.h.mu.Lock()
		r.h.begin(Pick{Wire: "anthropic"}, "check the build", Origin{Kind: OriginCron, Job: "c1"})
		r.h.mu.Unlock()
		r.until("note", "resumes by itself at")
		r.until("turn.completed", `"status":"finished"`)
		if r.saw(QuestionMethod) {
			t.Fatalf("a cron turn asked: %q", r.seen)
		}
	})
}
