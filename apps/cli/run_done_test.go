package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/output"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/core"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
)

func panicOf(call func()) (message string) {
	defer func() {
		if raised := recover(); raised != nil {
			message = fmt.Sprint(raised)
		}
	}()
	call()
	return ""
}

const stopCheckWorkRemainsReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-stop-check",` +
	`"answers":{` +
	`"stop_pressure":{"type":"score","score":0,"probabilities":{"0":0.97,"1":0.02,"2":0.01,"3":0},"confidence":0.9},` +
	`"stalled":{"type":"noul","noul":0.05},` +
	`"work_remains":{"type":"noul","noul":0.95},` +
	`"budget_exhausted":{"type":"noul","noul":0.02}` +
	`},"usage":{"input_tokens":12,"output_tokens":3,"cost":0.00003}}`

const stopCheckWorkDoneReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-stop-check-done",` +
	`"answers":{` +
	`"stop_pressure":{"type":"score","score":3,"probabilities":{"0":0,"1":0.01,"2":0.02,"3":0.97},"confidence":0.9},` +
	`"stalled":{"type":"noul","noul":0.9},` +
	`"work_remains":{"type":"noul","noul":0.05},` +
	`"budget_exhausted":{"type":"noul","noul":0.02}` +
	`},"usage":{"input_tokens":12,"output_tokens":3,"cost":0.00003}}`

func stubJev(t *testing.T, status int, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	t.Setenv(judgeEndpointEnvar, server.URL)
	t.Setenv(sys.TypeSafeKeyName, "stub-key-not-a-real-credential")
	bindTypeSafe(t)
}

func claimedDoneWithNothing() turn.Row {
	return turn.Row{
		ID:      "turn-sub-agent",
		Task:    "create hello.txt containing the single word hello, then stop",
		Outcome: turn.OutcomeStopped,
		Steps:   []turn.StepRow{{Index: 1, AssistantText: "Done, hello.txt is written."}},
	}
}

func openLineSubAgent() turn.Row {
	row := claimedDoneWithNothing()
	row.Task = "TOFU-1: create hello.txt containing the single word hello.\n\n## Acceptance\n- hello.txt says hello, pasted\n"
	return row
}

func reviewIn(t *testing.T, mode gate.Mode) typedDoneReview {
	t.Helper()
	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
	review.set.Mode = mode
	return review
}

func stopCheckRow(t *testing.T, id string) ledger.Row {
	t.Helper()
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	row, found, err := ledger.NewReader(dir).ByID(id)
	if err != nil || !found {
		t.Fatalf("the decision id %q names no ledger row (err %v)", id, err)
	}
	if row.Point != "stop_check" || row.TurnID != "turn-sub-agent" {
		t.Fatalf("row %s is %s for turn %q, want stop_check for the sub-agent", row.ID, row.Point, row.TurnID)
	}
	return row
}

func TestAShadowStopCheckDecidesNothingAndTheScreenStands(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkRemainsReply)

	decision, err := reviewIn(t, gate.ModeShadow).Review(context.Background(), claimedDoneWithNothing())
	if err != nil {
		t.Fatalf("the typed arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneAccepted {
		t.Fatalf("a shadow row reading allow reopened the sub-agent: %s", decision.Reason)
	}
	row := stopCheckRow(t, decision.ID)
	if row.Verdict != ledger.VerdictAllow || row.Reason.Mode != ledger.ModeShadow {
		t.Fatalf("row %s reads %s in %s, want allow in shadow", row.ID, row.Verdict, row.Reason.Mode)
	}
	if !strings.Contains(decision.Reason, "shadow") {
		t.Fatalf("the accepted reason does not say the row ran in shadow: %s", decision.Reason)
	}
	t.Logf("shadow: %s, %s", decision.Verdict, decision.Reason)
}

