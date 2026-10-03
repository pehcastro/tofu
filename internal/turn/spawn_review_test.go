package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

type scriptedCheck string

func (s scriptedCheck) Name() string { return string(s) }

func (s scriptedCheck) Definition() llm.Tool {
	return llm.Tool{Name: string(s), Parameters: map[string]any{"type": "object"}}
}

func (s scriptedCheck) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command string `json:"command"`
		Exit    int    `json:"exit"`
		Fail    string `json:"fail"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	result := Result{Content: "ran", Command: args.Command, FailureText: args.Fail}
	if s == "bash" {
		result.ExitCode = &args.Exit
	}
	return result, nil
}

func called(tool string, args map[string]any) llm.Decision {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: tool, Name: tool, Arguments: raw})
}

func edited(path string) llm.Decision { return called("edit", map[string]any{"path": path}) }

func bashed(command string, exit int) llm.Decision {
	return called("bash", map[string]any{"command": command, "exit": exit})
}

type gated struct {
	rows   []Row
	report string
	events []session.Event
	header session.Header
	seen   []subagent.SubAgent
	spawn  *SpawnTool
	ctx    context.Context
	store  *session.Store
	log    *session.Log
}

func (g *gated) collect(t *testing.T) {
	t.Helper()
	g.report, g.rows, g.header = reported(t, g.spawn), g.spawn.SubAgentRows(), g.log.Header()
	var err error
	if g.events, err = g.store.Events(g.log.ID()); err != nil {
		t.Fatal(err)
	}
}

func agentsByKind(t *testing.T, events []session.Event) map[session.EventKind][]string {
	t.Helper()
	agents := map[session.EventKind][]string{}
	for _, event := range events {
		if event.Kind == session.EventSpawn {
			var body session.SpawnBody
			if err := json.Unmarshal(event.Body, &body); err != nil {
				t.Fatal(err)
			}
			event.Agent = body.Agent
		}
		agents[event.Kind] = append(agents[event.Kind], event.Agent)
	}
	return agents
}

func gatedRun(t *testing.T, project string, definition subagent.Definition, decisions ...llm.Decision) *gated {
	t.Helper()
	root := t.TempDir()
	definition.Runs, definition.Description = subagent.RunsInherit, "a fixture"
	store := session.NewStore(filepath.Join(root, "sessions"))
	log, err := store.Open(session.Header{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	run := &gated{store: store, log: log}
	roster := &subagent.Roster{}
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(scriptedCheck("edit"), scriptedCheck("bash"), scriptedCheck("typecheck"), scriptedCheck("test")),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
		Step:           func(StepRow) { run.seen = append(run.seen, roster.SubAgents()...) },
	}
	run.spawn = NewSpawnTool("turn-orchestrator", base, roster)
	run.spawn.SubAgents.Defined, run.spawn.Project = []subagent.Definition{definition}, project
	args, err := json.Marshal(spawnArgs{Task: "change the crate", Owns: []string{"src/**", "README.md", "Cargo.toml"}, Agent: definition.Name})
	if err != nil {
		t.Fatal(err)
	}
	run.ctx = context.WithValue(context.Background(), spawnSiteKey{}, spawnSite{log: log, turn: "turn-orchestrator", agent: "turn-orchestrator", call: "call-spawn"})
	if _, err := run.spawn.Run(run.ctx, args); err != nil {
		t.Fatalf("spawn returned an error: %v", err)
	}
	run.collect(t)
	return run
}

var rustDev = subagent.Definition{Name: "rust-dev", Language: "rust", Gate: []string{"cargo clippy", "cargo test"}}

func reopenedFor(t *testing.T, rows []Row, want int, named ...string) {
	t.Helper()
	if len(rows) != want {
		t.Fatalf("%d rounds, want %d", len(rows), want)
	}
	if want == 1 {
		return
	}
	for _, check := range named {
		if !strings.Contains(rows[1].Task, check) {
			t.Fatalf("the reopen does not name %q: %q", check, rows[1].Task)
		}
	}
	t.Log(rows[1].Task)
}

func TestARustDevThatRanOnlyFmtIsReopenedNamingClippyAndTest(t *testing.T) {
	rows := gatedRun(t, "", rustDev,
		edited("src/lib.rs"), bashed("cargo fmt", 0), claimDecision("done"),
		bashed("cargo clippy --all-targets -- -D warnings", 0), bashed("cargo test -p parser", 0), claimDecision("done")).rows
	reopenedFor(t, rows, 2, "cargo clippy did not run", "cargo test did not run")
}

func TestARustDevThatRanClippyAndTestAfterItsLastEditIsAccepted(t *testing.T) {
	rows := gatedRun(t, "", rustDev,
		bashed("cargo test", 0), edited("src/lib.rs"), bashed("cargo clippy -p parser", 0), bashed("cd crates/parser && cargo test status", 0), claimDecision("done")).rows
	reopenedFor(t, rows, 1)
}

func TestARustDevWhoseClippyExits101IsReopenedAndARoundOfClippyAloneThenPasses(t *testing.T) {
	rows := gatedRun(t, "", rustDev,
		edited("src/lib.rs"), bashed("cargo clippy", 101), bashed("cargo test", 0), claimDecision("done"),
		bashed("cargo clippy", 0), claimDecision("done")).rows
	reopenedFor(t, rows, 2, "cargo clippy exited 101")
	if strings.Contains(rows[1].Task, "cargo test did not run") {
		t.Fatalf("cargo test passed after the last edit and is still named: %q", rows[1].Task)
	}
}

func TestAGateRunBeforeTheLastEditDoesNotCount(t *testing.T) {
	rows := gatedRun(t, "", rustDev,
		edited("src/lib.rs"), bashed("cargo clippy", 0), bashed("cargo test", 0), edited("src/main.rs"), claimDecision("done"),
		bashed("cargo clippy", 0), bashed("cargo test", 0), claimDecision("done")).rows
	reopenedFor(t, rows, 2, "cargo clippy did not run", "cargo test did not run")
}

func TestASubAgentWithNoGateOrNoChangeInItsLanguageIsUnaffected(t *testing.T) {
	for name, run := range map[string]*gated{
		"no gate":                 gatedRun(t, "", subagent.Definition{Name: "rust-dev", Language: "rust"}, edited("src/lib.rs"), bashed("cargo fmt", 0), claimDecision("done")),
		"only markdown and toml":  gatedRun(t, "", rustDev, edited("README.md"), edited("Cargo.toml"), claimDecision("done")),
		"a refused edit":          gatedRun(t, "", rustDev, edited("elsewhere/lib.rs"), claimDecision("done")),
		"no language on the file": gatedRun(t, "", subagent.Definition{Name: "rust-dev", Gate: rustDev.Gate}, edited("src/lib.rs"), claimDecision("done")),
	} {
		if rows := run.rows; len(rows) != 1 {
			t.Errorf("%s: %d rounds, want 1: %q", name, len(rows), rows[len(rows)-1].Task)
		}
	}
}

func TestATsDevGateNamesToolsAndAShellLookalikeDoesNotCount(t *testing.T) {
	rows := gatedRun(t, "", tsDev,
		edited("src/users.ts"), bashed("bun test", 0), bashed("test -f src/users.ts", 0), called("typecheck", map[string]any{"fail": "TS2322"}), claimDecision("done"),
		called("typecheck", map[string]any{}), called("test", map[string]any{}), claimDecision("done")).rows
	reopenedFor(t, rows, 2, "typecheck failed", "test did not run")
}

func TestASubAgentSentBackOnceByItsGateIsOneSubAgentInTheLogTheRosterAndTheReport(t *testing.T) {
	run := gatedRun(t, "", rustDev,
		edited("src/lib.rs"), bashed("cargo clippy", 0), claimDecision("done"),
		bashed("cargo test", 0), claimDecision("done"))
	reopenedFor(t, run.rows, 2, "cargo test did not run")
	agents := agentsByKind(t, run.events)
	if spawned := agents[session.EventSpawn]; !slices.Equal(spawned, []string{"rust-dev-1"}) {
		t.Errorf("the log holds spawn events for %q, want one, for rust-dev-1", spawned)
	}
	if ended := agents[session.EventAgentEnd]; !slices.Equal(ended, []string{"rust-dev-1"}) {
		t.Errorf("the log holds agent_end events for %q, want one, for rust-dev-1 after its last round", ended)
	}
	if began := agents[session.EventTurnStart]; !slices.Equal(began, []string{"rust-dev-1", "rust-dev-1"}) {
		t.Errorf("the log holds turn_start events for %q, want both rounds under rust-dev-1", began)
	}
	if listed := run.header.Agents; len(listed) != 1 || listed[0].Agent != "rust-dev-1" || listed[0].Status != subagent.Finished.String() {
		t.Errorf("the header lists %+v, want rust-dev-1 alone, finished", listed)
	}
	var rerun []subagent.SubAgent
	for _, agent := range run.seen {
		if agent.ID != "rust-dev-1" {
			t.Fatalf("the roster held %q while the sub-agent ran, want rust-dev-1 alone", agent.ID)
		}
		if agent.Round == 2 {
			rerun = append(rerun, agent)
		}
	}
	if len(rerun) == 0 || rerun[0].State != subagent.Working || rerun[0].Report != "sent back: cargo test did not run" {
		t.Errorf("the second round ran on the roster as %+v, want rust-dev-1 working, saying it was sent back and why", rerun)
	}
	if !strings.Contains(run.report, "sent back once: cargo test did not run") || strings.Contains(run.report, "escalating") || strings.Contains(run.report, "-r2") {
		t.Errorf("the report does not name the one send back and its reason, or still escalates:\n%s", run.report)
	}
	if !strings.Contains(run.report, "after 5 steps and 3 tool calls") {
		t.Errorf("the report does not count both rounds' steps and calls:\n%s", run.report)
	}
}

var pyDev = subagent.Definition{Name: "py-dev", Language: "python", Gate: []string{"ruff check", "pytest"}}

func TestAPyDevGateIsMetThroughARunnerAndNotByRuffFormat(t *testing.T) {
	for name, command := range map[string]string{
		"uv run, chained": "uv run ruff check src && uv run pytest tests/test_x.py::test_y",
		"python -m":       "CI=1 ruff check . ; uv run --frozen python -m pytest -q",
	} {
		t.Log(name)
		reopenedFor(t, gatedRun(t, "", pyDev, bashed("pytest", 0), edited("src/x.py"), bashed(command, 0), claimDecision("done")).rows, 1)
	}
	reopenedFor(t, gatedRun(t, "", pyDev,
		edited("src/x.py"), bashed("uv run ruff format src", 0), bashed("uv run pytest", 1), claimDecision("done"),
		bashed("python -m pytest", 0), bashed("ruff check", 0), claimDecision("done")).rows, 2, "ruff check did not run", "pytest exited 1")
}

func TestAGoDevGateIsMetByMakeTargetsWhoseRecipesRunIt(t *testing.T) {
	project := tsProjectHolding(t, map[string]string{"Makefile": "testall:\n\t@echo all\ntest:\n\t@go test ./...\nvet:\n\t-go vet ./...\n"})
	goDev := subagent.Definition{Name: "go-dev", Language: "go", Gate: []string{"go vet", "go test"}}
	reopenedFor(t, gatedRun(t, project, goDev, edited("src/x.go"), bashed("make vet && make test", 0), claimDecision("done")).rows, 1)
	reopenedFor(t, gatedRun(t, project, goDev,
		edited("src/x.go"), bashed("make testall", 0), bashed("make vet", 2), claimDecision("done"),
		bashed("make test vet", 0), claimDecision("done")).rows, 2, "go test did not run", "go vet exited 2")
}

var tsDev = subagent.Definition{Name: "ts-dev", Language: "typescript", Gate: []string{"typecheck", "test"}}

func tsProjectHolding(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestATsDevThatNeverCallsTestInAProjectWithNoTestsFinishesInOneRound(t *testing.T) {
	typechecked := []llm.Decision{edited("src/users.ts"), called("typecheck", map[string]any{}), claimDecision("done"), called("test", map[string]any{}), claimDecision("done")}
	bare := map[string]string{"package.json": `{"name":"bare"}`, "src/users.ts": "export {};\n"}
	if rows := gatedRun(t, tsProjectHolding(t, bare), tsDev, typechecked...).rows; len(rows) != 1 {
		t.Errorf("a project with no tests: %d rounds, want 1: %q", len(rows), rows[len(rows)-1].Task)
	}
	for name, files := range map[string]map[string]string{
		"a test script": {"package.json": `{"name":"scripted","scripts":{"test":"jest"}}`, "src/users.ts": "export {};\n"},
		"a .test. file": {"package.json": `{"name":"bare"}`, "src/users.ts": "export {};\n", "src/users.test.ts": "export {};\n"},
	} {
		reopenedFor(t, gatedRun(t, tsProjectHolding(t, files), tsDev, typechecked...).rows, 2, "test did not run")
		t.Log(name)
	}
}

func TestAResumedSubAgentIsOneSubAgentInTheLogAndItsChatLineSaysSo(t *testing.T) {
	run := gatedRun(t, "", subagent.Definition{Name: "go-dev"}, claimDecision("done"), called("bash", map[string]any{"command": "go vet"}), claimDecision("vetted"))
	if _, err := (messageTool{orchestrator: run.spawn}).Run(run.ctx, json.RawMessage(`{"to":"go-dev-1","text":"run go vet too"}`)); err != nil {
		t.Fatal(err)
	}
	run.collect(t)
	agents := agentsByKind(t, run.events)
	if spawned := agents[session.EventSpawn]; !slices.Equal(spawned, []string{"go-dev-1"}) {
		t.Errorf("the log holds spawn events for %q, want one, for go-dev-1", spawned)
	}
	if began := agents[session.EventTurnStart]; !slices.Equal(began, []string{"go-dev-1", "go-dev-1"}) {
		t.Errorf("the log holds turn_start events for %q, want the spawn and the resume both under go-dev-1", began)
	}
	if listed := run.header.Agents; len(listed) != 1 || listed[0].Agent != "go-dev-1" || listed[0].Status != subagent.Finished.String() {
		t.Errorf("the header lists %+v, want go-dev-1 alone, finished", listed)
	}
	if last := run.seen[len(run.seen)-1]; last.ID != "go-dev-1" || last.State != subagent.Working || last.Report != "resumed by a message" {
		t.Errorf("the resume ran on the roster as %+v, want go-dev-1 working, saying it was resumed", last)
	}
	if !strings.Contains(run.report, "sub-agent go-dev-1 is finished") || strings.Contains(run.report, "-m1") {
		t.Errorf("the resume's report is not go-dev-1's:\n%s", run.report)
	}
}

func TestATestToolAnsweringNoTestsAfterTheEditIsNotAFailure(t *testing.T) {
	rows := gatedRun(t, "", tsDev,
		edited("src/users.ts"), called("typecheck", map[string]any{}), called("test", map[string]any{"fail": testNoTests + "\npackage.json names no test runner"}), claimDecision("done")).rows
	reopenedFor(t, rows, 1)
}

func TestATypecheckStillWarmingIsNotRunRatherThanFailed(t *testing.T) {
	rows := gatedRun(t, "", tsDev,
		edited("src/users.ts"), called("test", map[string]any{}), called("typecheck", map[string]any{"fail": typecheckUnanswered}), claimDecision("done"),
		called("typecheck", map[string]any{}), claimDecision("done")).rows
	reopenedFor(t, rows, 2, "typecheck did not run")
	if strings.Contains(rows[1].Task, "typecheck failed") {
		t.Fatalf("a typecheck that had not answered is named as failed: %q", rows[1].Task)
	}
}
