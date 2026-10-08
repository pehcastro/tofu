package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tuisession "tofu/interface/tui/session"
	"tofu/internal/cron"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func projectSessions(t *testing.T, dir string) *session.Store {
	t.Helper()
	state, err := sys.ProjectStateDirAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	return session.OpenAt(state)
}

func buildTestRunTools(dir, set string) ([]turn.Tool, error) {
	shell, err := turn.ResolveRunShell("")
	if err != nil {
		return nil, err
	}
	built, _, err := buildRunToolsForRun(dir, set, nil, nil, shell)
	return built, err
}

func TestARealRunOffersSymbolsAndTheOffArmDoesNot(t *testing.T) {
	full := toolNames(t, armOpts(t))
	if !slices.Contains(full, "symbols") {
		t.Fatalf("a real run cannot ask where an identifier is declared: %v", full)
	}
	if three := toolNames(t, armOpts(t, "--tools", toolSetThree)); slices.Contains(three, "symbols") {
		t.Fatalf("the off arm must stay the three tools the recorded runs had: %v", three)
	}
}

type deletingModel struct {
	decisions []llm.Decision
	deletes   string
	asked     int
}

func (m *deletingModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if m.asked == 1 {
		if err := os.Remove(m.deletes); err != nil {
			return llm.Decision{}, err
		}
	}
	m.asked++
	return m.decisions[m.asked-1], nil
}

func TestASecondIdenticalReadInOneTurnNeverReachesTheTool(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	readable := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(readable, []byte("the note as it was written once"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := armOpts(t)
	opts.dir, opts.task = dir, "read the note twice"
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	read := func(id string) llm.Decision {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
		}}
	}
	model := &deletingModel{deletes: readable, decisions: []llm.Decision{
		read("call-1"),
		read("call-2"),
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it twice"},
	}}

	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(row.Steps) < 2 || len(row.Steps[1].ToolCalls) != 1 {
		t.Fatalf("the turn ran %d steps, want two reads and an answer", len(row.Steps))
	}
	second := row.Steps[1].ToolCalls[0]
	if second.Error != "" {
		t.Fatalf("the second read reached the tool and the file was gone by then: %s", second.Error)
	}
	var answers []string
	for _, message := range row.Conversation {
		if message.Role == llm.RoleTool {
			answers = append(answers, message.Content)
		}
	}
	if len(answers) != 2 || strings.Contains(answers[1], "\n") || !strings.Contains(answers[1], "call-1") {
		t.Fatalf("the second answer is not one line pointing at the first read, call-1: %q", answers)
	}
	t.Logf("the file was deleted between the two reads and the second answered %d bytes: %q", second.ResultBytes, answers[1])
}

func ranTool(t *testing.T, dir, name, args string) turn.Result {
	t.Helper()
	built, err := buildTestRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	for _, tool := range built {
		if tool.Name() != name {
			continue
		}
		result, err := tool.Run(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", name, args, err)
		}
		return result
	}
	t.Fatalf("no tool named %s was built", name)
	return turn.Result{}
}

func TestTheWriteAndEditARunBuildsAreCheckedByTheWarmTscItIsGiven(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"tsconfig.json": `{"compilerOptions":{"strict":true}}`, "package.json": `{"name":"warm"}`, "bun.lock": "{}"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	warm := newWarmProcesses(nil)
	t.Cleanup(warm.Close)
	shell, err := turn.ResolveRunShell("")
	if err != nil {
		t.Fatal(err)
	}
	built, _, err := buildRunToolsForRun(dir, toolSetFull, nil, warm, shell)
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]turn.Tool{}
	for _, tool := range built {
		named[tool.Name()] = tool
	}
	for _, step := range []struct{ tool, args, want string }{
		{"write", `{"path":"count.ts","content":"const count: number = \"many\";\n"}`, "count.ts(1,7): error TS2322"},
		{"edit", `{"path":"count.ts","old_string":"\"many\"","new_string":"3"}`, "errors in count.ts: 0"},
	} {
		result, err := named[step.tool].Run(context.Background(), json.RawMessage(step.args))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Content, step.want) || !strings.Contains(result.Content, "--watch") {
			t.Fatalf("%s was not typechecked by the watching tsc it was given:\n%s", step.tool, result.Content)
		}
	}
}

func TestASessionInATsconfigDirectoryStartsItsWatcherBeforeAnyToolCall(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	shell, err := turn.ResolveRunShell("")
	if err != nil {
		t.Fatal(err)
	}
	for name, start := range map[string]func(t *testing.T, dir string){
		"run": func(t *testing.T, dir string) {
			warm := newWarmProcesses(nil)
			t.Cleanup(warm.Close)
			if _, _, err := buildRunToolsForRun(dir, toolSetFull, nil, warm, shell); err != nil {
				t.Fatal(err)
			}
		},
		"app": func(t *testing.T, dir string) {
			t.Cleanup(newAppSession(dir, nil, nil, time.Now, sessionResume{}).engine.warm.Close)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for file, body := range map[string]string{
				"tsconfig.json": `{"compilerOptions":{"strict":true,"incremental":true,"tsBuildInfoFile":"warm.tsbuildinfo"}}`,
				"package.json":  `{"name":"warm"}`, "bun.lock": "{}", "count.ts": "export const count = 1;\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			start(t, dir)
			if !checkedWithin(filepath.Join(dir, "warm.tsbuildinfo"), konst.TypecheckDeadlineMillis*time.Millisecond) {
				t.Fatal("no tsc checked the project before any tool was called")
			}
		})
	}
}

