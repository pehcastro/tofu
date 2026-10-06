package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/settings"
	"tofu/internal/subagent"
	"tofu/library"
)

type namedTool string

func (n namedTool) Name() string { return string(n) }

func (n namedTool) Definition() llm.Tool {
	return llm.Tool{Name: string(n), Parameters: map[string]any{"type": "object"}}
}

func (namedTool) Run(context.Context, json.RawMessage) (Result, error) { return Result{}, nil }

func TestASubAgentIsOfferedWriteAndEditOnlyWhenItsDefinitionNamesThem(t *testing.T) {
	root := t.TempDir()
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	base := []Tool{read, write, namedTool("edit"), namedTool("typecheck"), namedTool("test"), namedTool("browser_tabs"), namedTool("browser_observe"), namedTool("browser_act"), namedTool("browser_motion"), namedTool("fetch"), namedTool("glob"), namedTool("search"), namedTool("symbols"), namedTool("bash")}
	var names []string
	for _, tool := range base {
		names = append(names, tool.Name())
	}
	found := subagent.Definitions(subagent.Scan{Library: library.Files(), Tools: append(names, "spawn")})
	gates := map[string]string{"go-dev": "go vet, go test", "py-dev": "ruff check, pytest"}
	for _, definition := range found.Definitions {
		if gate, isLanguageAgent := gates[definition.Name]; isLanguageAgent && (len(definition.References) != 4 || len(definition.Cut) > 0 || strings.Join(definition.Gate, ", ") != gate) {
			t.Errorf("%s loads %d references, cuts %q, gates on %q; want four references, none cut, and the gate %s", definition.Name, len(definition.References), definition.Cut, definition.Gate, gate)
		}
	}
	for agent, want := range map[string]string{
		"browser":  "browser_tabs browser_observe browser_act browser_motion",
		"research": "read write fetch glob search symbols bash reference",
		"ts-dev":   "read write edit typecheck test glob search symbols bash reference",
		"rust-dev": "read write edit glob search symbols bash reference",
		"go-dev":   "read write edit glob search symbols bash reference",
		"py-dev":   "read write edit glob search symbols bash reference",
		"qa":       "read write typecheck test glob search symbols bash reference",
		"":         "read write edit typecheck test browser_tabs browser_observe browser_act browser_motion fetch glob search symbols bash spawn message subagents",
	} {
		model := &stubModel{decisions: []llm.Decision{claimDecision("did the piece")}}
		config := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(base...), Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096,
			ArtifactDir: t.TempDir(), NewID: func() string { return "turn-orchestrator" }}
		spawn := NewSpawnTool("turn-orchestrator", config, &subagent.Roster{})
		spawn.SubAgents = SubAgents{Defined: found.Definitions}
		if _, err := spawn.Run(context.Background(), json.RawMessage(`{"agent":"`+agent+`","task":"do the piece","owns":["piece/**"]}`)); err != nil {
			t.Fatal(err)
		}
		reported(t, spawn)
		var offered []string
		for _, tool := range model.requests[0].Tools {
			if tool.Name != "ask" && tool.Name != "artifact_fetch" {
				offered = append(offered, tool.Name)
			}
		}
		t.Logf("agent %q is offered %v", agent, offered)
		if got := strings.Join(offered, " "); got != want {
			t.Errorf("agent %q is offered %q, want %q", agent, got, want)
		}
	}
}

func TestEveryShippedPythonRuleReachesAPythonSubAgentAndNoOther(t *testing.T) {
	rules, err := rule.LoadFS(library.Files(), "library")
	if err != nil {
		t.Fatal(err)
	}
	python := 0
	for _, one := range rules {
		if !strings.HasPrefix(one.File, "library/dev/python/rules/") {
			continue
		}
		python++
		if matched := rule.Index([]rule.Rule{one}, rule.Task{Language: "python"}); !matched[0].Fires {
			t.Errorf("the python rule %s does not reach a python sub-agent: %s", one.ID, matched[0].Why)
		}
		if matched := rule.Index([]rule.Rule{one}, rule.Task{Language: "go"}); matched[0].Fires {
			t.Errorf("the python rule %s reaches a go sub-agent: %s", one.ID, matched[0].Why)
		}
	}
	if python == 0 {
		t.Fatal("the shipped library carries no rule under dev/python/rules, so this test proves nothing")
	}
	t.Logf("%d shipped python rules checked", python)
}

