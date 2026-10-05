package turn_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type changingBeforeSpawn struct {
	*prefetchScript
	leadAsks int
	change   func() error
}

func (s *changingBeforeSpawn) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if !strings.Contains(request.Messages[0].Content, s.brief) {
		s.leadAsks++
		if s.leadAsks == 3 {
			if err := s.change(); err != nil {
				return llm.Decision{}, err
			}
		}
	}
	return s.prefetchScript.Ask(ctx, request)
}

func reading(calls ...map[string]any) llm.Decision {
	decision := llm.Decision{Build: "m1", Outcome: llm.OutcomeToolCalls}
	for i, args := range calls {
		raw, err := json.Marshal(args)
		if err != nil {
			panic(err)
		}
		decision.ToolCalls = append(decision.ToolCalls, llm.ToolCall{ID: "read-" + string(rune('a'+i)), Name: "read", Arguments: raw})
	}
	return decision
}

func TestTheBriefPlacesEachFileTheLeadReadInsideTheOwnedPathsOnceAndAsItIsNow(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		filepath.Join(root, "src", "a.ts"):    "ALPHA as the lead saw it\n",
		filepath.Join(root, "src", "b.ts"):    "BRAVO line one\nBRAVO line two\n",
		filepath.Join(root, "src", "c.ts"):    "CHARLIE named by the brief\n",
		filepath.Join(root, "src", "d.ts"):    "DELTA named by its base name alone\n",
		filepath.Join(root, "docs", "x.md"):   "XRAY outside the owned paths\n",
		filepath.Join(root, "src", "gone.ts"): "GOLF deleted before the spawn\n",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	memoised := tools.NewMemo().Wrap([]turn.Tool{read})
	brief := "Change src/c.ts so it exports a name; c.ts is the entry point and d.ts imports it."
	script := &changingBeforeSpawn{
		prefetchScript: &prefetchScript{brief: brief, lead: []llm.Decision{
			reading(map[string]any{"path": "src/a.ts"}, map[string]any{"path": "docs/x.md"}, map[string]any{"path": "missing.ts"}, map[string]any{"path": "src/c.ts"}),
			reading(map[string]any{"path": "./src/a.ts"}, map[string]any{"path": `src\a.ts`}, map[string]any{"path": "a.ts"},
				map[string]any{"path": "src/b.ts", "start_line": 2, "end_line": 2}, map[string]any{"path": "src/gone.ts"}),
			calling("spawn", map[string]any{"task": brief, "owns": []string{"src/**"}}),
		}},
		change: func() error {
			return errors.Join(os.WriteFile(filepath.Join(root, "src", "a.ts"), []byte("ALPHA as it is now\n"), 0o644),
				os.Remove(filepath.Join(root, "src", "gone.ts")))
		},
	}
	base := turn.Config{
		Model:          script,
		Spend:          turn.SpendAPIKey,
		Tools:          turn.NewRegistry(memoised...),
		Caps:           turn.Caps{MaxSteps: 6},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawner := turn.NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	orchestrator := base
	orchestrator.Task, orchestrator.Tools, orchestrator.Inbox = "hand the work to a sub-agent", turn.NewRegistry(append([]turn.Tool{spawner}, memoised...)...), spawner.Inbox

	if err := turn.Lead(context.Background(), orchestrator, nil, nil, func(turn.Row, error) {}); err != nil {
		t.Fatalf("Lead: %v", err)
	}

	if len(script.asked) == 0 {
		t.Fatal("the sub-agent was never asked")
	}
	first := script.asked[0].Messages[0].Content
	for marker, want := range map[string]int{
		"ALPHA as it is now":    1,
		"ALPHA as the lead saw": 0,
		"BRAVO line one":        1,
		"CHARLIE":               1,
		"DELTA":                 1,
		"repaired:":             0,
		"XRAY":                  0,
		"GOLF":                  0,
		"cached:":               0,
	} {
		if got := strings.Count(first, marker); got != want {
			t.Errorf("%q appears %d times in the sub-agent's first message, want %d:\n%s", marker, got, want, first)
		}
	}
	if strings.Index(first, "ALPHA") < strings.Index(first, "CHARLIE") {
		t.Errorf("the file the brief names should come before the files the lead read:\n%s", first)
	}
}