func checkedWithin(buildInfo string, limit time.Duration) bool {
	for waited := time.Duration(0); waited < limit; waited += 100 * time.Millisecond {
		if _, err := os.Stat(buildInfo); err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func TestASessionInAWorkspaceRootWarmsItsLargestPackagesUpToTheCap(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	warmed := `{"compilerOptions":{"strict":true,"incremental":true,"tsBuildInfoFile":"warm.tsbuildinfo"}}`
	dir := t.TempDir()
	files := map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true,"incremental":true,"tsBuildInfoFile":"warm.tsbuildinfo"},"include":["*.ts"]}`,
		"package.json":  `{"name":"root","workspaces":["packages/*","tools/none/*"]}`, "bun.lock": "{}", "root.ts": "export const root = 1;\n",
	}
	var packages []string
	for rank := range konst.TypecheckWarmPackages + 1 {
		name := "packages/p" + strconv.Itoa(rank)
		packages = append(packages, name)
		files[name+"/tsconfig.json"] = warmed
		for file := range konst.TypecheckWarmPackages + 1 - rank {
			files[name+"/src/f"+strconv.Itoa(file)+".ts"] = "export const f" + strconv.Itoa(file) + " = 1;\n"
		}
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	shell, err := turn.ResolveRunShell("")
	if err != nil {
		t.Fatal(err)
	}
	warm := newWarmProcesses(nil)
	t.Cleanup(warm.Close)
	if _, _, err := buildRunToolsForRun(dir, toolSetFull, nil, warm, shell); err != nil {
		t.Fatal(err)
	}
	for _, project := range append([]string{"."}, packages[:konst.TypecheckWarmPackages]...) {
		if !checkedWithin(filepath.Join(dir, project, "warm.tsbuildinfo"), konst.TypecheckDeadlineMillis*time.Millisecond) {
			t.Fatalf("%s was not checked before any tool was called", project)
		}
	}
	smallest := packages[konst.TypecheckWarmPackages]
	if checkedWithin(filepath.Join(dir, smallest, "warm.tsbuildinfo"), 3*time.Second) {
		t.Fatalf("%s, the smallest package past the cap of %d, was warmed too", smallest, konst.TypecheckWarmPackages)
	}
}

type effortServer struct {
	mutex   sync.Mutex
	bodies  []string
	replies []string
}

func (s *effortServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mutex.Lock()
	s.bodies = append(s.bodies, string(body))
	reply := `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}` + "\n" +
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}` + "\n" +
		`{"type":"content_block_stop","index":0}` + "\n" + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`
	if len(s.replies) > 0 {
		reply, s.replies = s.replies[0], s.replies[1:]
	}
	s.mutex.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	events := append([]string{`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`}, strings.Split(reply, "\n")...)
	for _, event := range append(events, `{"type":"message_stop"}`) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
	}
}

func TestASpawnedSubAgentsRequestsCarryTheEffortTheSpawnNames(t *testing.T) {
	for _, spawned := range []struct{ effort, child string }{
		{"low", `"output_config":{"effort":"low"}`},
		{"", `"output_config":{"effort":"medium"}`},
		{"minimal", ""},
	} {
		t.Run(cmp.Or(spawned.effort, "inherited"), func(t *testing.T) {
			emptyHome(t)
			args, _ := json.Marshal(map[string]any{"task": "add the users route", "owns": []string{"src/**"}, "effort": spawned.effort})
			spawnCall := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"` + anthropic.EncodeToolName("spawn", true) + `","input":{}}}` + "\n" +
				`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":` + strconv.Quote(string(args)) + `}}` + "\n" +
				`{"type":"content_block_stop","index":0}` + "\n" + `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`
			server := &effortServer{replies: []string{spawnCall}}
			listening := httptest.NewServer(server)
			t.Cleanup(listening.Close)
			orchestrator := models.Model{Subscription: "claude-sub", ID: "claude-test", Efforts: anthropic.ReasoningEfforts()}
			subscription := func(effort llm.Effort) turn.Subscription {
				wire, err := anthropic.New(anthropic.Config{BaseURL: listening.URL, Model: "claude-test", Proxy: true,
					Token: func(context.Context, string) (string, error) { return "sk-ant-oat01-test", nil }})
				if err != nil {
					t.Fatal(err)
				}
				return turn.Subscription{Wire: wire, Effort: effort}
			}
			open := func(opts runOpts) (appWire, error) {
				return appWire{held: &accounts{fixed: subscription(opts.effort), now: time.Now}, spend: turn.SpendSubscription, selected: orchestrator}, nil
			}
			dir := t.TempDir()
			opener := runtime{orchestrator: orchestrator, open: open}.subAgentOpener(runOpts{dir: dir, wire: wireSubscription, model: orchestrator.Slug(), effort: llm.EffortMedium})
			base := turn.Config{Model: subscription(llm.EffortMedium), Spend: turn.SpendSubscription, Caps: turn.Caps{MaxSteps: 5}, ResultBytesCap: 4096,
				ArtifactDir: t.TempDir(), NewID: func() string { return "turn-lead" }, Task: "hand the users route to a sub-agent"}
			spawner := turn.NewSpawnTool("turn-lead", base, &subagent.Roster{})
			spawner.SubAgents.Open = opener
			lead := base
			lead.Tools, lead.Inbox = turn.NewRegistry(spawner), spawner.Inbox
			leadRows(t, lead)
			t.Logf("%d requests reached the wire", len(server.bodies))
			subAgentAsked := ""
			for index, body := range server.bodies {
				t.Logf("request %d effort: %s", index, regexp.MustCompile(`"output_config":\{[^}]*\}`).FindString(body))
				if strings.Contains(body, "the paths you hold") {
					subAgentAsked = body
				}
			}
			if spawned.child == "" {
				if len(server.bodies) != 2 || subAgentAsked != "" || !strings.Contains(server.bodies[1], "it takes low, medium, high, xhigh, max") {
					t.Fatalf("a level the model does not take was not refused at the spawn, before any sub-agent request: %d requests", len(server.bodies))
				}
				return
			}
			if !strings.Contains(server.bodies[0], `"output_config":{"effort":"medium"}`) || !strings.Contains(subAgentAsked, spawned.child) {
				t.Fatalf("the sub-agent's request did not carry %s", spawned.child)
			}
		})
	}
}