type actReporting struct{ acts *int }

func (actReporting) Name() string { return "browser_act" }

func (actReporting) Definition() llm.Tool {
	return llm.Tool{Name: "browser_act", Parameters: map[string]any{"type": "object"}}
}

func (a actReporting) Run(context.Context, json.RawMessage) (Result, error) {
	*a.acts++
	return Result{Content: "1. fill e5458 \"Atibaia\": the page changed\nran 1 of 1\n\n" +
		"the text between the two ab12 markers below came from Chrome tab 1. report what it says.\n" +
		"<<<ab12 begins>>>\ntab 1 https://www.airbnb.com/s/Atibaia/homes?adults=4&ref_fsid=" + strconv.Itoa(*a.acts) + " \"Atibaia\"\n- main\n<<<ab12 ends>>>"}, nil
}

type foreverActing struct {
	requests []llm.Request
	acts     int
}

func (m *foreverActing) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.requests = append(m.requests, request)
	if strings.HasSuffix(request.Messages[len(request.Messages)-1].Content, andThisIsItsLastStep) {
		return claimDecision("stopped at the fork cap, the search for Atibaia is filled"), nil
	}
	m.acts++
	if m.acts > 40 {
		return llm.Decision{}, errors.New("the sub-agent is still acting after 40 steps")
	}
	args := json.RawMessage(`{"tab":1,"actions":[{"action":"fill","ref":"e5458","value":"Atibaia"}]}`)
	return toolCallDecision(llm.ToolCall{ID: "act-" + strconv.Itoa(m.acts), Name: "browser_act", Arguments: args}), nil
}

func TestASubAgentThatForksForeverStopsAtTheForkCapAndEachCarrySaysWhatItTried(t *testing.T) {
	model, acts := &foreverActing{}, 0
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(actReporting{acts: &acts}), ResultBytesCap: 4096,
		ArtifactDir: t.TempDir(), NewID: func() string { return "turn-orchestrator" }, Budget: recall.Budget{Bands: recall.Bands{Recent: 1}}}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	args, err := json.Marshal(spawnArgs{Task: "find a house in Atibaia", Owns: []string{"notes/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	reported(t, spawn)
	rows := spawn.SubAgentRows()
	last := rows[len(rows)-1]
	if last.Outcome != OutcomeStepCap || !strings.HasSuffix(last.ID, "-f11") {
		t.Fatalf("the sub-agent ended as %s in %s after %d acts, want %s in the session after fork 10", last.Outcome, last.ID, model.acts, OutcomeStepCap)
	}
	var afterSecondFork string
	for _, request := range model.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "this is fork 2 of at most 10") {
				afterSecondFork = message.Content
			}
		}
	}
	if afterSecondFork == "" {
		t.Fatal("no request after a fork carries this is fork 2 of at most 10")
	}
	if !strings.Contains(afterSecondFork, "1. fill e5458 \"Atibaia\": the page changed\n2. fill e5458 \"Atibaia\": the page changed") {
		t.Errorf("the carry after fork 2 does not hold the act lines it tried:\n%s", afterSecondFork)
	}
}

type forksTwiceThenAnswers struct{ asked int }

func (m *forksTwiceThenAnswers) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.asked++
	if m.asked > 2 || strings.HasSuffix(request.Messages[len(request.Messages)-1].Content, andThisIsItsLastStep) {
		return claimDecision("found the house"), nil
	}
	args := json.RawMessage(`{"tab":1,"actions":[{"action":"fill","ref":"e5458","value":"Atibaia"}]}`)
	return toolCallDecision(
		llm.ToolCall{ID: "act-" + strconv.Itoa(m.asked) + "a", Name: "browser_act", Arguments: args},
		llm.ToolCall{ID: "act-" + strconv.Itoa(m.asked) + "b", Name: "browser_act", Arguments: args}), nil
}

