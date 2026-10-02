package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

const leadKey, sideKey, usersRoute = "lead", "side", "add the users route"

type crew struct {
	mu      sync.Mutex
	scripts map[string][]llm.Decision
	asked   map[string][]llm.Request
	answers map[string]int
	holds   map[string]chan struct{}
}

func newCrew(scripts map[string][]llm.Decision) *crew {
	return &crew{scripts: scripts, asked: map[string][]llm.Request{}, answers: map[string]int{}, holds: map[string]chan struct{}{}}
}

func holdKey(key string, request int) string { return key + "#" + strconv.Itoa(request) }

func (c *crew) who(request llm.Request) string {
	if len(request.Tools) == 0 {
		return sideKey
	}
	for _, message := range request.Messages {
		if message.Role != llm.RoleUser {
			continue
		}
		for key := range c.scripts {
			if key != leadKey && key != sideKey && strings.Contains(message.Content, key) {
				return key
			}
		}
		return leadKey
	}
	return leadKey
}

func (c *crew) hold(key string, request int) func() {
	held := make(chan struct{})
	c.mu.Lock()
	c.holds[holdKey(key, request)] = held
	c.mu.Unlock()
	return sync.OnceFunc(func() { close(held) })
}

func (c *crew) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	key := c.who(request)
	c.mu.Lock()
	c.asked[key] = append(c.asked[key], request)
	held := c.holds[holdKey(key, len(c.asked[key]))]
	c.mu.Unlock()
	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return llm.Decision{}, ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	script := c.scripts[key]
	if len(script) == 0 {
		return llm.Decision{}, errors.New("crew: " + key + " has no decision left")
	}
	c.scripts[key], c.answers[key] = script[1:], c.answers[key]+1
	return script[0], nil
}

func (c *crew) requests(key string) []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.Request(nil), c.asked[key]...)
}

func (c *crew) answered(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answers[key]
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !done(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("after 3s, still waiting for %s", what)
		}
	}
}

func crewLead(t *testing.T, model *crew) Config {
	t.Helper()
	root := t.TempDir()
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(read), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel}}
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the work to a sub-agent", NewRegistry(read, spawn), spawn.Inbox
	return lead
}

type leading struct {
	mu   sync.Mutex
	rows []Row
	done chan error
}

func startLead(ctx context.Context, config Config, typed <-chan string) *leading {
	led := &leading{done: make(chan error, 1)}
	go func() {
		led.done <- Lead(ctx, config, typed, nil, func(row Row, _ error) {
			led.mu.Lock()
			led.rows = append(led.rows, row)
			led.mu.Unlock()
		})
	}()
	return led
}

func (l *leading) turns() []Row {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Row(nil), l.rows...)
}

func (l *leading) wait(t *testing.T) []Row {
	t.Helper()
	select {
	case err := <-l.done:
		if err != nil {
			t.Fatalf("the lead ended in an error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lead did not end within 5s")
	}
	return l.turns()
}

func lastUser(request llm.Request) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == llm.RoleUser {
			return request.Messages[i].Content
		}
	}
	return ""
}

func TestAPersonsMessageReachesTheLeadAtItsNextStepWhileASubAgentRuns(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    {spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("noted, sub-1 is on it"), claimDecision("sub-1 is done")},
		usersRoute: {claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 1)
	defer release()
	lead := crewLead(t, model)
	typed := false
	lead.Steering = func() []string {
		if typed || model.answered(leadKey) == 0 {
			return nil
		}
		for deadline := time.Now().Add(3 * time.Second); len(model.requests(usersRoute)) == 0 && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
		typed = true
		return []string{"also add /health"}
	}
	led := startLead(context.Background(), lead, nil)
	waitFor(t, "the lead's second request", func() bool { return len(model.requests(leadKey)) >= 2 })
	if said := lastUser(model.requests(leadKey)[1]); said != "also add /health" || model.answered(usersRoute) != 0 {
		t.Fatalf("the lead's next step saw %q with the sub-agent answered %d times, want the person's message while it runs", said, model.answered(usersRoute))
	}
	release()
	led.wait(t)
}

func TestAPersonsMessageStartsALeadTurnWhenTheLeadIsIdleAndASubAgentRuns(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    {spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is on it"), claimDecision("noted"), claimDecision("sub-1 is done")},
		usersRoute: {claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 1)
	defer release()
	lead := crewLead(t, model)
	typed := make(chan string, 1)
	led := startLead(context.Background(), lead, typed)
	waitFor(t, "the lead's first turn to end", func() bool { return len(led.turns()) == 1 })
	typed <- "also add /health"
	waitFor(t, "a lead turn started by the person's message", func() bool { return len(led.turns()) == 2 })
	if second := led.turns()[1]; second.Task != "also add /health" || model.answered(usersRoute) != 0 {
		t.Fatalf("the second lead turn's task is %q with the sub-agent answered %d times, want the person's message while it runs", second.Task, model.answered(usersRoute))
	}
	release()
	led.wait(t)
}

func TestASubAgentEndingAfterTheLeadsTurnStartsALeadTurnWithItsReport(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    {spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is on it"), claimDecision("sub-1 is done")},
		usersRoute: {claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 1)
	defer release()
	lead := crewLead(t, model)
	led := startLead(context.Background(), lead, nil)
	waitFor(t, "the lead's first turn to end", func() bool { return len(led.turns()) == 1 })
	release()
	turns := led.wait(t)
	if len(turns) != 2 {
		t.Fatalf("the lead ran %d turns, want 2: its own, then one started by the report", len(turns))
	}
	if report := turns[1].Task; !strings.HasPrefix(report, "sub-agent sub-1 is finished") || !strings.Contains(report, "the users route is added") {
		t.Fatalf("the second lead turn's first message is %q, want sub-1's report", report)
	}
}