func TestATsDevSpawnIsOfferedTypecheck(t *testing.T) {
	reply := func(text string) llm.Decision {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: text}
	}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"agent":"ts-dev","task":"check the project","owns":["src/**"]}`)}}},
		reply("ts-dev is on it"), reply("done"),
	}, subAgents: []llm.Decision{reply("checked")}}
	stubbedTurn(scratchProject(t), model)(t.Context(), onTheSubscription, "have ts-dev check the project", driveAppOn(t, nil).emit)
	if len(model.subAgentAsked) == 0 {
		t.Fatal("the ts-dev sub-agent never asked its model")
	}
	var offered []string
	for _, tool := range model.subAgentAsked[0].Tools {
		offered = append(offered, tool.Name)
	}
	t.Logf("ts-dev is offered %v", offered)
	if !slices.Contains(offered, "typecheck") {
		t.Fatal("the ts-dev sub-agent was not offered typecheck")
	}
}

func TestABashResultCarryingAFabricatedCitationReachesTheModelRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := ranTool(t, dir, "bash", `{"command":"echo the fix is in real.go:900 and in missing.go:1"}`)
	if !strings.Contains(result.Content, "citations: 2 found, 0 resolved, 2 refused") {
		t.Fatalf("a bash result carrying two made up citations reached the model unchecked:\n%s", result.Content)
	}
	for _, want := range []string{"refused real.go:900", "refused missing.go:1"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the verdict does not name %q:\n%s", want, result.Content)
		}
	}
	t.Logf("%s", result.Content)
}

func TestASearchResultGetsNoCitationVerdictBecauseThisBinaryWroteIt(t *testing.T) {
	dir := t.TempDir()
	var lines strings.Builder
	for range 200 {
		lines.WriteString("wanted := 1\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "many.go"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	result := ranTool(t, dir, "search", `{"pattern":"wanted"}`)
	if !strings.Contains(result.Content, "many.go:1-200") {
		t.Fatalf("search did not frame the 200 written lines as one unit, so the count proves nothing:\n%s", result.Content)
	}
	if strings.Contains(result.Content, "citations:") {
		t.Fatalf("a search result carries a citation verdict on output this binary generated:\n%s", result.Content)
	}
	t.Logf("search returned %d bytes over 200 matches and no citation verdict", len(result.Content))
}

func TestTheContextCeilingFlagWinsOverTheEnvironmentVariable(t *testing.T) {
	t.Setenv(recall.CeilingVariable, "90000")
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--context-ceiling", "20000", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	budget, err := contextBudget(opts, chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	if budget.CeilingTokens != 20000 || !budget.Automatic {
		t.Fatalf("the flag did not set the ceiling: %+v", budget)
	}
	if !strings.Contains(budget.Source, recall.CeilingVariable) {
		t.Fatalf("the record does not say which of the two won: %q", budget.Source)
	}

	variableAlone, err := contextBudget(armOpts(t), chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget with the variable alone: %v", err)
	}
	if variableAlone.CeilingTokens != 90000 {
		t.Fatalf("the variable stopped working when the flag arrived: %+v", variableAlone)
	}
	t.Logf("flag: %s\nvariable: %s", budget.Record(), variableAlone.Record())
}

func TestARunAtATwentyThousandCeilingCompactsAndTheRecordSaysWhatItDropped(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts, err := parseRunArgs([]string{"--dir", dir, "--context-ceiling", "20000", "--no-subagents", "read the big file over and over"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("x", 20000)), 0o600); err != nil {
		t.Fatal(err)
	}
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	var decisions []llm.Decision
	for i := range 8 {
		decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-" + string(rune('a'+i)), Name: "bash",
				Arguments: json.RawMessage(`{"command":"echo ` + strconv.Itoa(i) + " " + strings.Repeat("y", 6000) + `"}`)},
		}})
	}
	decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it"})

	budget, err := contextBudget(opts, chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	config, _ := mustConfig(t, opts, built, runtime{model: &queuedModel{decisions: decisions}, spend: turn.SpendSubscription, budget: budget})
	config.NoFork = true
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	dropped := 0
	for _, step := range row.Steps {
		if step.Compaction == nil {
			continue
		}
		for _, drop := range step.Compaction.Drops {
			dropped++
			if drop.Handle == "" || drop.Bytes == 0 || drop.Reason == "" {
				t.Fatalf("a drop says %+v, and a record of what was dropped names the tool, the bytes, the reason and the handle it is kept under", drop)
			}
			t.Logf("step %d dropped the %s result of %d bytes as %s, kept at %s",
				step.Compaction.Step, drop.Tool, drop.Bytes, drop.Reason, drop.Handle)
		}
	}
	if dropped == 0 {
		t.Fatalf("a %d token ceiling compacted nothing across %d steps", budget.CeilingTokens, len(row.Steps))
	}
}

func TestRunHelpKeepsTheCeilingFlagWorkingAndDoesNotOfferIt(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--help"}, &out, &errOut); code != exitOK {
		t.Fatalf("tofu run --help exited %d: %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "ceiling") {
		t.Fatalf("the help offers a development flag to whoever reads it:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--dir") {
		t.Fatalf("the help does not name the one required argument:\n%s", out.String())
	}

	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--context-ceiling", "20000", "a task"})
	if err != nil {
		t.Fatalf("the flag the help no longer names stopped working: %v", err)
	}
	budget, err := contextBudget(opts, chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	if budget.CeilingTokens != 20000 {
		t.Fatalf("the flag set a %d token ceiling, want the 20000 it was given", budget.CeilingTokens)
	}
	t.Logf("%s", out.String())
}

func TestARequestOverTheModelsWindowIsRefusedBeforeItIsSent(t *testing.T) {
	budget, err := recall.BudgetFor("a model with a small window", 1000)
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	queued := &queuedModel{decisions: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "sent"}}}
	guard, err := guarded(queued, budget)
	if err != nil {
		t.Fatalf("guarded: %v", err)
	}

	_, err = guard.Ask(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleUser, Content: strings.Repeat("x", 10000000)},
	}})
	var over *recall.OverWindow
	if !errors.As(err, &over) {
		t.Fatalf("a request far past the window came back as %v, want a typed refusal", err)
	}
	if over.WindowTokens != 1000 || over.RequestTokens <= 1000 {
		t.Fatalf("the refusal says %+v, and it has to name the window and the size of the request", over)
	}
	if len(queued.decisions) != 1 {
		t.Fatal("the model was asked anyway, so the request reached the wire")
	}

	decision, err := guard.Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "small"}}})
	if err != nil || decision.Content != "sent" {
		t.Fatalf("a request inside the window was not sent: %v %+v", err, decision)
	}
	t.Logf("refused: %v", over)
}

func TestAModelWithNoRecordedWindowCompactsAtTheOperatingCeiling(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts, err := parseRunArgs([]string{"--dir", dir, "--no-subagents", "read every part of the dump"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	var decisions []llm.Decision
	for i := range 10 {
		name := "part-" + strconv.Itoa(i) + ".txt"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat(strings.Repeat("x", 99)+"\n", 200)), 0o600); err != nil {
			t.Fatal(err)
		}
		decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-" + strconv.Itoa(i), Name: "bash", Arguments: json.RawMessage(`{"command":"cat ` + name + `"}`)},
		}})
	}
	decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it all"})
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}

	budget, err := contextBudget(opts, models.Model{ID: "a model no library records"})
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	if budget.WindowTokens != 0 || budget.CeilingTokens != konst.ContextCeilingTokens {
		t.Fatalf("a model nobody has a window for got %+v, want no window and the %d token ceiling tofu operates under",
			budget, konst.ContextCeilingTokens)
	}
	store := projectSessions(t, dir)
	config, _ := mustConfig(t, opts, built, runtime{model: &queuedModel{decisions: decisions}, spend: turn.SpendSubscription, budget: budget, sessions: store})
	config.NoFork = true
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}

	dropped := 0
	for _, step := range row.Steps {
		if step.Compaction != nil {
			dropped += len(step.Compaction.Drops)
		}
	}
	if dropped == 0 {
		t.Fatalf("a model with no recorded window compacted nothing across %d steps against a %d token target",
			len(row.Steps), budget.Bands.Target())
	}
	header, err := store.Header(row.ID)
	if err != nil {
		t.Fatalf("reading the record back: %v", err)
	}
	if header.ContextCeiling != konst.ContextCeilingTokens || !strings.Contains(header.AutoCompaction, "no context window is recorded") {
		t.Fatalf("the record says ceiling %d and %q", header.ContextCeiling, header.AutoCompaction)
	}
	t.Logf("%d results dropped over %d steps, and the record reads: %s", dropped, len(row.Steps), header.AutoCompaction)
}

func TestTheSpawnToolIsGivenTheSessionStoreBeforeTheTurnStarts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts := armOpts(t)
	opts.dir, opts.task = dir, "hand the work to a sub-agent"
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	store := projectSessions(t, dir)
	model := oneNoteSubAgent()

	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription, sessions: store})
	rows := leadRows(t, config)
	var named []string
	for _, row := range rows {
		named = append(named, row.SubAgentIDs...)
	}
	if len(named) != 1 {
		t.Fatalf("the orchestrator's turns name %v, want the one sub-agent", named)
	}
	subAgentID, row := named[0], rows[0]
	header, err := store.Header(row.Session)
	if err != nil {
		t.Fatalf("the orchestrator session was not recorded: %v", err)
	}
	if len(header.Agents) != 1 || header.Agents[0].Agent != subAgentID || header.Agents[0].SpawnTurn != row.ID || header.Agents[0].SpawnCall != "call-1" {
		t.Fatalf("the session indexes %+v, want %s spawned by call-1 in %s", header.Agents, subAgentID, row.ID)
	}
	events, err := store.Body(row.Session + session.TurnMark + subAgentID)
	if err != nil {
		t.Fatalf("reading the sub-agent's events from the orchestrator's log: %v", err)
	}
	steps := 0
	for _, event := range events {
		if event.Kind == session.EventStep {
			steps++
		}
	}
	if steps != 2 {
		t.Fatalf("the sub-agent record carries %d steps, want the write and the answer once each", steps)
	}
	t.Logf("sub-agent %s recorded %d steps and %d events under a store the spawn tool had before the turn began", subAgentID, steps, len(events))
}

func TestWithNoSearchKeyStoredTheTurnIsGivenFetchAndNoWebSearch(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	opts := armOpts(t)
	t.Chdir(root)
	names := toolNames(t, opts)
	if !slices.Contains(names, "fetch") {
		t.Errorf("a turn is given %v, and fetch is not among them", names)
	}
	if slices.Contains(names, "web_search") {
		t.Errorf("web_search is offered with no provider key stored: %v", names)
	}
	t.Logf("the tools a turn is given: %v", names)
}

func TestAProjectCarryingNoWebLibraryGetsFetchFromTheShippedOne(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	built, err := buildTestRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("a project carrying no web library could not build its tools: %v", err)
	}
	var names []string
	for _, tool := range built {
		names = append(names, tool.Name())
	}
	t.Logf("away from the repository root a turn is given: %v", names)
	if !slices.Contains(names, "fetch") {
		t.Errorf("the shipped web library did not reach a project of its own: %v", names)
	}
	if slices.Contains(names, "web_search") {
		t.Errorf("web_search is offered with no provider key stored: %v", names)
	}
}

func oneNoteSubAgent() *queuedModel {
	return &queuedModel{
		decisions: []llm.Decision{
			{Build: "stub-model", Outcome: llm.OutcomeToolCalls, Content: "the sub-agent is on it", ToolCalls: []llm.ToolCall{
				{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)},
			}},
			{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the sub-agent reported"},
		},
		subAgents: []llm.Decision{
			{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
				{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"a note"}`)},
			}},
			{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the sub-agent wrote it"},
		},
	}
}

