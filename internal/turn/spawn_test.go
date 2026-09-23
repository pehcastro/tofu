package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/crew"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
)

func claimDecision(text string) llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: text}
}

func spawnArgsJSON(t *testing.T, args spawnArgs) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func spawnCall(id, task string, owns ...string) llm.Decision {
	args, err := json.Marshal(spawnArgs{Task: task, Owns: owns})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "spawn", Arguments: args})
}

func writeCall(id, path, content string) llm.Decision {
	args, err := json.Marshal(writeArgs{Path: path, Content: content})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "write", Arguments: args})
}

func parentTurn(t *testing.T, root string, decisions []llm.Decision) (Config, *SpawnTool) {
	t.Helper()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the write tool: %v", err)
	}
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}
	const parentID = "turn-parent"
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(read, write),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return parentID },
	}
	spawn := NewSpawnTool(parentID, base, &crew.Roster{})
	parent := base
	parent.Task = "hand the work to a child"
	parent.Tools = NewRegistry(read, write, spawn)
	return parent, spawn
}

func firstToolCall(t *testing.T, row Row) ToolCallRow {
	t.Helper()
	for _, step := range row.Steps {
		if len(step.ToolCalls) > 0 {
			return step.ToolCalls[0]
		}
	}
	t.Fatalf("row %s recorded no tool call", row.ID)
	return ToolCallRow{}
}

func showRow(t *testing.T, label string, row Row) {
	t.Helper()
	rendered, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		t.Fatalf("rendering the %s row: %v", label, err)
	}
	t.Logf("%s row:\n%s", label, rendered)
}

func TestSpawnRunsAChildInAScratchTreeAndTheRowsNameEachOther(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mine"), 0o750); err != nil {
		t.Fatal(err)
	}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		writeCall("call-2", "mine/hello.txt", "written by the child"),
		messageDecision(),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(root, "mine", "hello.txt"))
	if err != nil {
		t.Fatalf("the child did not do the work: %v", err)
	}
	if string(written) != "written by the child" {
		t.Fatalf("the child wrote %q", written)
	}

	children := spawn.Children()
	if len(children) != 1 {
		t.Fatalf("expected one child row, got %d", len(children))
	}
	child := children[0]
	if child.ID != "turn-parent-c1" {
		t.Fatalf("the child row does not name its parent: %q", child.ID)
	}
	if !strings.HasPrefix(child.ID, row.ID) {
		t.Fatalf("child %q is not under parent %q", child.ID, row.ID)
	}
	call := firstToolCall(t, row)
	if call.Command != "spawn "+child.ID+" in_review: write the greeting under mine/" {
		t.Fatalf("the parent row does not name the child, its state and its mission: %+v", call)
	}
	if len(child.Steps) == 0 || len(child.Steps[0].ToolCalls) != 1 {
		t.Fatalf("the child row does not carry the work it did: %+v", child.Steps)
	}

	showRow(t, "parent", row)
	showRow(t, "child", child)
}

