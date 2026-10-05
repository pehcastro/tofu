package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

const checkEvery = 40 * time.Millisecond

func checkingLead(t *testing.T, model *crew, every time.Duration) (Config, *SpawnTool) {
	t.Helper()
	root := t.TempDir()
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(read, write), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	spawn.Limits = func() SubAgentLimits { return SubAgentLimits{Running: 2, Depth: 1, CheckIn: every} }
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the work to a sub-agent", NewRegistry(read, write, spawn), spawn.Inbox
	return lead, spawn
}

func writeUsers() llm.Decision {
	args, _ := json.Marshal(map[string]string{"path": "src/users.ts", "content": "export const users = []\n"})
	return toolCallDecision(llm.ToolCall{ID: "call-write", Name: "write", Arguments: args})
}

func noted(n int) []llm.Decision {
	var said []llm.Decision
	for range n {
		said = append(said, claimDecision("noted"))
	}
	return said
}

func TestACheckOnASlowSubAgentWakesTheLeadAndNoneFollowsItsReport(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    append([]llm.Decision{spawnCall("call-spawn", usersRoute, "src/users.ts")}, noted(200)...),
		usersRoute: {writeUsers(), claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 2)
	defer release()
	lead, spawn := checkingLead(t, model, checkEvery)
	led := startLead(context.Background(), lead, nil)
	waitFor(t, "a lead turn started by a check", func() bool {
		turns := led.turns()
		return len(turns) >= 2 && strings.HasPrefix(turns[1].Task, "check on sub-1")
	})
	check := led.turns()[1].Task
	for _, want := range []string{"steps since the last check: 1, of 1 in all", "files changed since then: src/users.ts", "last tool: write"} {
		if !strings.Contains(check, want) {
			t.Fatalf("the first check is %q, want it to say %q", check, want)
		}
	}
	if asked := model.requests(leadKey); !strings.Contains(lastUser(asked[len(asked)-1]), "check on sub-1") {
		t.Fatalf("the lead's latest request ends on %q, want the check", lastUser(asked[len(asked)-1]))
	}
	waitFor(t, "an idle check while the sub-agent's model call is held", func() bool {
		for _, turn := range led.turns()[2:] {
			if strings.Contains(turn.Task, "steps since the last check: none") {
				return true
			}
		}
		return false
	})
	release()
	turns := led.wait(t)
	if last := turns[len(turns)-1].Task; !strings.Contains(last, "sub-agent sub-1 is finished") {
		t.Fatalf("the lead's last turn began on %q, want sub-1's report, with no check after it", last)
	}
	time.Sleep(4 * checkEvery)
	if left := spawn.Inbox.Take(); len(left) > 0 {
		t.Fatalf("after sub-1 ended its timer still posted %q", left)
	}
}

func TestNoCheckIsSentWhenTheIntervalIsZero(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    {spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is on it"), claimDecision("sub-1 is done")},
		usersRoute: {writeUsers(), claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 2)
	defer release()
	lead, _ := checkingLead(t, model, 0)
	led := startLead(context.Background(), lead, nil)
	waitFor(t, "the sub-agent's held step", func() bool { return len(model.requests(usersRoute)) == 2 })
	time.Sleep(4 * checkEvery)
	release()
	if turns := led.wait(t); len(turns) != 2 {
		t.Fatalf("the lead ran %d turns, want 2, its own and the report's, with no check between", len(turns))
	}
}

func TestAnUnreadCheckIsReplacedAndCountsFromTheLastOneRead(t *testing.T) {
	inbox, start := NewInbox(), time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	if err := inbox.reserve(1); err != nil {
		t.Fatal(err)
	}
	held := &heldSubAgent{agent: subagent.SubAgent{ID: "sub-1"}, inbox: NewInbox()}
	inbox.keep(held, func() {})
	check := &checkIn{id: "sub-1", started: start, missed: func(Row) []string { return nil }}
	inbox.attach(held, check)
	step := StepRow{ToolCalls: []ToolCallRow{{Tool: "bash"}}}
	check.stepped(step, start.Add(time.Minute))
	inbox.postCheck(held, check, nil, start.Add(2*time.Minute))
	check.stepped(step, start.Add(3*time.Minute))
	inbox.postCheck(held, check, nil, start.Add(4*time.Minute))
	unread := inbox.Take()
	if len(unread) != 1 || !strings.Contains(unread[0], "steps since the last check: 2, of 2 in all") {
		t.Fatalf("two checks with the first unread left %q, want one check counting both steps", unread)
	}
	inbox.postCheck(held, check, nil, start.Add(34*time.Minute))
	idle := inbox.Take()
	if len(idle) != 1 || !strings.Contains(idle[0], "steps since the last check: none, and no step for 31m0s") {
		t.Fatalf("a check after the lead read the last one is %q, want none since then and idle since the step at 3m", idle)
	}
	inbox.ended(held, "", false, nil)
	inbox.postCheck(held, check, nil, start.Add(64*time.Minute))
	if after := inbox.Take(); len(after) > 0 {
		t.Fatalf("a check on an ended sub-agent was posted: %q", after)
	}
	if _, _, err := inbox.resume("sub-1", "", 1, func() {}); err != nil {
		t.Fatal(err)
	}
	inbox.postCheck(held, check, nil, start.Add(65*time.Minute))
	if stale := inbox.Take(); len(stale) > 0 {
		t.Fatalf("the ended run's timer posted after a resume began, before the new run's check was attached: %q", stale)
	}
	inbox.attach(held, &checkIn{id: "sub-1", started: start.Add(65 * time.Minute), missed: func(Row) []string { return nil }})
	inbox.postCheck(held, check, nil, start.Add(66*time.Minute))
	if stale := inbox.Take(); len(stale) > 0 {
		t.Fatalf("the ended run's timer posted for the resumed run: %q", stale)
	}
}

func TestTheGateIsReadOutsideTheInboxLock(t *testing.T) {
	spawn := &SpawnTool{Inbox: NewInbox(), Limits: func() SubAgentLimits { return SubAgentLimits{CheckIn: checkEvery} }}
	held := &heldSubAgent{agent: subagent.SubAgent{ID: "sub-1"}, running: true}
	heldLock := make(chan bool, 64)
	check := &checkIn{id: "sub-1", started: time.Now(), missed: func(Row) []string {
		free := spawn.Inbox.mu.TryLock()
		if free {
			spawn.Inbox.mu.Unlock()
		}
		heldLock <- !free
		return []string{"test did not run"}
	}}
	stop := spawn.watch(held, check)
	waitFor(t, "a check carrying the gate", func() bool {
		spawn.Inbox.mu.Lock()
		defer spawn.Inbox.mu.Unlock()
		return len(spawn.Inbox.items) > 0
	})
	stop()
	for len(heldLock) > 0 {
		if <-heldLock {
			t.Fatal("the gate was read while the inbox lock was held, so the lead's read of its inbox waited on the disk")
		}
	}
	if got := spawn.Inbox.Take(); len(got) != 1 || !strings.Contains(got[0], "its gate has not passed since its last edit: test did not run") {
		t.Fatalf("the check is %q, want the gate state in it", got)
	}
}