type watchingModel struct {
	inner     turn.Model
	store     *session.Store
	subAgent  string
	mu        sync.Mutex
	leadFirst *bool
	found     []bool
}

func (m *watchingModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	_, err := m.store.Header(m.subAgent)
	m.mu.Lock()
	switch {
	case turn.SubAgentAsking(ctx) != "":
		m.found = append(m.found, err == nil)
	case m.leadFirst == nil:
		onDisk := err == nil
		m.leadFirst = &onDisk
	}
	m.mu.Unlock()
	return m.inner.Ask(ctx, request)
}

func TestASubAgentLeavesItsRecordWhileTheOrchestratorsTurnIsStillRunning(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts := armOpts(t)
	opts.dir, opts.task, opts.turnID = dir, "hand the work to a sub-agent", "turn-orchestrator"
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	store := projectSessions(t, dir)
	model := &watchingModel{store: store, subAgent: "sub-1", inner: oneNoteSubAgent()}

	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription, sessions: store})
	leadRows(t, config)
	if model.leadFirst == nil || *model.leadFirst {
		t.Errorf("the sub-agent's record was on disk before the orchestrator spawned it")
	}
	if !slices.Equal(model.found, []bool{true, true}) {
		t.Errorf("the sub-agent's record was on disk %v at its asks, want it there for both of its two", model.found)
	}
	t.Logf("the sub-agent's record was on disk at its asks %v", model.found)
}

