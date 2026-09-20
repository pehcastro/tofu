package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/turn"
)

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
	t.Setenv("OPENROUTER_KEY", "stub-key-not-a-real-credential")
}

func claimedDoneWithNothing() turn.Row {
	return turn.Row{
		ID:      "turn-child",
		Task:    "create hello.txt containing the single word hello, then stop",
		Outcome: turn.OutcomeStopped,
		Steps:   []turn.StepRow{{Index: 1, AssistantText: "Done, hello.txt is written."}},
	}
}

func TestTheDoneReviewIsOffUntilTheCommandLineAsksForIt(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if opts.doneArm != doneArmOff {
		t.Fatalf("the done review defaults to %q, want %q", opts.doneArm, doneArmOff)
	}
	review, err := newDoneReview(opts.doneArm)
	if err != nil || review != nil {
		t.Fatalf("the off arm built a reviewer %v (err %v), so a child would be judged by default", review, err)
	}
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--done-review", "maybe", "a task"}); err == nil {
		t.Fatal("tofu run accepted an unknown done review arm rather than refusing it by name")
	}
}

func TestADoneReviewIsRefusedOnAnArmThatCanSpawnNoChild(t *testing.T) {
	for _, without := range [][]string{{"--no-crew"}, {"--tools", toolSetThree}} {
		args := []string{"--dir", t.TempDir(), "--done-review", doneArmTyped}
		args = append(append(args, without...), "a task")
		_, err := parseRunArgs(args)
		if err == nil {
			t.Fatalf("tofu run %v took a done review arm on a run that spawns nothing, so the flag says a check is running that is not", without)
		}
		if !strings.Contains(err.Error(), doneArmTyped) || !strings.Contains(err.Error(), without[0]) {
			t.Fatalf("the refusal names neither flag: %v", err)
		}
		t.Logf("tofu run %s --done-review typed: %v", strings.Join(without, " "), err)
	}
}

func TestTheCheapDoneReviewArmReopensAChildThatRanNoTool(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--done-review", doneArmCheap, "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if opts.doneArm != doneArmCheap {
		t.Fatalf("--done-review cheap parsed as %q", opts.doneArm)
	}
	review, err := newDoneReview(opts.doneArm)
	if err != nil {
		t.Fatalf("newDoneReview: %v", err)
	}
	decision, err := review.Review(context.Background(), claimedDoneWithNothing())
	if err != nil {
		t.Fatalf("the cheap arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneReopen {
		t.Fatalf("the cheap arm answered %q for a child with no tool call at all", decision.Verdict)
	}
	t.Logf("cheap arm: %s, %s", decision.Verdict, decision.Reason)
}

func TestTheTypedDoneReviewArmReopensAChildAndLogsTheDecision(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkRemainsReply)

	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--done-review", doneArmTyped, "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if opts.doneArm != doneArmTyped {
		t.Fatalf("--done-review typed parsed as %q", opts.doneArm)
	}
	review, err := newDoneReview(opts.doneArm)
	if err != nil {
		t.Fatalf("newDoneReview: %v", err)
	}

	decision, err := review.Review(context.Background(), claimedDoneWithNothing())
	if err != nil {
		t.Fatalf("the typed arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneReopen {
		t.Fatalf("the typed arm answered %q where work_remains is 0.95", decision.Verdict)
	}
	if decision.ID == "" {
		t.Fatal("the typed arm returned no decision id, so nothing points at the ledger row")
	}

	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	row, found, err := ledger.NewReader(dir).ByID(decision.ID)
	if err != nil || !found {
		t.Fatalf("row %s is not in the ledger at %s (err %v)", decision.ID, dir, err)
	}
	if row.Point != "stop_check" || row.Verdict != ledger.VerdictAllow || row.TurnID != "turn-child" {
		t.Fatalf("the logged row is %s %s for turn %q, want a stop_check allow for the child", row.Point, row.Verdict, row.TurnID)
	}
	if len(row.Answers) != 4 || row.StateBuilder == "" {
		t.Fatalf("the row carries %d answers and state builder %q", len(row.Answers), row.StateBuilder)
	}
	t.Logf("typed arm: %s, %s", decision.Verdict, decision.Reason)
}

func TestTheTypedDoneReviewBelievesAChildThatDidTheWork(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkDoneReply)

	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
	exitZero := 0
	child := turn.Row{
		ID:      "turn-child",
		Task:    "create hello.txt containing the single word hello, then stop",
		Outcome: turn.OutcomeStopped,
		Steps: []turn.StepRow{
			{Index: 1, ToolCalls: []turn.ToolCallRow{{Tool: "write", Command: "write hello.txt", ExitCode: &exitZero}}},
			{Index: 2, AssistantText: "Done, hello.txt is written."},
		},
	}

	decision, err := review.Review(context.Background(), child)
	if err != nil {
		t.Fatalf("the typed arm failed: %v", err)
	}
	if decision.Verdict != turn.DoneAccepted {
		t.Fatalf("the typed arm reopened a child that wrote the file and said so: %s", decision.Reason)
	}
	t.Logf("typed arm: %s, %s", decision.Verdict, decision.Reason)
}

