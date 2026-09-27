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
	final   []llm.Message
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
	if request.Messages[len(request.Messages)-1].Role == llm.RoleTool {
		s.final = request.Messages
		return messageDecision(), nil
	}
	return toolCallDecision(s.spawns...), nil
}

func spawnsIn(t *testing.T, owns ...string) (*spawnScript, Config) {
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
	orchestrator := base
	orchestrator.Task = "hand the work to sub-agents"
	orchestrator.Tools = NewRegistry(NewSpawnTool("turn-orchestrator", base, &subagent.Roster{}))
	return script, orchestrator
}

func TestSpawnsWithDisjointOwnsRunTogetherAndAnswerInCallOrder(t *testing.T) {
	script, orchestrator := spawnsIn(t, "a/**", "b/**", "c/**")

	started := time.Now()
	if _, err := Run(context.Background(), orchestrator); err != nil {
		t.Fatalf("Run: %v", err)
	}
	took := time.Since(started)

	if took >= 600*time.Millisecond || script.widest != 3 {
		t.Fatalf("three 300ms spawns took %v with at most %d running at once, want under 600ms with 3", took, script.widest)
	}
	answers := script.final[len(script.final)-3:]
	for i, answer := range answers {
		n := strconv.Itoa(i + 1)
		if answer.ToolCallID != "call-"+n || !strings.Contains(answer.Content, "finished brief-"+n) {
			t.Fatalf("answer %d is %s carrying %q, want call-%s carrying brief-%s", i, answer.ToolCallID, answer.Content, n, n)
		}
	}
}

func TestSpawnsWithOverlappingOwnsRunInOrder(t *testing.T) {
	script, orchestrator := spawnsIn(t, "a/**", "a/b.go")

	if _, err := Run(context.Background(), orchestrator); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if script.widest != 1 {
		t.Fatalf("two spawns holding overlapping paths ran %d at once, want 1", script.widest)
	}
	answers := script.final[len(script.final)-2:]
	if !strings.Contains(answers[0].Content, "finished brief-1") {
		t.Fatalf("the first spawn did not run: %q", answers[0].Content)
	}
	if held := answers[1].Content; !strings.Contains(held, "already holds") || !strings.Contains(held, "overlaps") {
		t.Fatalf("the second spawn does not say it waited on an overlap: %q", held)
	}
}