func TestTheWebLibraryComesFromTheDirectoryTheRunNamesNotTheOneItWasStartedIn(t *testing.T) {
	opts, plain := armOpts(t), t.TempDir()
	off := filepath.Join(opts.dir, ".tofu", "web")
	if err := os.MkdirAll(off, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(off, "fetch.yaml"), []byte("use: off\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(plain)
	if named := toolNames(t, opts); slices.Contains(named, "fetch") {
		t.Errorf("--dir %s says use: off and the run offered fetch anyway: %v", opts.dir, named)
	}

	t.Chdir(opts.dir)
	opts.dir = plain
	if standing := toolNames(t, opts); !slices.Contains(standing, "fetch") {
		t.Errorf("--dir %s has no web library and the run obeyed the use: off of the directory it stood in: %v", plain, standing)
	}
}

func armOpts(t *testing.T, args ...string) runOpts {
	t.Helper()
	opts, err := parseRunArgs(append([]string{"--dir", t.TempDir()}, append(args, "a task")...))
	if err != nil {
		t.Fatalf("parseRunArgs %v: %v", args, err)
	}
	return opts
}

func mustConfig(t *testing.T, opts runOpts, built []turn.Tool, run runtime) (turn.Config, *turn.SpawnTool) {
	t.Helper()
	config, spawner, err := runConfig(opts, built, run)
	if err != nil {
		t.Fatalf("runConfig: %v", err)
	}
	return config, spawner
}

func toolNames(t *testing.T, opts runOpts) []string {
	t.Helper()
	built, err := buildTestRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools %s: %v", opts.toolSet, err)
	}
	config, _ := mustConfig(t, opts, built, runtime{spend: turn.SpendSubscription})
	var named []string
	for _, definition := range config.Tools.Definitions() {
		named = append(named, definition.Name)
	}
	return named
}

func chosenFor(t *testing.T) models.Model {
	t.Helper()
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	selected, err := chooseModel(opts)
	if err != nil {
		t.Fatalf("chooseModel returned an error: %v", err)
	}
	return selected
}

func leadRows(t *testing.T, config turn.Config) []turn.Row {
	t.Helper()
	var rows []turn.Row
	if err := turn.Lead(context.Background(), config, nil, nil, func(row turn.Row, _ error) { rows = append(rows, row) }); err != nil {
		t.Fatalf("turn.Lead: %v", err)
	}
	return rows
}

type queuedModel struct {
	mu            sync.Mutex
	decisions     []llm.Decision
	subAgents     []llm.Decision
	subAgentAsked []llm.Request
}

func (m *queuedModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	queue := &m.decisions
	if turn.SubAgentAsking(ctx) != "" {
		queue = &m.subAgents
		m.subAgentAsked = append(m.subAgentAsked, request)
	}
	if len(*queue) == 0 {
		return llm.Decision{}, errors.New("queuedModel: no more decisions queued")
	}
	next := (*queue)[0]
	*queue = (*queue)[1:]
	return next, nil
}

type leadRecording struct {
	*queuedModel
	leadAsked []llm.Request
}

func (m *leadRecording) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if turn.SubAgentAsking(ctx) == "" {
		m.mu.Lock()
		m.leadAsked = append(m.leadAsked, request)
		m.mu.Unlock()
	}
	return m.queuedModel.Ask(ctx, request)
}