func TestAJevErrorLeavesTheChildsClaimStandingAndSaysSoOnTheRow(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusInternalServerError, `{"error":{"message":"the route is down"}}`)

	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
	spawned := spawnOneChild(t, review, []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "I finished the task."},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child reported back"},
	})

	if len(spawned) != 1 {
		t.Fatalf("a failed review produced %d child rows, want the child's one claim standing", len(spawned))
	}
	child := spawned[0]
	if len(child.Warnings) != 1 || !strings.Contains(child.Warnings[0], "the done review did not run") {
		t.Fatalf("the child row carries warnings %v, want the reason the review did not run", child.Warnings)
	}
	if len(child.DecisionIDs) != 0 {
		t.Fatalf("a review that never answered still put %v on the child row", child.DecisionIDs)
	}
	t.Logf("child warning: %s", child.Warnings[0])

	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
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

func TestTheTypedDoneReviewReopensTheChildInsideARunAndWhyPrintsTheChain(t *testing.T) {
	t.Chdir(t.TempDir())
	stubJev(t, http.StatusOK, stopCheckWorkRemainsReply)

	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
	spawned := spawnOneChild(t, review, []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "I finished the task."},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "this time I read the file first."},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child reported back"},
	})

	if len(spawned) != 2 {
		t.Fatalf("the typed review left %d child rows, want the claim and the re-opened run", len(spawned))
	}
	first, second := spawned[0], spawned[1]
	if len(first.DecisionIDs) != 1 || len(second.DecisionIDs) != 1 || first.DecisionIDs[0] != second.DecisionIDs[0] {
		t.Fatalf("the decision id reached %v and %v, want the one decision on both rows", first.DecisionIDs, second.DecisionIDs)
	}
	if second.ID != first.ID+"-r" {
		t.Fatalf("the re-opened run is %q, want %q", second.ID, first.ID+"-r")
	}
	if !strings.Contains(second.Task, "did not believe you") || !strings.Contains(second.Task, doneReviewPoint) {
		t.Fatalf("the re-opened child was not told why: %q", second.Task)
	}

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{first.DecisionIDs[0]}, &out, &errOut, time.Now); code != exitOK {
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

func spawnOneChild(t *testing.T, review turn.DoneReview, decisions []llm.Decision) []turn.Row {
	t.Helper()
	dir := t.TempDir()
	opts := armOpts(t)
	opts.dir, opts.task, opts.gateArm = dir, "hand the note to a child", gateOff
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	queued := append([]llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}}}, decisions...)

	config, spawner := runConfig(opts, built, runtime{model: &queuedModel{decisions: queued}, spend: turn.SpendSubscription})
	spawner.Review = review
	if _, err := turn.Run(context.Background(), config); err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	return spawner.Children()
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

func TestLiveTheTypedDoneReviewReadsTwoRecordedChildren(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to spend a fraction of a cent on two live stop_check@1 decisions")
	}
	key, err := jev.Key(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	t.Setenv("OPENROUTER_KEY", key)

	review, err := newTypedDoneReview()
	if err != nil {
		t.Fatalf("newTypedDoneReview: %v", err)
	}
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
	if code := whyVerb([]string{empty.ID}, &out, &errOut, time.Now); code != exitOK {
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
