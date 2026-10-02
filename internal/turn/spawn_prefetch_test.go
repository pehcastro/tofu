package turn_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type prefetchScript struct {
	mu       sync.Mutex
	brief    string
	lead     []llm.Decision
	subAgent []llm.Decision
	asked    []llm.Request
}

func (s *prefetchScript) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	script := &s.lead
	if strings.Contains(request.Messages[0].Content, s.brief) {
		script = &s.subAgent
		s.asked = append(s.asked, request)
	}
	if len(*script) == 0 {
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	next := (*script)[0]
	*script = (*script)[1:]
	return next, nil
}

func calling(name string, args any) llm.Decision {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{{ID: "call-" + name, Name: name, Arguments: raw}}}
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
	ledger := turn.NewReadLedger()
	read, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := tools.NewEdit(root)
	if err != nil {
		t.Fatal(err)
	}
	brief := "Read API_SPEC.md, then change src/db.ts. Leave ../secret.txt, big.txt and src/index.ts alone."
	script := &prefetchScript{brief: brief, lead: []llm.Decision{calling("spawn", map[string]any{"task": brief, "owns": []string{"src/**"}})}, subAgent: []llm.Decision{
		calling("edit", map[string]any{"path": "src/db.ts", "old_string": "open()", "new_string": "open('app.db')"}),
		{Build: "m1", Outcome: llm.OutcomeMessage, Content: "changed src/db.ts"},
	}}
	base := turn.Config{
		Model:          script,
		Spend:          turn.SpendAPIKey,
		Tools:          turn.NewRegistry(tools.NewMemo().Wrap([]turn.Tool{read.Reading(ledger), edit.Reading(ledger)})...),
		Caps:           turn.Caps{MaxSteps: 5},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(outside, "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawner := turn.NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	orchestrator := base
	orchestrator.Task, orchestrator.Tools, orchestrator.Inbox = "hand the work to a sub-agent", turn.NewRegistry(spawner), spawner.Inbox

	if err := turn.Lead(context.Background(), orchestrator, nil, nil, func(turn.Row, error) {}); err != nil {
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
	if answer := edited[len(edited)-1]; answer.Role != llm.RoleTool || strings.Contains(answer.Content, "has not been read") {
		t.Fatalf("the edit of src/db.ts was refused as unread: %q", answer.Content)
	}
	if body, _ := os.ReadFile(filepath.Join(root, "src", "db.ts")); !strings.Contains(string(body), "open('app.db')") {
		t.Fatalf("src/db.ts was not changed: %q", body)
	}
}