func TestOnAReactProjectTheLeadCarriesTheDesignRulesAndNoWritingRuleAndItsTsDevCarriesBoth(t *testing.T) {
	dir := scratchProject(t)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies": {"react": "19"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const task = "make the save button in src/components/Dialog.tsx keep its label while it saves"
	reply := func(text string) llm.Decision {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: text}
	}
	model := &leadRecording{queuedModel: &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"agent":"ts-dev","task":"` + task + `","owns":["src/**"]}`)}}},
		reply("ts-dev is on it"), reply("done"),
	}, subAgents: []llm.Decision{reply("saved")}}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, task, driveAppOn(t, nil).emit)
	if len(model.leadAsked) == 0 || len(model.subAgentAsked) == 0 {
		t.Fatalf("the lead asked %d times and the sub-agent %d, and both must ask once", len(model.leadAsked), len(model.subAgentAsked))
	}
	sent := func(request llm.Request) (prompt string, tools []string) {
		for _, message := range request.Messages {
			prompt += message.Content + "\n"
		}
		for _, tool := range request.Tools {
			tools = append(tools, tool.Name)
		}
		return prompt, tools
	}
	lead, leadTools := sent(model.leadAsked[0])
	for _, writing := range []string{"from the rule react_events_not_effects]", "from the rule ts_handled_promise]", "from the rule fe_motion_properties]"} {
		if strings.Contains(lead, writing) {
			t.Errorf("the lead carries %q, which says how code is written", writing)
		}
	}
	for _, design := range []string{"[code_rules, from the rule fe_consequential_actions]", "[code_rules, from the rule fe_control_states]", "[code_rules, from the rule fe_four_states]"} {
		if !strings.Contains(lead, design) {
			t.Errorf("the lead lost %q, which says what to build and so what to brief", design)
		}
	}
	if !strings.Contains(lead, "from the rule verify_scoped]") {
		t.Error("the lead lost the process rule verify_scoped along with the code rules")
	}
	if !strings.Contains(lead, "from the rule verify_sub_agents]") {
		t.Error("the lead lost verify_sub_agents, which ships on and no setting turned off")
	}
	for _, unused := range leadNeverCalls() {
		if slices.Contains(leadTools, unused) {
			t.Errorf("the lead is offered %s, which no recorded lead ever called: %v", unused, leadTools)
		}
	}
	if !slices.Contains(leadTools, "spawn") {
		t.Errorf("the lead lost spawn: %v", leadTools)
	}
	writer, _ := sent(model.subAgentAsked[0])
	for _, code := range []string{"[code_rules, from the rule fe_control_states]", "[code_rules, from the rule fe_dialog]", "[code_rules, from the rule react_events_not_effects]", "[code_rules, from the rule ts_handled_promise]"} {
		if !strings.Contains(writer, code) {
			t.Errorf("the ts-dev sub-agent writing the code lost %q", code)
		}
	}

	opts, err := parseRunArgs([]string{"--dir", dir, "--no-subagents", task})
	if err != nil {
		t.Fatal(err)
	}
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatal(err)
	}
	alone, _ := mustConfig(t, opts, built, runtime{spend: turn.SpendSubscription})
	if !strings.Contains(alone.SystemMessage()+alone.FirstUserMessage(), "[code_rules, from the rule fe_control_states]") {
		t.Error("a run with no sub-agents writes the code itself and lost the code rules")
	}
	var aloneTools []string
	for _, definition := range alone.Tools.Definitions() {
		aloneTools = append(aloneTools, definition.Name)
	}
	if !slices.Contains(aloneTools, "github_pr_diff") {
		t.Errorf("a run with no sub-agents has no lead and lost a tool only the lead goes without: %v", aloneTools)
	}
}

func TestTheLeadEndsOnACleanSingleReportWhetherOrNotItChecksSubAgents(t *testing.T) {
	spawn := `{"text":"spawning go-dev to add Extra","tools":[{"name":"spawn","args":{"agent":"go-dev","task":"add func Extra to package stats in stats/extra.go","owns":["stats/**"]}}]}`
	gated := []string{
		`{"agent":"c1","tools":[{"name":"write","args":{"path":"stats/extra.go","content":"package stats\n\nfunc Extra() int { return 1 }\n"}}]}`,
		`{"agent":"c1","tools":[{"name":"bash","args":{"command":"go vet ./stats/"}}]}`,
		`{"agent":"c1","tools":[{"name":"bash","args":{"command":"go test ./stats/"}}]}`,
		`{"agent":"c1","text":"VERIFIED. go vet and go test passed after the edit."}`,
	}
	leadChecks := []string{`{"tools":[{"name":"bash","args":{"command":"go vet ./stats/"}}]}`, `{"text":"go-dev added stats.Extra."}`}
	ran := `step 1: tool_call tool=bash command="[^"]*go vet \./stats/" sub_agent="" exit_code=0`
	refused := `step 1: tool_call tool=bash command="" .*was not executed: this turn ends on a clean sub-agent report`
	concerns := append([]string{`{"agent":"c1","tools":[{"name":"read","args":{"path":"stats/missing.go"}}]}`}, gated...)
	for _, run := range []struct {
		name     string
		settings string
		agent    []string
		checked  string
	}{
		{"clean", `{"verifySubAgents": 1}`, gated, refused},
		{"done_with_concerns", `{"verifySubAgents": 1}`, concerns, ran},
		{"clean_with_the_check_off", `{"verifySubAgents": 0}`, gated, refused},
		{"done_with_concerns_with_the_check_off", `{"verifySubAgents": 0}`, concerns, ran},
	} {
		t.Run(run.name, func(t *testing.T) {
			home, dir := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			for name, body := range map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n", "stats/stats.go": "package stats\n\nfunc Count() int { return 0 }\n", ".tofu/settings.json": run.settings} {
				if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			deck := filepath.Join(home, "deck.jsonl")
			if err := os.WriteFile(deck, []byte(strings.Join(slices.Concat([]string{spawn}, run.agent, leadChecks), "\n")), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv(cassetteVariable, deck)
			var out, errOut bytes.Buffer
			if code := runVerb([]string{"--dir", dir, "--no-gate", "--sift", "free", "add Extra to package stats"}, &out, &errOut); code != exitOK {
				t.Fatalf("tofu run exited %d: %s", code, errOut.String())
			}
			t.Logf("tofu run printed:\n%s", out.String())
			turns := regexp.MustCompile(`(?m)^turn turn-\S+ outcome`).FindAllStringIndex(out.String(), -1)
			if len(turns) != 2 {
				t.Fatalf("the lead ran %d turns, want 2: the spawn, then the report", len(turns))
			}
			second, _, _ := strings.Cut(out.String()[turns[1][0]:], "\nturn go-dev-1 ")
			if !regexp.MustCompile(run.checked).MatchString(second) {
				t.Errorf("the lead's report turn, want its first call to match %s:\n%s", run.checked, second)
			}
		})
	}
}