func TestAnOpenLineIsAskedToJevAndReopenedInEitherMode(t *testing.T) {
	for _, mode := range []gate.Mode{gate.ModeShadow, gate.ModeEnforced} {
		t.Chdir(t.TempDir())
		stubJev(t, http.StatusOK, stopCheckWorkDoneReply)

		decision, err := reviewIn(t, mode).Review(context.Background(), openLineSubAgent())
		if err != nil {
			t.Fatalf("%s: the typed arm failed: %v", mode, err)
		}
		if decision.Verdict != turn.DoneReopen || !strings.Contains(decision.Reason, "hello.txt says hello") {
			t.Fatalf("%s: an unmet acceptance line was not reopened naming the line: %s %s", mode, decision.Verdict, decision.Reason)
		}
		row := stopCheckRow(t, decision.ID)
		if row.Reason.Mode != mode.Ledger() {
			t.Fatalf("%s: row %s recorded mode %s", mode, row.ID, row.Reason.Mode)
		}
		t.Logf("%s: %s, %s", mode, decision.Verdict, decision.Reason)
	}
}

func TestAJevErrorStillReopensAnOpenLine(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusInternalServerError, `{"error":{"message":"the route is down"}}`)

	decision, err := reviewIn(t, gate.ModeShadow).Review(context.Background(), openLineSubAgent())
	if err != nil {
		t.Fatalf("jev being down let an unmet line through as an error, so the claim would stand: %v", err)
	}
	if decision.Verdict != turn.DoneReopen {
		t.Fatalf("jev being down accepted an unmet line: %s", decision.Reason)
	}
	if row := stopCheckRow(t, decision.ID); row.Verdict != ledger.VerdictAsk {
		t.Fatalf("the fallback row %s reads %s, want ask", row.ID, row.Verdict)
	}
}

func TestTheDoneReviewIsScreenUnlessTheSettingOrTheCommandLineSaysOtherwise(t *testing.T) {
	emptyHome(t)
	dir := t.TempDir()
	reviewOf := func(args ...string) turn.DoneReview {
		t.Helper()
		opts, err := parseRunArgs(append(append([]string{"--dir", dir}, args...), "a task"))
		if err != nil {
			t.Fatalf("tofu run %v: %v", args, err)
		}
		return newDoneReview(settled(opts, nil).doneArm, func(note string) { t.Errorf("tofu run %v said %q", args, note) })
	}
	if review := reviewOf(); review != turn.DoneReview(turn.ContractDoneReview{}) {
		t.Fatalf("with no setting and no flag the done review is %#v, want the screen", review)
	}
	for _, spawnless := range [][]string{{"--no-subagents"}, {"--tools", toolSetThree}} {
		if review := reviewOf(spawnless...); review != nil {
			t.Fatalf("tofu run %v spawns nothing and still built the done review %#v", spawnless, review)
		}
	}
	store, err := settingspkg.OpenFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetText(settingspkg.Project, settingspkg.DoneReview, settingspkg.DoneReviewOff); err != nil {
		t.Fatal(err)
	}
	if review := reviewOf(); review != nil {
		t.Fatalf("doneReview off still built the done review %#v", review)
	}
	if review := reviewOf("--done-review", settingspkg.DoneReviewCheap); review != turn.DoneReview(turn.CheapDoneReview{}) {
		t.Fatalf("--done-review cheap over doneReview off built %#v, want the flag to win", review)
	}
	if _, err := parseRunArgs([]string{"--dir", dir, "--done-review", "maybe", "a task"}); err == nil {
		t.Fatal("tofu run accepted an unknown done review arm rather than refusing it by name")
	}
}

func TestATypedDoneReviewWithNoJevKeyFallsBackToTheScreenAndSaysSo(t *testing.T) {
	emptyHome(t)
	t.Setenv(sys.OpenRouterClassifierKeyName, "")
	t.Chdir(t.TempDir())
	var said []string
	review := newDoneReview(settingspkg.DoneReviewTyped, func(note string) { said = append(said, note) })
	if review != turn.DoneReview(turn.ContractDoneReview{}) {
		t.Fatalf("typed with no Jev key built %#v, want the screen", review)
	}
	if len(said) != 1 || !strings.Contains(said[0], "typed") || !strings.Contains(said[0], "screen") {
		t.Fatalf("the fallback said %q, want one note naming typed and screen", said)
	}
	t.Log(said[0])
}

