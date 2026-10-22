package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type spawnScript struct {
	spawns  []llm.ToolCall
	working sync.Mutex
	running int
	widest  int
	led     [][]llm.Message
}

func (s *spawnScript) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	first := request.Messages[0].Content
	if brief, isSubAgent := strings.CutPrefix(first[strings.LastIndex(first, "\n")+1:], "brief-"); isSubAgent {
		s.working.Lock()
		s.running++
		s.widest = max(s.widest, s.running)
		s.working.Unlock()
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
		}
		s.working.Lock()
		s.running--
		s.working.Unlock()
		return claimDecision("finished brief-" + brief), nil
	}
	s.working.Lock()
	defer s.working.Unlock()
	s.led = append(s.led, request.Messages)
	if len(s.led) > 1 {
		return messageDecision(), nil
	}
	return toolCallDecision(s.spawns...), nil
}

func (s *spawnScript) heard() string {
	s.working.Lock()
	defer s.working.Unlock()
	var said []string
	for _, message := range s.led[len(s.led)-1] {
		said = append(said, message.Content)
	}
	return strings.Join(said, "\n")
}

func spawnsIn(t *testing.T, owns ...string) *spawnScript {
	t.Helper()
	script := &spawnScript{}
	for i, held := range owns {
		n := strconv.Itoa(i + 1)
		args, err := json.Marshal(spawnArgs{Task: "brief-" + n, Owns: []string{held}})
		if err != nil {
			t.Fatal(err)
		}
		script.spawns = append(script.spawns, llm.ToolCall{ID: "call-" + n, Name: "spawn", Arguments: args})
	}
	root := t.TempDir()
	base := Config{
		Model:          script,
		Spend:          SpendAPIKey,
		Caps:           Caps{MaxSteps: 5},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	orchestrator := base
	orchestrator.Task, orchestrator.Tools, orchestrator.Inbox = "hand the work to sub-agents", NewRegistry(spawn), spawn.Inbox
	started := time.Now()
	startLead(context.Background(), orchestrator, nil).wait(t)
	if took := time.Since(started); took >= 600*time.Millisecond {
		t.Fatalf("%d spawns of 300ms each took %v, want them running together", len(owns), took)
	}
	return script
}

func TestSpawnsWithDisjointOwnsRunTogetherAndEveryReportReachesTheLead(t *testing.T) {
	script := spawnsIn(t, "a/**", "b/**", "c/**")

	if script.widest != 3 {
		t.Fatalf("three spawns ran at most %d at once, want 3", script.widest)
	}
	heard := script.heard()
	for n := 1; n <= 3; n++ {
		running, report := "sub-"+strconv.Itoa(n)+" is running", "finished brief-"+strconv.Itoa(n)
		if !strings.Contains(heard, running) || !strings.Contains(heard, report) {
			t.Errorf("the lead never heard %q and then %q:\n%s", running, report, heard)
		}
	}
}

func TestASpawnOverlappingARunningSubAgentIsRefusedNamingIt(t *testing.T) {
	script := spawnsIn(t, "a/**", "a/b.go")

	if script.widest != 1 {
		t.Fatalf("two spawns holding overlapping paths ran %d at once, want 1", script.widest)
	}
	if heard := script.heard(); !strings.Contains(heard, "finished brief-1") || !strings.Contains(heard, "sub-1 already holds") {
		t.Fatalf("the second spawn was not refused naming sub-1, or the first did not report:\n%s", heard)
	}
}

const firstPiece, secondPiece, thirdPiece = "the first piece", "the second piece", "the third piece"

const refusesTheSecond = `"PreToolUse":[{"matcher":"Agent","hooks":[{"type":"command","command":"if grep -q 'second piece'; then echo 'no ticket names the second piece' >&2; exit 2; fi"}]}]`

func threeSpawns(t *testing.T, hooks string) (*crew, Config) {
	t.Helper()
	_, bash := hookedProject(t, "{"+hooks+"}")
	spawning := func(id, task, owns string) llm.ToolCall { return spawnCall(id, task, owns).ToolCalls[0] }
	model := newCrew(map[string][]llm.Decision{
		leadKey: {toolCallDecision(spawning("call-1", firstPiece, "a/**"), spawning("call-2", secondPiece, "b/**"), spawning("call-3", thirdPiece, "c/**")),
			claimDecision("lead 2"), claimDecision("lead 3"), claimDecision("lead 4"), claimDecision("lead 5")},
		firstPiece: {claimDecision("first done")},
		thirdPiece: {claimDecision("third done")},
	})
	lead := crewLead(t, model)
	lead.Tools, lead.Person = NewRegistry(append(lead.Tools.tools, bash)...), alwaysTrusting
	return model, lead
}

func TestSpawnASiblingRefusedByAHookNeverHoldsTheNextOneBack(t *testing.T) {
	model, lead := threeSpawns(t, refusesTheSecond)

	startLead(context.Background(), lead, nil).wait(t)

	if model.answered(thirdPiece) != 1 {
		t.Errorf("the third spawn's sub-agent answered %d times, want 1", model.answered(thirdPiece))
	}
	if refused := answerTo(t, model, "call-2"); !strings.Contains(refused, "no ticket names the second piece") {
		t.Errorf("the second spawn answered %q, want the hook's refusal", refused)
	}
}

func TestStopLeadEndsATurnWhoseSpawnIsBlockedAndTheQueuedMessageIsSent(t *testing.T) {
	blocksTheThird := `"SubagentSpawn":[{"hooks":[{"type":"command","timeout":600,"command":"if grep -q 'third piece'; then sleep 600; fi"}]}]`
	model, lead := threeSpawns(t, refusesTheSecond+","+blocksTheThird)
	release := model.hold(firstPiece, 1)
	defer release()
	typed := make(chan string, 1)
	led := startLead(context.Background(), lead, typed)
	waitFor(t, "the first sub-agent to start", func() bool { return len(model.requests(firstPiece)) > 0 })
	time.Sleep(500 * time.Millisecond)
	typed <- "the queued message"

	stopped := time.Now()
	lead.Inbox.StopLead()
	waitFor(t, "the queued message to reach the lead", func() bool {
		asked := model.requests(leadKey)
		if len(asked) < 2 {
			return false
		}
		last := asked[len(asked)-1].Messages
		return strings.Contains(last[len(last)-1].Content, "the queued message")
	})
	if took := time.Since(stopped); took > 5*time.Second {
		t.Errorf("the stop took %v to end the turn, want under 5s", took)
	}
	release()
	select {
	case <-led.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the lead did not end within 5s of the first sub-agent's report")
	}
	if len(model.requests(thirdPiece)) != 0 || model.answered(firstPiece) != 1 {
		t.Errorf("after the stop the third was asked %d times and the first answered %d, want the third never started and the first left to finish",
			len(model.requests(thirdPiece)), model.answered(firstPiece))
	}
}