const overrideAsked = "no_unit_test_after_code"

const ruleOverrideCassette = `{"text":"a rule stops me","tools":[{"name":"rule_override","args":{"rule":"no_unit_test_after_code","reason_from_rule":"tests written after the code agree with it","why_now":"you asked for unit tests on the parser","change":"off"}}]}
{"text":"the answer is in"}
`

func TestTheLeadAsksWhereToOverrideARuleAndOnlyAYesWritesTheFile(t *testing.T) {
	for _, answer := range []struct {
		key             string
		project, global bool
	}{
		{"1", true, false},
		{"2", false, true},
		{"3", false, false},
	} {
		t.Run(answer.key, func(t *testing.T) {
			dir, home := drivenProject(t), t.TempDir()
			script := written(t, dir, "override.drive", strings.Join([]string{
				"wait " + tuisession.Placeholder,
				"type add unit tests to the parser",
				"key enter",
				"wait [3] no",
				"screen",
				"key " + answer.key,
				"wait cooked for",
			}, "\n"))
			deck := written(t, dir, "override.cassette", ruleOverrideCassette)
			var out, errOut bytes.Buffer
			if code := driveVerb([]string{script, "--cassette", deck, "--home", home, "--plain", "--timeout", "30s", "--gate", "off"}, strings.NewReader(""), &out, &errOut); code != exitOK {
				t.Fatalf("tofu drive exited %d: %s\n%s", code, errOut.String(), out.String())
			}
			for _, want := range []string{overrideAsked, "ordering", "[1] this project"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the asking screen never shows %q:\n%s", want, out.String())
				}
			}
			for _, layer := range []struct {
				dir   string
				wrote bool
			}{
				{filepath.Join(dir, ".tofu", "rules"), answer.project},
				{filepath.Join(home, ".tofu", "rules"), answer.global},
			} {
				data, err := os.ReadFile(filepath.Join(layer.dir, overrideAsked+"@1.yaml"))
				if layer.wrote != (err == nil) {
					t.Fatalf("key %s: %s holds the override %v, want %v", answer.key, layer.dir, err == nil, layer.wrote)
				}
				if layer.wrote && !strings.Contains(string(data), "by: asked") {
					t.Errorf("key %s wrote an override not marked by: asked:\n%s", answer.key, data)
				}
			}
		})
	}
}