func TestADoneReviewIsRefusedOnAnArmThatCanSpawnNoSubAgent(t *testing.T) {
	for _, without := range [][]string{{"--no-subagents"}, {"--tools", toolSetThree}} {
		args := []string{"--dir", t.TempDir(), "--done-review", settingspkg.DoneReviewTyped}
		args = append(append(args, without...), "a task")
		_, err := parseRunArgs(args)
		if err == nil {
			t.Fatalf("tofu run %v took a done review arm on a run that spawns nothing, so the flag says a check is running that is not", without)
		}
		if !strings.Contains(err.Error(), settingspkg.DoneReviewTyped) || !strings.Contains(err.Error(), without[0]) {
			t.Fatalf("the refusal names neither flag: %v", err)
		}
		t.Logf("tofu run %s --done-review typed: %v", strings.Join(without, " "), err)
	}
}

func TestTheCheapDoneReviewArmReopensASubAgentThatRanNoTool(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--done-review", settingspkg.DoneReviewCheap, "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if opts.doneArm != settingspkg.DoneReviewCheap {
		t.Fatalf("--done-review cheap parsed as %q", opts.doneArm)
	}
	decision, err := newDoneReview(opts.doneArm, nil).Review(context.Background(), claimedDoneWithNothing())
	if err != nil {
		t.Fatalf("the cheap arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneReopen {
		t.Fatalf("the cheap arm answered %q for a sub-agent with no tool call at all", decision.Verdict)
	}
	t.Logf("cheap arm: %s, %s", decision.Verdict, decision.Reason)
}

func TestTheTypedDoneReviewArmReopensASubAgentAndLogsTheDecision(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkRemainsReply)

	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--done-review", settingspkg.DoneReviewTyped, "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if opts.doneArm != settingspkg.DoneReviewTyped {
		t.Fatalf("--done-review typed parsed as %q", opts.doneArm)
	}
	if review := newDoneReview(opts.doneArm, func(note string) { t.Fatalf("the typed arm fell back with a Jev stub bound: %s", note) }); review == nil {
		t.Fatal("--done-review typed built no review")
	}

	decision, err := reviewIn(t, gate.ModeEnforced).Review(context.Background(), claimedDoneWithNothing())
	if err != nil {
		t.Fatalf("the typed arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneReopen {
		t.Fatalf("the typed arm answered %q where work_remains is 0.95", decision.Verdict)
	}
	if decision.ID == "" {
		t.Fatal("the typed arm returned no decision id, so nothing points at the ledger row")
	}

	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	row, found, err := ledger.NewReader(dir).ByID(decision.ID)
	if err != nil || !found {
		t.Fatalf("row %s is not in the ledger at %s (err %v)", decision.ID, dir, err)
	}
	if row.Point != "stop_check" || row.Verdict != ledger.VerdictAllow || row.TurnID != "turn-sub-agent" {
		t.Fatalf("the logged row is %s %s for turn %q, want a stop_check allow for the sub-agent", row.Point, row.Verdict, row.TurnID)
	}
	if len(row.Answers) != 4 || row.StateBuilder == "" {
		t.Fatalf("the row carries %d answers and state builder %q", len(row.Answers), row.StateBuilder)
	}
	t.Logf("typed arm: %s, %s", decision.Verdict, decision.Reason)
}

func TestTheTypedDoneReviewBelievesASubAgentThatDidTheWork(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkDoneReply)

	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
	exitZero := 0
	subAgent := turn.Row{
		ID:      "turn-sub-agent",
		Task:    "create hello.txt containing the single word hello, then stop",
		Outcome: turn.OutcomeStopped,
		Steps: []turn.StepRow{
			{Index: 1, ToolCalls: []turn.ToolCallRow{{Tool: "write", Command: "write hello.txt", ExitCode: &exitZero}}},
			{Index: 2, AssistantText: "Done, hello.txt is written."},
		},
	}

	decision, err := review.Review(context.Background(), subAgent)
	if err != nil {
		t.Fatalf("the typed arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneAccepted {
		t.Fatalf("the typed arm reopened a sub-agent that wrote the file and said so: %s", decision.Reason)
	}
	t.Logf("typed arm: %s, %s", decision.Verdict, decision.Reason)
}

func TestAJevErrorLeavesTheSubAgentsClaimStandingAndSaysSoOnTheRow(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusInternalServerError, `{"error":{"message":"the route is down"}}`)

	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
	spawned := spawnOneSubAgent(t, review, []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "I finished the task."},
	})

	if len(spawned) != 1 {
		t.Fatalf("a failed review produced %d sub-agent rows, want the sub-agent's one claim standing", len(spawned))
	}
	subAgent := spawned[0]
	if len(subAgent.Warnings) != 1 || !strings.Contains(subAgent.Warnings[0], "the done review did not run") {
		t.Fatalf("the sub-agent row carries warnings %v, want the reason the review did not run", subAgent.Warnings)
	}
	if len(subAgent.DecisionIDs) != 0 {
		t.Fatalf("a review that never answered still put %v on the sub-agent row", subAgent.DecisionIDs)
	}
	t.Logf("sub-agent warning: %s", subAgent.Warnings[0])

	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	var logged []ledger.Row
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{Point: "stop_check"}, func(row ledger.Row) error {
		logged = append(logged, row)
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(logged) != 1 || logged[0].Verdict != ledger.VerdictAsk {
		t.Fatalf("a review that could not reach jev logged %d rows %+v, want one row reading ask", len(logged), logged)
	}
	t.Logf("ledger row %s verdict %s", logged[0].ID, logged[0].Verdict)
}

