package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

var warmBrief = regexp.MustCompile(`brief-\d+`)

type warmScript struct {
	spawns   []llm.ToolCall
	mu       sync.Mutex
	began    map[string]time.Time
	answered map[string]time.Time
	spawned  bool
}

func (s *warmScript) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	brief := ""
	for _, message := range request.Messages {
		if message.Role == llm.RoleUser && brief == "" {
			brief = warmBrief.FindString(message.Content)
		}
	}
	if brief == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.spawned {
			return messageDecision(), nil
		}
		s.spawned = true
		return toolCallDecision(s.spawns...), nil
	}
	s.mu.Lock()
	if _, seen := s.began[brief]; !seen {
		s.began[brief] = time.Now()
	}
	s.mu.Unlock()
	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
	}
	s.mu.Lock()
	if _, seen := s.answered[brief]; !seen {
		s.answered[brief] = time.Now()
	}
	s.mu.Unlock()
	return claimDecision("finished " + brief), nil
}

func warmSpawns(t *testing.T, agents ...string) *warmScript {
	t.Helper()
	script := &warmScript{began: map[string]time.Time{}, answered: map[string]time.Time{}}
	for i, agent := range agents {
		n := strconv.Itoa(i + 1)
		args, err := json.Marshal(spawnArgs{Task: "brief-" + n, Owns: []string{"dir" + n + "/**"}, Agent: agent})
		if err != nil {
			t.Fatal(err)
		}
		script.spawns = append(script.spawns, llm.ToolCall{ID: "call-" + n, Name: "spawn", Arguments: args})
	}
	base := Config{
		Model:          script,
		Spend:          SpendAPIKey,
		Caps:           Caps{MaxSteps: 5},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawner := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	spawner.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel}, {Name: "go-dev", Runs: subagent.RunsModel}}
	orchestrator := base
	orchestrator.Task, orchestrator.Tools, orchestrator.Inbox = "hand the work to sub-agents", NewRegistry(spawner), spawner.Inbox
	startLead(context.Background(), orchestrator, nil).wait(t)
	if len(script.answered) != len(agents) {
		t.Fatalf("%d of %d sub-agents were answered", len(script.answered), len(agents))
	}
	return script
}

func TestSiblingsOfOneDefinitionBeginAfterTheFirstModelCall(t *testing.T) {
	script := warmSpawns(t, "ts-dev", "ts-dev", "ts-dev")

	warmed := script.answered["brief-1"]
	for _, brief := range []string{"brief-2", "brief-3"} {
		if began := script.began[brief]; began.Before(warmed) {
			t.Fatalf("%s began %v before brief-1's first model call returned", brief, warmed.Sub(began))
		}
	}
}

func TestSpawnsOfDifferentDefinitionsBeginTogether(t *testing.T) {
	script := warmSpawns(t, "ts-dev", "go-dev")

	if began, first := script.began["brief-2"], script.answered["brief-1"]; !began.Before(first) {
		t.Fatalf("go-dev began %v after ts-dev's first model call returned, want both at once", began.Sub(first))
	}
}
