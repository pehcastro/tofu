package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tofu/internal/judge/state"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type seenByAll struct {
	mu       sync.Mutex
	lead     []llm.Decision
	subAgent []llm.Decision
	leadSaw  []string
	subSaw   []string
}

func (m *seenByAll) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	last := request.Messages[len(request.Messages)-1].Content
	queue, saw := &m.subAgent, &m.subSaw
	if SubAgentAsking(ctx) == "" {
		queue, saw = &m.lead, &m.leadSaw
	}
	*saw = append(*saw, last)
	if len(*queue) == 0 {
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "nothing more queued"}, nil
	}
	next := (*queue)[0]
	*queue = (*queue)[1:]
	return next, nil
}

func (m *seenByAll) asksReachingTheLead() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	asks := 0
	for _, saw := range m.leadSaw {
		asks += strings.Count(saw, " asks to run ")
	}
	return asks
}

type gateSeeing struct {
	mu   sync.Mutex
	seen []string
}

func (g *gateSeeing) Decide(ctx context.Context, request GateRequest) (GateDecision, error) {
	g.mu.Lock()
	g.seen = append(g.seen, SubAgentAsking(ctx)+":"+request.Tool)
	g.mu.Unlock()
	return asksForSubAgents{}.Decide(ctx, request)
}

func shellCall(id, command string) llm.Decision {
	raw, _ := json.Marshal(map[string]any{"command": command})
	return toolCallDecision(llm.ToolCall{ID: id, Name: "bash", Arguments: raw})
}

func messageCall(id string, args map[string]any) llm.Decision {
	raw, _ := json.Marshal(args)
	return toolCallDecision(llm.ToolCall{ID: id, Name: "message", Arguments: raw})
}

func crewOn(t *testing.T, model *seenByAll, gate Gate) Config {
	t.Helper()
	project, bash := hookedProject(t, `{}`)
	for _, folder := range []string{"mine", "yours"} {
		if err := os.MkdirAll(filepath.Join(project, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(bash), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096, Project: project,
		ArtifactDir: filepath.Join(project, "artifacts"), NewID: func() string { return "turn-lead" }, Gate: gate, GateMode: GateEnforce}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the notes to sub-agents", NewRegistry(bash, spawn), spawn.Inbox
	return lead
}

func TestAnAllowHereStandsForThatSubAgentAndThatKindOfCallOnlyAndNeedsNoProse(t *testing.T) {
	model := &seenByAll{
		lead: []llm.Decision{
			spawnCall("call-spawn-1", "write two notes under mine/", "mine/**"),
			messageCall("call-allow-here", map[string]any{"to": "sub-1", "answer": "allow_here"}),
			spawnCall("call-spawn-2", "write a note under yours/", "yours/**"),
			messageCall("call-allow", map[string]any{"to": "sub-2", "answer": "allow"}),
			claimDecision("both wrote their notes"),
		},
		subAgent: []llm.Decision{
			shellCall("s1-a", "echo hello > mine/a.txt"),
			shellCall("s1-b", "echo hello > mine/b.txt"),
			claimDecision("wrote mine/a.txt and mine/b.txt"),
			shellCall("s2-a", "echo hello > yours/a.txt"),
			claimDecision("wrote yours/a.txt"),
		},
	}
	lead := crewOn(t, model, asksForSubAgents{})
	startLead(t.Context(), lead, nil).wait(t)
	for _, written := range []string{"mine/a.txt", "mine/b.txt", "yours/a.txt"} {
		if _, err := os.Stat(filepath.Join(lead.Project, written)); err != nil {
			t.Errorf("%s was not written: %v", written, err)
		}
	}
	if asks := model.asksReachingTheLead(); asks != 2 {
		t.Errorf("%d asks reached the lead, want 2: one from sub-1, whose second call stood, and one from sub-2", asks)
	}
	for i, saw := range model.leadSaw {
		if strings.HasPrefix(saw, "sub-1's call is") || strings.HasPrefix(saw, "sub-2's call is") {
			t.Errorf("request %d asked the lead again after it answered, so the answer cost a prose turn:\n%s", i, saw)
		}
	}
	if !strings.Contains(strings.Join(model.leadSaw, "\n"), "allow_here") {
		t.Errorf("the ask never offered allow_here:\n%s", strings.Join(model.leadSaw, "\n---\n"))
	}
}

func TestAStandingAllowEndsWhenTheSubAgentEnds(t *testing.T) {
	model := &seenByAll{
		lead: []llm.Decision{
			spawnCall("call-spawn-1", "write a note under mine/", "mine/**"),
			messageCall("call-allow-here", map[string]any{"to": "sub-1", "answer": "allow_here"}),
			messageCall("call-more", map[string]any{"to": "sub-1", "text": "write one more"}),
			claimDecision("asked sub-1 for one more"),
			messageCall("call-allow", map[string]any{"to": "sub-1", "answer": "allow"}),
			claimDecision("sub-1 wrote both"),
		},
		subAgent: []llm.Decision{
			shellCall("s1-a", "echo hello > mine/a.txt"),
			claimDecision("wrote mine/a.txt"),
			shellCall("s1-b", "echo hello > mine/b.txt"),
			claimDecision("wrote mine/b.txt"),
		},
	}
	lead := crewOn(t, model, asksForSubAgents{})
	startLead(t.Context(), lead, nil).wait(t)
	if asks := model.asksReachingTheLead(); asks != 2 {
		t.Errorf("%d asks reached the lead, want 2: the standing allow ended with sub-1's run", asks)
	}
}

func TestACallTheReadListRefusesNeverReachesTheGateOrTheLead(t *testing.T) {
	model := &seenByAll{
		lead: []llm.Decision{
			spawnCall("call-spawn-1", "peek at a script", "mine/**"),
			claimDecision("sub-1 reported"),
		},
		subAgent: []llm.Decision{
			shellCall("s1-a", "node /tmp/peek.mjs"),
			claimDecision("node was refused"),
		},
	}
	gate := &gateSeeing{}
	lead := crewOn(t, model, gate)
	startLead(t.Context(), lead, nil).wait(t)
	for _, seen := range gate.seen {
		if seen == "sub-1:bash" {
			t.Errorf("jev was asked about a call the read list refuses: %v", gate.seen)
		}
	}
	if asks := model.asksReachingTheLead(); asks != 0 {
		t.Errorf("%d asks reached the lead for a call the read list refuses", asks)
	}
	if len(model.subSaw) < 2 || !strings.Contains(model.subSaw[1], "read list") {
		t.Errorf("the sub-agent did not read the read list's refusal: %q", model.subSaw)
	}
}

func TestAWriteToTheTempFolderReadsAsTempToJev(t *testing.T) {
	project := t.TempDir()
	for _, c := range []struct {
		command string
		want    state.TargetLocation
	}{
		{"cat > /tmp/peek.mjs", state.LocationTemp},
		{"cat > " + filepath.ToSlash(filepath.Join(os.TempDir(), "peek.mjs")), state.LocationTemp},
		{"cat > " + filepath.ToSlash(filepath.Join(project, "peek.mjs")), state.LocationInsideProject},
		{"cat > /etc/peek.mjs", state.LocationOutsideProject},
	} {
		got := state.TargetsOf(state.ToolGateInput{Tool: "bash", Input: map[string]any{"command": c.command}, Cwd: project, ProjectDir: project})
		if len(got.Targets) != 1 || got.Targets[0].Location != c.want {
			t.Errorf("%s reads as %+v, want one target at %s", c.command, got, c.want)
		}
	}
}
