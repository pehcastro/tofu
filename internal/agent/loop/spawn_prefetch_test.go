package loop_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit"
	"tofu/internal/tools/registry"
)

type prefetchScript struct {
	mu       sync.Mutex
	brief    string
	lead     []core.Decision
	subAgent []core.Decision
	asked    []core.Request
}

func (s *prefetchScript) Ask(_ context.Context, request core.Request) (core.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	script := &s.lead
	opening := request.Messages[slices.IndexFunc(request.Messages, func(message core.Message) bool { return message.Origin.Source != core.SourceMemoryView })]
	if strings.Contains(opening.Content, s.brief) {
		script = &s.subAgent
		s.asked = append(s.asked, request)
	}
	if len(*script) == 0 {
		return core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: "done"}, nil
	}
	next := (*script)[0]
	*script = (*script)[1:]
	return next, nil
}

func calling(name string, args any) core.Decision {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return core.Decision{Build: "m1", Outcome: core.OutcomeToolCalls, ToolCalls: []core.ToolCall{{ID: "call-" + name, Name: name, Arguments: raw}}}
}

func TestABriefNamingFilesStartsTheSubAgentHoldingThemAndItsEditIsNotRefused(t *testing.T) {
	outside := t.TempDir()
	root := filepath.Join(outside, "project")
	files := map[string]string{
		filepath.Join(outside, "secret.txt"):   "outside the project",
		filepath.Join(root, "API_SPEC.md"):     "GET /users returns every user",
		filepath.Join(root, "src", "db.ts"):    "export const db = open()\n",
		filepath.Join(root, "big.txt"):         strings.Repeat("x", konst.SubAgentReferenceBytes),
		filepath.Join(root, "src", "index.ts"): "import { db } from './db'\n",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ledger := loop.NewReadLedger()
	read, err := loop.NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := edit.NewEdit(root)
	if err != nil {
		t.Fatal(err)
	}
	brief := "Read API_SPEC.md, then change src/db.ts. Leave ../secret.txt, big.txt and src/index.ts alone."
	script := &prefetchScript{brief: brief, lead: []core.Decision{calling("spawn", map[string]any{"task": brief, "owns": []string{"src/**"}})}, subAgent: []core.Decision{
		calling("edit", map[string]any{"path": "src/db.ts", "old_string": "open()", "new_string": "open('app.db')"}),
		{Build: "m1", Outcome: core.OutcomeMessage, Content: "changed src/db.ts"},
	}}
	base := loop.Config{
		Model:          script,
		Spend:          loop.SpendAPIKey,
		Tools:          loop.NewRegistry(registry.NewMemo().Wrap([]contract.Tool{read.Reading(ledger), edit.Reading(ledger)})...),
		Caps:           loop.Caps{MaxSteps: 5},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(outside, "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawner := loop.NewSpawnTool("turn-orchestrator", base, &subagents.Roster{})
	orchestrator := base
	orchestrator.Task, orchestrator.Tools, orchestrator.Inbox = "hand the work to a sub-agent", loop.NewRegistry(spawner), spawner.Inbox

	if err := loop.Lead(context.Background(), orchestrator, nil, nil, func(loop.Row, error) {}); err != nil {
		t.Fatalf("Lead: %v", err)
	}

	if len(script.asked) != 2 {
		t.Fatalf("the sub-agent was asked %d times, want 2", len(script.asked))
	}
	first := script.asked[0].Messages[0].Content
	for _, want := range []string{"GET /users returns every user", "export const db = open()"} {
		if !strings.Contains(first, want) {
			t.Errorf("the sub-agent's first message does not hold %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "outside the project") {
		t.Errorf("a file outside the project reached the first message:\n%s", first)
	}
	if strings.Contains(first, "xxxxxxxx") || !strings.Contains(first, "big.txt, src/index.ts") {
		t.Errorf("big.txt passes the budget and src/index.ts comes after it, so both should be named as skipped and neither shown:\n%s", first)
	}
	if !strings.HasSuffix(first, brief) {
		t.Errorf("the brief is not the end of the first message, so the files reached the task the gate reads as the person's words:\n%s", first)
	}
	edited := script.asked[1].Messages
	if answer := edited[len(edited)-1]; answer.Role != core.RoleTool || strings.Contains(answer.Content, "has not been read") {
		t.Fatalf("the edit of src/db.ts was refused as unread: %q", answer.Content)
	}
	if body, _ := os.ReadFile(filepath.Join(root, "src", "db.ts")); !strings.Contains(string(body), "open('app.db')") {
		t.Fatalf("src/db.ts was not changed: %q", body)
	}
}