func TestAnOffOverrideLeavesOneLineInThePromptAndAStaleOneLeavesTheRule(t *testing.T) {
	for _, version := range []int{1, 2} {
		project := chdirTemp(t)
		data, err := rule.OverrideFile(overrideAsked, "", rule.Override{Of: overrideAsked, Version: version, Reason: "the SDK's unit tests are its contract", By: rule.ByAsked, At: "2026-10-05"})
		if err != nil {
			t.Fatal(err)
		}
		if err := sys.WriteFile(filepath.Join(project, ".tofu", "rules", overrideAsked+"@1.yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		prompt, err := composeRun(runOpts{dir: project, task: "add unit tests to the parser"}, nil, runtime{open: openAppWire})
		if err != nil {
			t.Fatal(err)
		}
		system := prompt.composed.System()
		said := strings.Count(system, "the SDK's unit tests are its contract")
		if version == 1 && (said != 1 || strings.Contains(system, "NEVER write a unit test")) {
			t.Errorf("an applied off override says so %d times and the rule text is there %v, want one line and no rule:\n%s", said, strings.Contains(system, "NEVER write a unit test"), system)
		}
		if version == 2 && said != 0 {
			t.Errorf("a stale override is said to be in force while the rule still runs:\n%s", system)
		}
	}
}

func TestTheLeadAloneHoldsTheCronToolAndAFiredTurnNeverPushes(t *testing.T) {
	for _, arms := range [][]string{nil, {"--no-subagents"}, {"--tools", toolSetThree}} {
		opts := armOpts(t, arms...)
		built, err := buildTestRunTools(opts.dir, opts.toolSet)
		if err != nil {
			t.Fatal(err)
		}
		config, _ := mustConfig(t, opts, built, runtime{spend: turn.SpendSubscription, cron: &cron.Book{}, now: time.Now, notify: func(string) {}})
		var lead []string
		for _, definition := range config.Tools.Definitions() {
			lead = append(lead, definition.Name)
		}
		if !slices.Contains(lead, tools.CronToolName) {
			t.Errorf("arms %v: the lead is not given the cron tool: %v", arms, lead)
		}
		if slices.ContainsFunc(built, func(tool turn.Tool) bool { return tool.Name() == tools.CronToolName }) {
			t.Errorf("arms %v: the cron tool is in the set a sub-agent is given", arms)
		}
		bash := withoutPush(built)[slices.IndexFunc(built, func(tool turn.Tool) bool { return tool.Name() == "bash" })]
		for command, refused := range map[string]bool{"git push origin main": true, "git -C . push --force": true, "git status": false} {
			raw, _ := json.Marshal(map[string]string{"command": command})
			_, err := bash.Run(t.Context(), raw)
			if pushed := err != nil && strings.Contains(err.Error(), "never pushes"); pushed != refused {
				t.Errorf("arms %v: %q on a fired turn: refused %v, want %v (%v)", arms, command, pushed, refused, err)
			}
		}
	}
}

func TestEveryLeadThatCanAskAPersonHoldsRuleOverrideAndTheThreeToolArmAloneStaysThree(t *testing.T) {
	for _, arm := range []struct {
		args   []string
		person bool
		holds  bool
	}{
		{nil, false, true},
		{nil, true, true},
		{[]string{"--no-subagents"}, false, true},
		{[]string{"--no-subagents"}, true, true},
		{[]string{"--tools", toolSetThree}, false, false},
		{[]string{"--tools", toolSetThree}, true, true},
	} {
		opts := armOpts(t, arm.args...)
		opts.gateArm = gateOff
		built, err := buildTestRunTools(opts.dir, opts.toolSet)
		if err != nil {
			t.Fatal(err)
		}
		asked := 0
		var person turn.Person
		if arm.person {
			person = func(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
				asked++
				return turn.PersonDenied, nil
			}
		}
		call := llm.ToolCall{ID: "call-1", Name: turn.RuleOverrideToolName, Arguments: json.RawMessage(`{"rule":"tool_pick","why_now":"the person asked for find","change":"off"}`)}
		done := llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "worked within tool_pick"}
		model := &queuedModel{decisions: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}}, done, done, done}}
		config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription, leadAsks: person})
		held := 0
		for _, definition := range config.Tools.Definitions() {
			if definition.Name == turn.RuleOverrideToolName {
				held++
			}
		}
		if held > 1 || (held == 1) != arm.holds {
			t.Errorf("arms %v, a person %v: the lead holds %d rule_override, want it %v", arm.args, arm.person, held, arm.holds)
			continue
		}
		if !arm.holds {
			continue
		}
		leadRows(t, config)
		if asked > 1 || arm.person != (asked == 1) {
			t.Errorf("arms %v, a person %v: rule_override asked the person %d times", arm.args, arm.person, asked)
		}
	}
}
