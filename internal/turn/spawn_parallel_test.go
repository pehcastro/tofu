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