func TestTheTypedDoneReviewReopensTheSubAgentInsideARunAndWhyPrintsTheChain(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkRemainsReply)

	review := reviewIn(t, gate.ModeEnforced)
	rounds := konst.SubAgentMaxRounds
	queued := make([]llm.Decision, 0, rounds)
	for round := 1; round <= rounds; round++ {
		queued = append(queued, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage,
			Content: fmt.Sprintf("attempt %d, I finished the task.", round)})
	}
	spawned := spawnOneSubAgent(t, review, queued)

	if len(spawned) != rounds {
		t.Fatalf("the typed review left %d sub-agent rows, want one per round up to the cap of %d", len(spawned), rounds)
	}
	for i, subAgent := range spawned {
		if len(subAgent.DecisionIDs) != 1 {
			t.Fatalf("row %d (%s) carries %d decision ids, want the one review that judged it", i, subAgent.ID, len(subAgent.DecisionIDs))
		}
	}
	first, second, third := spawned[0], spawned[1], spawned[2]
	if second.ID != first.ID || third.ID != first.ID {
		t.Fatalf("the re-opened rounds are %q and %q, want both under %q", second.ID, third.ID, first.ID)
	}
	if !strings.Contains(second.Task, "did not believe you") || !strings.Contains(second.Task, doneReviewPoint) {
		t.Fatalf("the re-opened sub-agent was not told why: %q", second.Task)
	}
	capped := strings.Join(third.Warnings, " ")
	if !strings.Contains(capped, "round cap") {
		t.Fatalf("the round that hit the cap was not refused with the cap named: %q", capped)
	}

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{first.DecisionIDs[0]}, &out, &errOut, time.Now); code != output.ExitOK {
		t.Fatalf("tofu why exited %d (stderr %q)", code, errOut.String())
	}
	printed := out.String()
	for _, wanted := range []string{"stop_check", "work_remains", "ALLOW"} {
		if !strings.Contains(printed, wanted) {
			t.Fatalf("tofu why %s does not name %s:\n%s", first.DecisionIDs[0], wanted, printed)
		}
	}
	t.Logf("re-opened task: %q", second.Task)
	t.Logf("tofu why %s\n%s", first.DecisionIDs[0], printed)
}