func TestAChildWritingOutsideItsPathsIsRefusedAtTheWrite(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"mine", "theirs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "theirs", "notes.txt"), []byte("somebody else's file"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "stay inside mine/", "mine/**"),
		toolCallDecision(llm.ToolCall{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{"path":"theirs/notes.txt"}`)}),
		writeCall("call-3", "theirs/stolen.txt", "the child reached outside"),
		messageDecision(),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "theirs", "stolen.txt")); !os.IsNotExist(err) {
		t.Fatalf("the write happened and was only reported afterwards: %v", err)
	}
	child := spawn.Children()[0]
	if read := firstToolCall(t, child); read.Tool != "read" || read.Error != "" {
		t.Fatalf("ownership holds writes, not reads, and the read was refused: %+v", read)
	}
	refusal := child.Steps[1].ToolCalls[0].Error
	if !strings.Contains(refusal, "outside the paths this agent holds") {
		t.Fatalf("the refusal does not say what was wrong: %q", refusal)
	}
	if !strings.Contains(refusal, "mine/**") {
		t.Fatalf("the refusal does not name the paths the child holds: %q", refusal)
	}
	if !strings.Contains(firstToolCall(t, row).Command, child.ID) {
		t.Fatalf("the parent row does not name the child that was refused")
	}
	t.Logf("refused: %s", refusal)
}

func spawnDirect(t *testing.T, spawn *SpawnTool, task string, owns ...string) Result {
	t.Helper()
	result, err := spawn.Run(context.Background(), spawnArgsJSON(t, spawnArgs{Task: task, Owns: owns}))
	if err != nil {
		t.Fatalf("spawning onto %v returned an error instead of a result: %v", owns, err)
	}
	return result
}

func TestASpawnOntoAHeldPathHandsBackTheChildHoldingIt(t *testing.T) {
	root := t.TempDir()
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "the crew package", "internal/crew/**"),
		claimDecision("I rewrote the roster and the tests pass"),
		messageDecision(),
	})
	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	result := spawnDirect(t, spawn, "one file inside the crew package", "internal/crew/owns.go")
	for _, want := range []string{
		"turn-parent-c1",
		"internal/crew/**",
		"internal/crew/owns.go",
		"Send this work to turn-parent-c1 rather than starting a rival",
		"I rewrote the roster and the tests pass",
	} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the handback does not carry %q: %q", want, result.Content)
		}
	}
	if result.Command != "handback turn-parent-c1" {
		t.Fatalf("the handback is not recorded as one: %q", result.Command)
	}
	if len(spawn.Children()) != 1 {
		t.Fatalf("a rival child was started: %d children", len(spawn.Children()))
	}
	t.Logf("handback:\n%s", result.Content)
}

func TestTwoDisjointChildrenRunAndAThirdOverlappingEitherIsHandedBack(t *testing.T) {
	root := t.TempDir()
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "the crew package", "internal/crew/**"),
		claimDecision("crew done"),
		spawnCall("call-2", "the turn package", "internal/turn/**"),
		claimDecision("turn done"),
		messageDecision(),
	})
	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	children := spawn.Children()
	if len(children) != 2 || children[0].ID != "turn-parent-c1" || children[1].ID != "turn-parent-c2" {
		t.Fatalf("two disjoint children did not both run: %+v", children)
	}
	for _, c := range []struct{ glob, holder, claim string }{
		{"internal/crew/owns.go", "turn-parent-c1", "crew done"},
		{"internal/turn/spawn.go", "turn-parent-c2", "turn done"},
	} {
		result := spawnDirect(t, spawn, "a third child", c.glob)
		if !strings.Contains(result.Content, c.holder) || !strings.Contains(result.Content, c.claim) {
			t.Fatalf("%q was not handed back to %s with its report: %q", c.glob, c.holder, result.Content)
		}
	}
	if len(spawn.Children()) != 2 {
		t.Fatalf("a third child ran: %d children", len(spawn.Children()))
	}
}

func TestAChildWithNoEvidenceIsReopenedExactlyOnce(t *testing.T) {
	root := t.TempDir()
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		claimDecision("all done"),
		claimDecision("still done, and still nothing to show"),
		messageDecision(),
	})
	spawn.Review = CheapDoneReview{}

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	children := spawn.Children()
	reopened := 0
	for _, child := range children {
		if strings.HasSuffix(child.ID, "-r") {
			reopened++
		}
	}
	if reopened != 1 {
		t.Fatalf("expected exactly one re-open, got %d in %d children", reopened, len(children))
	}
	if children[1].ID != "turn-parent-c1-r" {
		t.Fatalf("the re-opened row does not name the child it re-opens: %q", children[1].ID)
	}
	if !strings.Contains(children[1].Task, "not one tool call ran without failing") {
		t.Fatalf("the re-opened child was not told why: %q", children[1].Task)
	}
	if last := children[1].Steps[len(children[1].Steps)-1].AssistantText; last != "still done, and still nothing to show" {
		t.Fatalf("the second claim did not stand: %q", last)
	}
	asked := parent.Model.(*stubModel).calls
	if asked != 4 {
		t.Fatalf("the model was asked %d times, so the re-open was not bounded at one round", asked)
	}
	t.Logf("re-opened task: %q", children[1].Task)
}

type stubReview struct {
	writer   *ledger.Writer
	verdict  DoneVerdict
	reviewed int
}

func (r *stubReview) Review(_ context.Context, child Row) (DoneDecision, error) {
	r.reviewed++
	row, err := r.writer.Append(ledger.Row{
		Point:     "stop_check@1",
		Questions: "stop_check",
		Version:   1,
		Build:     "jev-test",
		Verdict:   ledger.VerdictAsk,
		TurnID:    child.ID,
		Answers: []ledger.Answer{{
			Question: "work_remains", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.93,
		}},
	})
	if err != nil {
		return DoneDecision{}, err
	}
	return DoneDecision{ID: row.ID, Verdict: r.verdict, Reason: "work_remains 0.93"}, nil
}

func TestTheReopenDecisionIsOnTheChildRowAndInTheLedger(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ledger")
	review := &stubReview{writer: ledger.NewWriter(dir), verdict: DoneReopen}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		claimDecision("all done"),
		claimDecision("done again"),
		messageDecision(),
	})
	spawn.Review = review

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	children := spawn.Children()
	if review.reviewed != 1 || len(children) != 2 {
		t.Fatalf("reviewed %d times, %d children", review.reviewed, len(children))
	}
	ids := children[0].DecisionIDs
	if len(ids) != 1 || ids[0] == "" {
		t.Fatalf("the reviewed row does not carry the decision id: %v", ids)
	}
	if !slices.Equal(children[1].DecisionIDs, ids) {
		t.Fatalf("the re-opened row does not carry the decision that re-opened it: %v", children[1].DecisionIDs)
	}
	row, found, err := ledger.NewReader(dir).ByID(ids[0])
	if err != nil || !found {
		t.Fatalf("tofu why reads the ledger by id and %s is not there: found %v, %v", ids[0], found, err)
	}
	if row.Point != "stop_check@1" || len(row.Answers) != 1 {
		t.Fatalf("the ledger row is not the stop_check chain why would print: %+v", row)
	}
	t.Logf("decision %s: point %s, verdict %s, %s=%.2f", row.ID, row.Point, row.Verdict, row.Answers[0].Question, row.Answers[0].Noul)
}

func TestWithTheReviewOffTheChildClaimReachesTheParentUnchanged(t *testing.T) {
	root := t.TempDir()
	const claim = "I could not find the file, so I changed nothing and this is not done"
	_, spawn := parentTurn(t, root, []llm.Decision{claimDecision(claim), messageDecision()})
	if spawn.Review != nil {
		t.Fatal("the done review is on by default, and off is meant to be the behaviour today")
	}

	result := spawnDirect(t, spawn, "the crew package", "internal/crew/**")

	children := spawn.Children()
	if len(children) != 1 {
		t.Fatalf("the claim was reviewed: %d children", len(children))
	}
	if result.Content != spawn.Reports()[0].Text() {
		t.Fatalf("the parent saw something other than the child's report: %q", result.Content)
	}
	if !strings.HasSuffix(result.Content, claim) {
		t.Fatalf("the claim did not reach the parent byte for byte: %q", result.Content)
	}
}

type childTool struct {
	name string
	run  func(ctx context.Context) (Result, error)
}

func (t *childTool) Name() string { return t.name }

func (t *childTool) Definition() llm.Tool {
	return llm.Tool{Name: t.name, Description: "a tool the child calls", Parameters: map[string]any{"type": "object"}}
}

func (t *childTool) Run(ctx context.Context, _ json.RawMessage) (Result, error) { return t.run(ctx) }

func childCall(id, name string) llm.Decision {
	return toolCallDecision(llm.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(`{}`)})
}

func onlyChild(t *testing.T, spawn *SpawnTool) crew.SubAgent {
	t.Helper()
	held := spawn.roster.SubAgents()
	if len(held) != 1 {
		t.Fatalf("the roster holds %d sub-agents, want 1", len(held))
	}
	return held[0]
}

func TestAChildThatReturnsNormallyIsInReviewBecauseFinishingIsTheOrchestratorsWord(t *testing.T) {
	root := t.TempDir()
	_, spawn := parentTurn(t, root, []llm.Decision{claimDecision("I did the work and the tests pass")})

	result := spawnDirect(t, spawn, "write the greeting under mine/", "mine/**")

	held := onlyChild(t, spawn)
	if held.State != crew.InReview {
		t.Fatalf("a child that stopped talking reads as %s, want in_review", held.State)
	}
	if strings.Contains(result.Content, "finished") {
		t.Fatalf("the child's own report calls the work finished: %q", result.Content)
	}
	if !strings.Contains(result.Content, "is in_review") {
		t.Fatalf("the report does not say the work is waiting to be checked: %q", result.Content)
	}
	t.Logf("report: %s", result.Content)
}

func TestOnlyTheReviewMovesAChildToFinished(t *testing.T) {
	root := t.TempDir()
	review := &stubReview{writer: ledger.NewWriter(filepath.Join(root, "ledger")), verdict: DoneAccepted}
	_, spawn := parentTurn(t, root, []llm.Decision{claimDecision("I did the work")})
	spawn.Review = review

	result := spawnDirect(t, spawn, "write the greeting under mine/", "mine/**")

	if held := onlyChild(t, spawn); held.State != crew.Finished {
		t.Fatalf("the review accepted the work and the child reads as %s, want finished", held.State)
	}
	if !strings.Contains(result.Content, "is finished") {
		t.Fatalf("the report does not carry the state the review reached: %q", result.Content)
	}
}

func TestAChildIsWorkingWhileItRuns(t *testing.T) {
	root := t.TempDir()
	_, spawn := parentTurn(t, root, []llm.Decision{childCall("call-1", "probe"), claimDecision("done")})
	var seen crew.State
	spawn.base.Tools = NewRegistry(&childTool{name: "probe", run: func(context.Context) (Result, error) {
		seen = spawn.roster.SubAgents()[0].State
		return Result{Content: "the roster was read from inside the child"}, nil
	}})

	spawnDirect(t, spawn, "read the roster from inside", "mine/**")

	if seen != crew.Working {
		t.Fatalf("a child running its tools reads as %s, want working", seen)
	}
}

func readWhileTheChildIsStillRunning(t *testing.T, root, task string, decisions []llm.Decision) crew.SubAgent {
	t.Helper()
	_, spawn := parentTurn(t, root, decisions)
	stepped := make(chan StepRow, len(decisions))
	spawn.base.Step = func(step StepRow) { stepped <- step }
	var seen crew.SubAgent
	probes := 0
	spawn.base.Tools = NewRegistry(&childTool{name: "probe", run: func(context.Context) (Result, error) {
		probes++
		if probes > 1 {
			<-stepped
			seen = spawn.roster.SubAgents()[0]
		}
		return Result{Content: "the roster was read from inside the child"}, nil
	}})

	spawnDirect(t, spawn, task, "mine/**")

	if seen.State != crew.Working {
		t.Fatalf("the child read as %s, so it was not still running and this proves nothing", seen.State)
	}
	return seen
}

func TestARunningChildCarriesWhenItStartedAndTheStepThatMovedIt(t *testing.T) {
	seen := readWhileTheChildIsStillRunning(t, t.TempDir(), "read the roster from inside", []llm.Decision{
		childCall("call-1", "probe"), childCall("call-2", "probe"), claimDecision("done"),
	})

	if seen.Started.IsZero() {
		t.Fatal("a running child carries no start time")
	}
	if seen.Active.Before(seen.Started) {
		t.Fatalf("a running child last stepped at %v, before it started at %v", seen.Active, seen.Started)
	}
	if seen.Steps != 1 {
		t.Fatalf("one step had ended and the running child reads %d steps", seen.Steps)
	}
	t.Logf("started %v, last active %v, %d steps", seen.Started, seen.Active, seen.Steps)
}

func TestARunningChildCarriesTheToolItCalledAndNeverTheArguments(t *testing.T) {
	const planted = "sk-live-4f9c2b7e0a13d85f6c21"
	secretCall := toolCallDecision(llm.ToolCall{ID: "call-1", Name: "probe", Arguments: json.RawMessage(`{"command":"export OPENROUTER_KEY=` + planted + `"}`)})
	seen := readWhileTheChildIsStillRunning(t, t.TempDir(), "call a tool with a key in its arguments", []llm.Decision{
		secretCall, childCall("call-2", "probe"), claimDecision("done"),
	})

	if !slices.Equal(seen.Calling, []string{"probe"}) {
		t.Fatalf("a running child carries %v, want the name of the call its first step made", seen.Calling)
	}
	if held := fmt.Sprintf("%+v", seen); strings.Contains(held, planted) {
		t.Fatalf("the roster repeats a key that was in a call's arguments: %s", held)
	}
	t.Logf("calling %v, and the planted %q is nowhere in %+v", seen.Calling, planted, seen)
}

func TestAChildThatOutRunsTheBoundCarriesItsNewestCalls(t *testing.T) {
	steps := konst.SubAgentCallsWatched + 2
	var decisions []llm.Decision
	for step := range steps {
		decisions = append(decisions, childCall("call-"+strconv.Itoa(step+1), "probe"))
	}
	seen := readWhileTheChildIsStillRunning(t, t.TempDir(), "call one tool many times", append(decisions, claimDecision("done")))

	if len(seen.Calling) > konst.SubAgentCallsWatched {
		t.Fatalf("a child %d steps in carries %d calls, want at most %d", steps, len(seen.Calling), konst.SubAgentCallsWatched)
	}
	t.Logf("%d steps of one call each left %v", steps, seen.Calling)
}

func TestAParkedChildKeepsTheWorkItHadAlreadyDone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mine"), 0o750); err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	_, spawn := parentTurn(t, root, []llm.Decision{
		writeCall("call-1", "mine/half.txt", "half the work, done before the stop"),
		childCall("call-2", "park"),
		claimDecision("I was asked to stop"),
	})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	spawn.base.Tools = NewRegistry(write, &childTool{name: "park", run: func(context.Context) (Result, error) {
		stop()
		return Result{Content: "the orchestrator asked this sub-agent to stop"}, nil
	}})

	result, err := spawn.Run(ctx, spawnArgsJSON(t, spawnArgs{Task: "write half of it and then be stopped", Owns: []string{"mine/**"}}))
	if err != nil {
		t.Fatalf("a parked child is not a failed one: %v", err)
	}

	if held := onlyChild(t, spawn); held.State != crew.Parked {
		t.Fatalf("a child the orchestrator stopped reads as %s, want parked", held.State)
	}
	half, err := os.ReadFile(filepath.Join(root, "mine", "half.txt"))
	if err != nil || string(half) != "half the work, done before the stop" {
		t.Fatalf("the work done before the stop was lost: %q, %v", half, err)
	}
	kept := spawn.Children()[0]
	if len(kept.Steps) < 2 || firstToolCall(t, kept).Tool != "write" {
		t.Fatalf("the parked row does not carry what the child produced: %+v", kept.Steps)
	}
	if !strings.Contains(result.Content, "is parked") {
		t.Fatalf("the report does not say the child was parked: %q", result.Content)
	}
	t.Logf("parked report: %s", result.Content)
}

func TestAChildWhoseModelFailsIsRecordedAsErrored(t *testing.T) {
	root := t.TempDir()
	_, spawn := parentTurn(t, root, nil)
	spawn.base.Model = &errorModel{err: errors.New("the wire is down")}

	args := spawnArgsJSON(t, spawnArgs{Task: "work that cannot start", Owns: []string{"mine/**"}})
	if _, err := spawn.Run(context.Background(), args); err == nil {
		t.Fatal("a child whose model failed returned a result")
	} else if !strings.Contains(err.Error(), "is errored") || !strings.Contains(err.Error(), "the wire is down") {
		t.Fatalf("the error does not carry the state and the cause: %v", err)
	}

	held := onlyChild(t, spawn)
	if held.State != crew.Errored {
		t.Fatalf("a child whose model failed reads as %s, want errored", held.State)
	}
	if !strings.Contains(held.Report, "is errored") {
		t.Fatalf("the roster kept no report for the errored child: %q", held.Report)
	}
}

func TestAChildStoppedByTheStepCapIsInReviewAndKeepsItsWork(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mine"), 0o750); err != nil {
		t.Fatal(err)
	}
	_, spawn := parentTurn(t, root, []llm.Decision{
		writeCall("call-1", "mine/one.txt", "the one step it was allowed"),
		claimDecision("I ran out of steps"),
	})
	spawn.base.Caps = Caps{MaxSteps: 1}

	result := spawnDirect(t, spawn, "more work than one step", "mine/**")

	if held := onlyChild(t, spawn); held.State != crew.InReview {
		t.Fatalf("a capped child reads as %s, want in_review", held.State)
	}
	capped := spawn.Children()[0]
	if capped.Outcome != OutcomeStepCap {
		t.Fatalf("the child row says %s, want the step cap", capped.Outcome)
	}
	if _, err := os.Stat(filepath.Join(root, "mine", "one.txt")); err != nil {
		t.Fatalf("the capped child lost the step it did take: %v", err)
	}
	t.Logf("capped report: %s", result.Content)
}

func TestAChildThatEndsReleasesTheProcessItLeftBehind(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH, so no process can be started to outlive a child")
	}
	root := t.TempDir()
	_, spawn := parentTurn(t, root, []llm.Decision{childCall("call-1", "linger"), claimDecision("I left something running")})
	var left *exec.Cmd
	spawn.base.Tools = NewRegistry(&childTool{name: "linger", run: func(ctx context.Context) (Result, error) {
		left = exec.CommandContext(ctx, shell, "-c", "sleep 30")
		if err := left.Start(); err != nil {
			return Result{}, err
		}
		return Result{Content: "a process is running in the background"}, nil
	}})

	spawnDirect(t, spawn, "start something and walk away", "mine/**")

	if left == nil {
		t.Fatal("the child never started the process")
	}
	waited := make(chan error, 1)
	go func() { waited <- left.Wait() }()
	select {
	case err := <-waited:
		t.Logf("the process the child left behind ended with %v once the child did", err)
	case <-time.After(10 * time.Second):
		t.Fatalf("process %d is still alive after its child ended", left.Process.Pid)
	}
}

func TestTheParentKeepsTheStepsOfOnlyTheMostRecentChildren(t *testing.T) {
	root := t.TempDir()
	review := &stubReview{writer: ledger.NewWriter(filepath.Join(root, "ledger")), verdict: DoneReopen}
	var decisions []llm.Decision
	for range konst.CrewMaxBreadth * 2 {
		decisions = append(decisions, claimDecision("done"))
	}
	_, spawn := parentTurn(t, root, decisions)
	spawn.Review = review

	for child := 1; child <= konst.CrewMaxBreadth; child++ {
		spawnDirect(t, spawn, "a piece of the work", fmt.Sprintf("part%d/**", child))
	}

	rows := spawn.Children()
	if len(rows) != konst.CrewMaxBreadth*2 {
		t.Fatalf("%d rows, want %d: every child was reopened once", len(rows), konst.CrewMaxBreadth*2)
	}
	carrying := 0
	for _, row := range rows {
		if len(row.Steps) > 0 || row.Conversation != nil {
			carrying++
		}
		if row.ID == "" {
			t.Fatalf("a released row lost the identity the parent counts it by: %+v", row)
		}
	}
	if carrying != konst.SubAgentRetainedRows {
		t.Fatalf("%d of %d rows still carry their steps and their conversation, and the limit is %d",
			carrying, len(rows), konst.SubAgentRetainedRows)
	}
	for _, row := range rows[:len(rows)-konst.SubAgentRetainedRows] {
		if len(row.Steps) != 0 || row.Conversation != nil {
			t.Fatalf("row %s was not released: %d steps, %d messages", row.ID, len(row.Steps), len(row.Conversation))
		}
	}
	t.Logf("%d child rows, %d still carrying what they said", len(rows), carrying)
}

func TestTheRecordCarriesTheMissionAndTheBriefVerbatim(t *testing.T) {
	root := t.TempDir()
	const brief = "write the greeting under mine/, then read it back and say what it says"
	store := session.NewStore(filepath.Join(root, "sessions"))
	parent, spawn := parentTurn(t, root, []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "spawn",
			Arguments: spawnArgsJSON(t, spawnArgs{Task: brief, Mission: "work on BOJI-196", Owns: []string{"mine/**"}})}),
		claimDecision("the greeting says hello"),
		messageDecision(),
	})
	parent.Sessions, spawn.base.Sessions = store, store

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	child, err := store.Header("turn-parent-c1")
	if err != nil {
		t.Fatalf("the child wrote no session: %v", err)
	}
	if child.Task != brief {
		t.Fatalf("the child's record does not carry the brief verbatim: %q", child.Task)
	}
	events, err := store.Body("turn-parent")
	if err != nil {
		t.Fatal(err)
	}
	recorded := ""
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatal(err)
		}
		for _, call := range step.ToolCalls {
			if call.Tool == "spawn" {
				recorded = call.Command + "\n" + string(call.Args)
			}
		}
	}
	for _, want := range []string{"work on BOJI-196", brief, "turn-parent-c1"} {
		if !strings.Contains(recorded, want) {
			t.Fatalf("the recorded session does not carry %q:\n%s", want, recorded)
		}
	}
	t.Logf("recorded:\n%s", recorded)
}

func TestTheDepthBoundRefusesAndNamesItsLimit(t *testing.T) {
	root := t.TempDir()
	var decisions []llm.Decision
	for level := 1; level <= konst.CrewMaxDepth+1; level++ {
		decisions = append(decisions, spawnCall("call-1", "one level deeper", fmt.Sprintf("level%d/**", level)))
	}
	for range konst.CrewMaxDepth + 1 {
		decisions = append(decisions, messageDecision())
	}
	parent, spawn := parentTurn(t, root, decisions)

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	deepest := spawn.Children()[len(spawn.Children())-1]
	refusal := firstToolCall(t, deepest).Error
	want := fmt.Sprintf("a child at depth %d would pass the crew depth limit of %d", konst.CrewMaxDepth+1, konst.CrewMaxDepth)
	if !strings.Contains(refusal, want) {
		t.Fatalf("the deepest child was not refused with %q: %q", want, refusal)
	}
	t.Logf("refused: %s", refusal)
}

func TestTheBreadthBoundRefusesAndNamesItsLimit(t *testing.T) {
	root := t.TempDir()
	var decisions []llm.Decision
	for child := 1; child <= konst.CrewMaxBreadth; child++ {
		decisions = append(decisions, spawnCall("call-1", "a piece of the work", fmt.Sprintf("part%d/**", child)), messageDecision())
	}
	decisions = append(decisions, spawnCall("call-1", "one child too many", "extra/**"), messageDecision())
	parent, spawn := parentTurn(t, root, decisions)

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if len(spawn.Children()) != konst.CrewMaxBreadth {
		t.Fatalf("expected %d children, got %d", konst.CrewMaxBreadth, len(spawn.Children()))
	}
	refusal := row.Steps[konst.CrewMaxBreadth].ToolCalls[0].Error
	want := fmt.Sprintf("already spawned %d children and the crew breadth limit is %d", konst.CrewMaxBreadth, konst.CrewMaxBreadth)
	if !strings.Contains(refusal, want) {
		t.Fatalf("the extra child was not refused with %q: %q", want, refusal)
	}
	t.Logf("refused: %s", refusal)
}
