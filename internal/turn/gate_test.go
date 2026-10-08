package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
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

func TestASubAgentsAskCarriesItsArgumentsCutToTheCapAndNamesTheCall(t *testing.T) {
	root := t.TempDir()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("0123456789", 9000)
	args, err := json.Marshal(map[string]string{"path": "mine/big.txt", "content": big})
	if err != nil {
		t.Fatal(err)
	}
	model := &leadAndSubAgent{
		lead: []llm.Decision{
			spawnCall("call-spawn", "write the big file under mine/", "mine/**"),
			called("message", map[string]any{"to": "sub-1", "answer": "allow"}),
			claimDecision("allowed sub-1's write"),
			claimDecision("sub-1 wrote it"),
		},
		subAgent: []llm.Decision{
			toolCallDecision(llm.ToolCall{ID: "call-big", Name: "write", Arguments: args}),
			claimDecision("wrote mine/big.txt"),
		},
	}
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" },
		Gate: asksForSubAgents{}, GateMode: GateEnforce}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the big file to a sub-agent", NewRegistry(write, spawn), spawn.Inbox
	startLead(t.Context(), lead, nil).wait(t)

	if len(model.leadSaw) < 2 {
		t.Fatalf("the lead was asked %d times, want a second turn for the ask", len(model.leadSaw))
	}
	asked := model.leadSaw[1]
	if len(asked) > konst.GateAskArgsBytes+1024 {
		t.Errorf("the ask the lead saw is %d bytes for a %d byte call", len(asked), len(args))
	}
	for _, want := range []string{"mine/big.txt", "lookup", "call-big"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the ask lacks %q:\n%s", want, asked)
		}
	}
	if written, err := os.ReadFile(filepath.Join(root, "mine", "big.txt")); err != nil || len(written) != len(big) {
		t.Errorf("the allowed write left %d bytes, want %d: %v", len(written), len(big), err)
	}
}

type asksWithReason struct{ reason bool }

func (g asksWithReason) Decide(_ context.Context, request GateRequest) (GateDecision, error) {
	decided := GateDecision{ID: "row-" + request.Tool, Verdict: ledger.VerdictAsk}
	if g.reason {
		decided.Reason = &ledger.Reason{Question: "risk", Comparison: "risk_ask_at", Value: 1.77, Threshold: 1.5}
	}
	return decided, nil
}

func TestAnAskThatRunsNamesWhatAllowedIt(t *testing.T) {
	answering := func(answer PersonAnswer) Person {
		return func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) { return answer, nil }
	}
	for _, case_ := range []struct {
		name    string
		person  Person
		reason  bool
		refused bool
		want    string
	}{
		{"gatePrompt auto runs it unasked", answering(PersonDenied).RunsWhatJevAsks(), true, false, allowedInAutoMode},
		{"the person allows it", answering(PersonAllowedOnce), true, false, allowedByThePerson},
		{"the person allows it here for good", answering(PersonAlwaysHere), true, false, allowedByThePerson},
		{"the person refuses it", answering(PersonDenied), true, true, ""},
		{"auto mode with no reason to carry it", answering(PersonDenied).RunsWhatJevAsks(), false, false, ""},
	} {
		t.Run(case_.name, func(t *testing.T) {
			root := t.TempDir()
			write, err := NewWriteTool(root)
			if err != nil {
				t.Fatal(err)
			}
			model := &leadAndSubAgent{lead: []llm.Decision{called("write", map[string]any{"path": "note.txt", "content": "a note"}), claimDecision("wrote note.txt")}}
			row, err := Run(t.Context(), Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write), Caps: Caps{MaxSteps: 4}, ResultBytesCap: 4096,
				ArtifactDir: filepath.Join(root, "artifacts"), Gate: asksWithReason{reason: case_.reason}, GateMode: GateEnforce, Person: case_.person, Task: "write a note"})
			if err != nil {
				t.Fatal(err)
			}
			call := row.Steps[0].ToolCalls[0]
			if call.Refused != case_.refused {
				t.Fatalf("refused is %v, want %v: %+v", call.Refused, case_.refused, call)
			}
			allowedBy := ""
			if call.GateReason != nil {
				allowedBy = call.GateReason.AllowedBy
			}
			if allowedBy != case_.want {
				t.Errorf("the recorded reason says allowed by %q, want %q", allowedBy, case_.want)
			}
		})
	}
}

func TestReadAndBackupOfARunningSubAgentCarryItsConversationSoFar(t *testing.T) {
	const task = "read the note and say what it holds"
	model := newCrew(map[string][]llm.Decision{
		leadKey: {spawnCall("call-spawn", task, "notes/**"), claimDecision("sub-1 is on it"), claimDecision("sub-1 is done")},
		task:    {called("read", map[string]any{"path": "note.txt"}), claimDecision("the note says hello")},
	})
	release := model.hold(task, 2)
	defer release()
	lead := crewLead(t, model)
	lead.Sessions, lead.Session = session.NewStore(t.TempDir()), session.NewEventID()
	spawn := lead.Tools.byName["spawn"].(*SpawnTool)
	spawn.base.Sessions, spawn.base.Session = lead.Sessions, lead.Session
	led := startLead(t.Context(), lead, nil)
	asked := func() int {
		model.mu.Lock()
		defer model.mu.Unlock()
		return len(model.asked[task])
	}
	for deadline := time.Now().Add(5 * time.Second); asked() < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the sub-agent never reached its second request")
		}
	}
	read, err := spawn.read("sub-1")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := spawn.backup("sub-1")
	if err != nil {
		t.Fatal(err)
	}
	release()
	led.wait(t)
	for _, want := range []string{"its conversation, 3 messages", task, "note.txt"} {
		if !strings.Contains(read.Content, want) {
			t.Errorf("read on the running sub-agent lacks %q:\n%s", want, read.Content)
		}
	}
	if !strings.Contains(backup.Content, ": 3 messages") {
		t.Errorf("backup of the running sub-agent says %q, want its 3 messages so far", backup.Content)
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