func spawnOneSubAgent(t *testing.T, review turn.DoneReview, subAgent []llm.Decision) []turn.Row {
	t.Helper()
	dir := t.TempDir()
	opts := armOpts(t)
	opts.dir, opts.task, opts.gateArm = dir, "hand the note to a sub-agent", gateOff
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	reported := llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the sub-agent reported back"}
	lead := []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}}, reported, reported}

	config, spawner := mustConfig(t, opts, built, runtime{model: &queuedModel{decisions: lead, subAgents: subAgent}, spend: turn.SpendSubscription})
	spawner.Review = review
	leadRows(t, config)
	return spawner.SubAgentRows()
}

func recordedTurn(t *testing.T, name string) turn.Row {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "bench", "stopcheck", "corpus", name))
	if err != nil {
		t.Fatalf("the recorded turn moved: %v", err)
	}
	var row turn.Row
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return row
}

func TestLiveTheTypedDoneReviewReadsTwoRecordedSubAgents(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to spend a fraction of a cent on two live stop_check@1 decisions")
	}
	sys.AllowLiveCredential(t)
	key, err := jev.TypeSafeKey(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	t.Setenv(sys.TypeSafeKeyName, key)

	review := reviewIn(t, gate.ModeEnforced)
	nothing := recordedTurn(t, "turn-18d69b47d7028fec.json")
	wrote := recordedTurn(t, "turn-18d690dd45903d58.json")

	empty, err := review.Review(context.Background(), nothing)
	if err != nil {
		t.Fatalf("the live review of %s failed: %v", nothing.ID, err)
	}
	t.Logf("%s (%d steps): %s %s", nothing.ID, len(nothing.Steps), empty.Verdict, empty.Reason)
	did, err := review.Review(context.Background(), wrote)
	if err != nil {
		t.Fatalf("the live review of %s failed: %v", wrote.ID, err)
	}
	t.Logf("%s (%d steps): %s %s", wrote.ID, len(wrote.Steps), did.Verdict, did.Reason)

	if empty.Verdict != turn.DoneReopen {
		t.Errorf("jev accepted %s, which claimed a file and recorded no step at all", nothing.ID)
	}
	if did.Verdict != turn.DoneAccepted {
		t.Errorf("jev reopened %s, which wrote the file the task asked for and said so", wrote.ID)
	}

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{empty.ID}, &out, &errOut, time.Now); code != output.ExitOK {
		t.Fatalf("tofu why exited %d (stderr %q)", code, errOut.String())
	}
	t.Logf("tofu why %s\n%s", empty.ID, out.String())
}

func TestDoneVerdictNamesEveryLedgerVerdict(t *testing.T) {
	want := map[ledger.Verdict]turn.DoneVerdict{
		ledger.VerdictAllow: turn.DoneReopen,
		ledger.VerdictAsk:   turn.DoneAccepted,
		ledger.VerdictDeny:  turn.DoneAccepted,
	}
	for _, v := range ledger.AllVerdicts() {
		if v == ledger.VerdictUnset {
			if raised := panicOf(func() { doneVerdict(v) }); raised == "" {
				t.Errorf("doneVerdict(unset) no longer panics: the impossible state is now reachable")
			}
			continue
		}
		expected, named := want[v]
		if !named {
			t.Fatalf("%s carries no expected done verdict, so a new ledger verdict can reach doneVerdict untested", v)
		}
		if got := doneVerdict(v); got != expected {
			t.Errorf("doneVerdict(%s) = %s, want %s", v, got, expected)
		}
	}
}

func TestTheSpawnerTofuBuildsKnowsTheProjectItsGateLooksIn(t *testing.T) {
	opts := armOpts(t)
	built, err := buildTestRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	_, spawner := mustConfig(t, opts, built, runtime{spend: turn.SpendSubscription})
	if spawner == nil || spawner.Project != opts.dir {
		t.Fatalf("the spawner runConfig builds, for tofu run and the interface alike, looks for tests in %q, want the run directory %q", spawner.Project, opts.dir)
	}
}