func TestAForkedSubAgentReportsEveryForkUnderTheNameMessageReaches(t *testing.T) {
	acts := 0
	model := &forksTwiceThenAnswers{}
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(actReporting{acts: &acts}), ResultBytesCap: 4096,
		ArtifactDir: t.TempDir(), NewID: func() string { return "turn-orchestrator" }, Budget: recall.Budget{Bands: recall.Bands{Recent: 1}}}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	args, err := json.Marshal(spawnArgs{Task: "find a house in Atibaia", Owns: []string{"notes/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	report := reported(t, spawn)
	t.Logf("the hand-back:\n%s", report)
	if want := "sub-agent sub-1 is "; !strings.Contains(report, want) || strings.Contains(report, "sub-1-f") {
		t.Errorf("the hand-back does not name the agent as sub-1, or names a fork:\n%s", report)
	}
	if want := "after 3 steps and 4 tool calls across 2 forks"; !strings.Contains(report, want) {
		t.Errorf("the hand-back does not count every fork, want %q:\n%s", want, report)
	}
	if _, err := (messageTool{orchestrator: spawn}).Run(context.Background(), json.RawMessage(`{"to":"sub-1","text":"which listing?"}`)); err != nil {
		t.Fatal(err)
	}
	if answered := reported(t, spawn); !strings.Contains(answered, "sub-agent sub-1 is ") || model.asked != 4 {
		t.Errorf("a message to sub-1 did not reach the agent:\n%s", answered)
	}
}

const ordersRoute = "add the orders route"

func sessionWithTwoFinishedSubAgents(t *testing.T) (*session.Store, string) {
	t.Helper()
	users, err := json.Marshal(spawnArgs{Task: usersRoute, Owns: []string{"src/users.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	orders, err := json.Marshal(spawnArgs{Task: ordersRoute, Owns: []string{"src/orders.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	model := newCrew(map[string][]llm.Decision{
		leadKey: {
			toolCallDecision(llm.ToolCall{ID: "call-users", Name: "spawn", Arguments: users}, llm.ToolCall{ID: "call-orders", Name: "spawn", Arguments: orders}),
			claimDecision("one reported"), claimDecision("both reported"), claimDecision("done"),
		},
		usersRoute:  {claimDecision("the users route is added")},
		ordersRoute: {claimDecision("the orders route is added")},
	})
	store, id := session.NewStore(t.TempDir()), session.NewEventID()
	lead := crewLead(t, model)
	lead.Sessions, lead.Session = store, id
	startLead(context.Background(), lead, nil).wait(t)
	return store, id
}

func TestASessionReadBackAsAResumeListsBothItsFinishedSubAgents(t *testing.T) {
	store, id := sessionWithTwoFinishedSubAgents(t)
	resumed := &subagent.Roster{}
	if err := RestoreSubAgents(store, id, resumed, NewInbox()); err != nil {
		t.Fatal(err)
	}
	listed, err := subAgentsTool{orchestrator: NewSpawnTool("turn-lead", Config{}, resumed)}.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("subagents after the resume:\n%s", listed.Content)
	for _, want := range []string{"sub-1: no definition, finished, ran ", "sub-2: no definition, finished, ran "} {
		if !strings.Contains(listed.Content, want) {
			t.Errorf("the list after a resume does not say %q", want)
		}
	}
	for _, agent := range resumed.SubAgents() {
		if !strings.Contains(agent.Report, "sub-agent "+agent.ID+" is finished") || !slices.Contains([]string{usersRoute, ordersRoute}, agent.Brief) {
			t.Errorf("%s came back with the brief %q and the report %q, want its own brief and its own report", agent.ID, agent.Brief, agent.Report)
		}
	}
}

func TestEachPostedReportIsRecordedWithTheSubAgentsNameAndState(t *testing.T) {
	store, id := sessionWithTwoFinishedSubAgents(t)
	events, err := store.Events(id)
	if err != nil {
		t.Fatal(err)
	}
	var reported []string
	for _, event := range events {
		var report session.ReportBody
		if event.Kind == session.EventReport && json.Unmarshal(event.Body, &report) == nil &&
			report.State == "finished" && strings.Contains(report.Text, "sub-agent "+event.Agent+" is finished") {
			reported = append(reported, event.Agent)
		}
	}
	if slices.Sort(reported); !slices.Equal(reported, []string{"sub-1", "sub-2"}) {
		t.Fatalf("the session records finished reports for %v, want sub-1 and sub-2", reported)
	}
}

func restoredWithout(t *testing.T, store *session.Store, id string, dropped func(session.Event) bool) *subagent.Roster {
	t.Helper()
	header, err := store.Header(id)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(id)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := session.NewStore(t.TempDir())
	if err := rewritten.Write(header, slices.DeleteFunc(events, dropped)); err != nil {
		t.Fatal(err)
	}
	resumed := &subagent.Roster{}
	if err := RestoreSubAgents(rewritten, id, resumed, NewInbox()); err != nil {
		t.Fatal(err)
	}
	return resumed
}

func TestAResumeReadsEachReportFromItsOwnEventAndFromTheLeadsMessagesOnlyInAnOlderSession(t *testing.T) {
	store, id := sessionWithTwoFinishedSubAgents(t)
	for written, dropped := range map[string]func(session.Event) bool{
		"with report events and no report in the lead's messages": func(event session.Event) bool {
			var message session.MessageBody
			return event.Kind == session.EventMessage && json.Unmarshal(event.Body, &message) == nil && strings.Contains(message.Origin, sourceReport)
		},
		"before report events": func(event session.Event) bool { return event.Kind == session.EventReport },
	} {
		for _, agent := range restoredWithout(t, store, id, dropped).SubAgents() {
			if !strings.Contains(agent.Report, "sub-agent "+agent.ID+" is finished") {
				t.Errorf("a session written %s restores %s with the report %q", written, agent.ID, agent.Report)
			}
		}
	}
}

func TestAMessageAfterAResumeReachesAFinishedSubAgentWithItsConversation(t *testing.T) {
	store, id := sessionWithTwoFinishedSubAgents(t)
	resumed, inbox := &subagent.Roster{}, NewInbox()
	if err := RestoreSubAgents(store, id, resumed, inbox); err != nil {
		t.Fatal(err)
	}
	model := newCrew(map[string][]llm.Decision{usersRoute: {claimDecision("the health route is added")}})
	spawn := NewSpawnTool("turn-lead", Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(), Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096, ArtifactDir: t.TempDir()}, resumed)
	spawn.Inbox = inbox
	if _, err := (messageTool{orchestrator: spawn}).Run(context.Background(), json.RawMessage(`{"to":"sub-1","text":"also add /health"}`)); err != nil {
		t.Fatal(err)
	}
	if report := reported(t, spawn); !strings.Contains(report, "sub-agent sub-1 is finished") {
		t.Errorf("sub-1 did not report after the message:\n%s", report)
	}
	asked := model.requests(usersRoute)
	if len(asked) != 1 || !slices.ContainsFunc(asked[0].Messages, func(message llm.Message) bool { return message.Content == "the users route is added" }) {
		t.Fatalf("sub-1 was asked %d times, want once carrying what it said before the resume", len(asked))
	}
}

func reported(t *testing.T, spawn *SpawnTool) string {
	t.Helper()
	reports, _ := spawn.Inbox.next(context.Background(), nil)
	if len(reports) != 1 {
		t.Fatalf("the spawn's inbox holds %d reports, want 1", len(reports))
	}
	return reports[0].text
}

func TestTheLeadHasTheSpawnResultBeforeTheSubAgentsFirstRequestIsAnswered(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    {spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is done")},
		usersRoute: {claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 1)
	defer release()
	lead := crewLead(t, model)
	led := startLead(context.Background(), lead, nil)
	waitFor(t, "the lead's spawn turn to end", func() bool { return len(led.turns()) == 1 })
	held := led.turns()[0].Conversation
	if result := held[len(held)-2]; result.ToolCallID != "call-spawn" || !strings.HasPrefix(result.Content, "sub-1 is running") || model.answered(usersRoute) != 0 {
		t.Fatalf("the lead's spawn turn holds %q before its last line with the sub-agent answered %d times, want the spawn result while it runs",
			result.Content, model.answered(usersRoute))
	}
	release()
	led.wait(t)
}

func TestASpawnWithNoOwnsStartsAndItsWriteEditAndNestedSpawnAreRefused(t *testing.T) {
	names := []string{"read", "write", "edit", "typecheck", "test", "glob", "search", "symbols", "bash", "spawn"}
	found := subagent.Definitions(subagent.Scan{Library: library.Files(), Tools: names})
	for spawned, nested := range map[string]string{
		`{"agent":"ts-dev","task":"run ping and report"}`: `unknown tool "spawn"`,
		`{"task":"run ping and report","owns":[]}`:        "spawned without owns, so a sub-agent you spawn holds none either",
	} {
		root := t.TempDir()
		write, err := NewWriteTool(root)
		if err != nil {
			t.Fatal(err)
		}
		model := &stubModel{decisions: []llm.Decision{toolCallDecision(
			llm.ToolCall{ID: "w", Name: "write", Arguments: json.RawMessage(`{"path":"ping-run-1.txt","content":"placeholder"}`)},
			llm.ToolCall{ID: "e", Name: "edit", Arguments: json.RawMessage(`{"path":"ping-run-1.txt","old_string":"a","new_string":"b"}`)},
			llm.ToolCall{ID: "s", Name: "spawn", Arguments: json.RawMessage(`{"task":"write it for me","owns":["**"]}`)}),
			claimDecision("ping answered three times")}}
		base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write, namedTool("edit"), namedTool("bash")), ResultBytesCap: 4096,
			ArtifactDir: t.TempDir(), NewID: func() string { return "turn-orchestrator" }}
		spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
		spawn.SubAgents = SubAgents{Defined: found.Definitions}
		if _, err := spawn.Run(context.Background(), json.RawMessage(spawned)); err != nil {
			t.Fatalf("%s did not start: %v", spawned, err)
		}
		reported(t, spawn)
		if len(model.requests) != 2 {
			t.Fatalf("%s: the sub-agent asked %d times, want 2", spawned, len(model.requests))
		}
		said := map[string]string{}
		for _, message := range model.requests[1].Messages {
			if message.Role == llm.RoleTool {
				said[message.ToolCallID] = message.Content
			}
		}
		t.Logf("%s:\nwrite: %s\nedit: %s\nspawn: %s", spawned, said["w"], said["e"], said["s"])
		for _, call := range []string{"w", "e"} {
			if !strings.Contains(said[call], "spawned without owns, so you write nothing") {
				t.Errorf("%s: call %s was not refused as a sub-agent spawned without owns: %q", spawned, call, said[call])
			}
		}
		if !strings.Contains(said["s"], nested) {
			t.Errorf("%s: a nested spawn holding ** says %q, want %q", spawned, said["s"], nested)
		}
		if _, err := os.Stat(filepath.Join(root, "ping-run-1.txt")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: the placeholder file exists or could not be checked: %v", spawned, err)
		}
	}
}

type heldModel struct{ release chan struct{} }

func (m heldModel) Ask(ctx context.Context, _ llm.Request) (llm.Decision, error) {
	select {
	case <-m.release:
		return claimDecision("done"), nil
	case <-ctx.Done():
		return llm.Decision{}, ctx.Err()
	}
}

func TestTheSpawnPastTheSettingIsRefusedWhileTheOthersRunAndStartsOnceTheyEnd(t *testing.T) {
	const running = 15
	_, spawn := orchestratorTurn(t, t.TempDir(), nil)
	model := heldModel{release: make(chan struct{})}
	spawn.base.Model = model
	spawn.Limits = func() SubAgentLimits { return SubAgentLimits{Running: running, Depth: 2} }
	if described := spawn.Definition().Description; !strings.Contains(described, "At most 15 sub-agents running at once") || !strings.Contains(described, settings.SubAgentsPerTurn) {
		t.Errorf("the model is not told the setting's number and name: %q", described)
	}
	spawnNumber := func(n int) error {
		args, err := json.Marshal(spawnArgs{Task: "piece " + strconv.Itoa(n), Owns: []string{"piece" + strconv.Itoa(n) + "/**"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = spawn.Run(context.Background(), args)
		return err
	}
	for n := 1; n <= running; n++ {
		if err := spawnNumber(n); err != nil {
			t.Fatalf("spawn %d of %d was refused: %v", n, running, err)
		}
	}
	refused := spawnNumber(running + 1)
	if refused == nil || !strings.Contains(refused.Error(), settings.SubAgentsPerTurn) || !strings.Contains(refused.Error(), "15") {
		t.Fatalf("spawn 16 with 15 running and the setting at 15 was not refused naming the setting: %v", refused)
	}
	close(model.release)
	for ended := 0; ended < running; {
		reports, _ := spawn.Inbox.next(context.Background(), nil)
		if len(reports) == 0 {
			t.Fatalf("nothing runs and %d of %d reports came", ended, running)
		}
		ended += len(reports)
	}
	if err := spawnNumber(running + 2); err != nil {
		t.Fatalf("a spawn after the 15 ended was refused: %v", err)
	}
	reported(t, spawn)
}
