package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type leadAndSubAgent struct {
	mu       sync.Mutex
	lead     []llm.Decision
	subAgent []llm.Decision
	leadSaw  []string
}

func (m *leadAndSubAgent) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	queue := &m.subAgent
	if SubAgentAsking(ctx) == "" {
		queue = &m.lead
		m.leadSaw = append(m.leadSaw, request.Messages[len(request.Messages)-1].Content)
	}
	if len(*queue) == 0 {
		return llm.Decision{}, errors.New("no more decisions queued")
	}
	next := (*queue)[0]
	*queue = (*queue)[1:]
	return next, nil
}

type asksForSubAgents struct{}

func (asksForSubAgents) Decide(ctx context.Context, request GateRequest) (GateDecision, error) {
	if SubAgentAsking(ctx) == "" {
		return GateDecision{ID: "lead-" + request.Tool, Verdict: ledger.VerdictAllow}, nil
	}
	return GateDecision{ID: "sub-" + request.Tool, Verdict: ledger.VerdictAsk,
		Reason: &ledger.Reason{Question: "risk", Comparison: "risk_ask_at", Value: 2.4, Threshold: 2}}, nil
}

func TestASubAgentsAskReachesTheLeadAndTheLeadsAllowRunsTheCall(t *testing.T) {
	root := t.TempDir()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	model := &leadAndSubAgent{
		lead: []llm.Decision{
			spawnCall("call-spawn", "write the note under mine/", "mine/**"),
			called("message", map[string]any{"to": "sub-1", "answer": "allow"}),
			claimDecision("allowed sub-1's write"),
			claimDecision("sub-1 wrote the note"),
		},
		subAgent: []llm.Decision{
			called("write", map[string]any{"path": "mine/note.txt", "content": "a note"}),
			claimDecision("wrote mine/note.txt"),
		},
	}
	personAsked := 0
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" },
		Gate: asksForSubAgents{}, GateMode: GateEnforce,
		Person: func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
			personAsked++
			return PersonDenied, nil
		}}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the note to a sub-agent", NewRegistry(write, spawn), spawn.Inbox
	startLead(t.Context(), lead, nil).wait(t)

	if personAsked != 0 {
		t.Errorf("the person was asked %d times for a sub-agent's call", personAsked)
	}
	if len(model.leadSaw) < 2 {
		t.Fatalf("the lead was asked %d times, want a second turn for the ask", len(model.leadSaw))
	}
	for _, want := range []string{"sub-1", "write", "mine/note.txt", "risk 2.40", "answer allow or deny"} {
		if !strings.Contains(model.leadSaw[1], want) {
			t.Errorf("the lead's second turn opens without %q:\n%s", want, model.leadSaw[1])
		}
	}
	if written, err := os.ReadFile(filepath.Join(root, "mine", "note.txt")); err != nil || string(written) != "a note" {
		t.Errorf("the lead allowed the write and the file holds %q: %v", written, err)
	}
}

func TestASubAgentsPreToolUseAskReachesTheLeadAndTheLeadsAllowRunsTheCall(t *testing.T) {
	project, bash := hookedProject(t, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"ask\",\"permissionDecisionReason\":\"a write under mine needs a yes\"}}'"}]}]}`)
	model := &leadAndSubAgent{
		lead: []llm.Decision{
			spawnCall("call-spawn", "note the run under mine/", "mine/**"),
			called("message", map[string]any{"to": "sub-1", "answer": "allow"}),
			claimDecision("allowed sub-1's bash"),
			claimDecision("sub-1 noted the run"),
		},
		subAgent: []llm.Decision{
			called("bash", map[string]any{"command": "mkdir -p mine && echo ran > mine/ran.txt"}),
			claimDecision("noted the run in mine/ran.txt"),
		},
	}
	var personAsked []string
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(bash), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096, Project: project,
		ArtifactDir: filepath.Join(project, "artifacts"), NewID: func() string { return "turn-lead" },
		Person: func(_ context.Context, request GateRequest, _ GateDecision) (PersonAnswer, error) {
			if request.Tool == "hooks" {
				return PersonAllowedOnce, nil
			}
			personAsked = append(personAsked, request.Tool)
			return PersonDenied, nil
		}}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the note to a sub-agent", NewRegistry(spawn), spawn.Inbox
	startLead(t.Context(), lead, nil).wait(t)

	if len(personAsked) != 0 {
		t.Errorf("the person was asked about %v for a sub-agent's call", personAsked)
	}
	if len(model.leadSaw) < 2 {
		t.Fatalf("the lead was asked %d times, want a second turn for the hook's ask", len(model.leadSaw))
	}
	for _, want := range []string{"sub-1", "bash", "a write under mine needs a yes", "answer allow or deny"} {
		if !strings.Contains(model.leadSaw[1], want) {
			t.Errorf("the lead's second turn opens without %q:\n%s", want, model.leadSaw[1])
		}
	}
	if written, err := os.ReadFile(filepath.Join(project, "mine", "ran.txt")); err != nil || strings.TrimSpace(string(written)) != "ran" {
		t.Errorf("the lead allowed the bash call and mine/ran.txt holds %q: %v", written, err)
	}
}
