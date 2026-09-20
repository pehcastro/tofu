package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/session"
)

func costing(decision llm.Decision, usd float64) llm.Decision {
	decision.Usage.Cost = usd
	return decision
}

func sameMoney(usd, want float64) bool { return math.Abs(usd-want) < 0.000001 }

func parentWithAStore(t *testing.T, root string, decisions []llm.Decision) (Config, *SpawnTool, *session.Store) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "mine"), 0o750); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(filepath.Join(root, ".tofu", "sessions"))
	parent, spawn := parentTurn(t, root, decisions)
	parent.Sessions, spawn.base.Sessions = store, store
	return parent, spawn, store
}

func recordedSteps(t *testing.T, store *session.Store, id string) (int, float64) {
	t.Helper()
	events, err := store.Body(id)
	if err != nil {
		t.Fatalf("reading the record of %s: %v", id, err)
	}
	steps, cost := 0, 0.0
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("a recorded step of %s is not a step row: %v", id, err)
		}
		steps++
		cost += step.CostUSD
	}
	return steps, cost
}

func TestAParentAndItsChildAreBothRecordedSessionsThatNameEachOther(t *testing.T) {
	root := filepath.Join(os.TempDir(), "tofu-session-pair")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	parent, _, store := parentWithAStore(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		writeCall("call-2", "mine/hello.txt", "written by the child"),
		claimDecision("I wrote mine/hello.txt"),
		claimDecision("the child did the work"),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(row.ChildIDs) != 1 {
		t.Fatalf("the parent row names %v children, want one", row.ChildIDs)
	}
	childID := row.ChildIDs[0]

	parentHeader, err := store.Header(row.ID)
	if err != nil {
		t.Fatalf("the parent has no record: %v", err)
	}
	childHeader, err := store.Header(childID)
	if err != nil {
		t.Fatalf("the child ran its own loop and left no record: %v", err)
	}
	if childHeader.Parent != row.ID {
		t.Fatalf("the child record names %q as its parent, want %q", childHeader.Parent, row.ID)
	}
	if childHeader.Outcome != OutcomeStopped.String() {
		t.Fatalf("the child record closed as %q, want the outcome of a loop that ran to an answer", childHeader.Outcome)
	}
	if steps, _ := recordedSteps(t, store, childID); steps < 2 {
		t.Fatalf("the child record carries %d steps, want the write and the answer", steps)
	}
	if written, err := os.ReadFile(filepath.Join(root, "mine", "hello.txt")); err != nil || string(written) != "written by the child" {
		t.Fatalf("the child did not do the work: %q %v", written, err)
	}
	t.Logf("read both back with: cd %s && tofu session info %s && tofu session info %s",
		root, parentHeader.ID, childHeader.ID)
}

func TestTheParentSeesTheChildStepsWhileItRunsAndNotOnlyAtTheEnd(t *testing.T) {
	root := t.TempDir()
	const childID = "turn-parent-c1"
	parent, spawn, store := parentWithAStore(t, root, []llm.Decision{
		spawnCall("call-1", "run the probe three times", "mine/**"),
		costing(childCall("call-2", "probe"), 0.01),
		costing(childCall("call-3", "probe"), 0.01),
		costing(childCall("call-4", "probe"), 0.01),
		claimDecision("the probes ran"),
		claimDecision("the child reported"),
	})
	var steps []int
	var spends []float64
	spawn.base.Tools = NewRegistry(&childTool{name: "probe", run: func(context.Context) (Result, error) {
		seen, cost := recordedSteps(t, store, childID)
		steps, spends = append(steps, seen), append(spends, cost)
		return Result{Content: fmt.Sprintf("the parent could see %d steps and $%.4f", seen, cost)}, nil
	}})

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if len(steps) != 3 {
		t.Fatalf("the probe ran %d times, want three readings", len(steps))
	}
	if steps[0] == steps[1] || steps[1] == steps[2] {
		t.Fatalf("the parent saw the same child at every point: %v", steps)
	}
	if spends[0] >= spends[2] {
		t.Fatalf("the child's spend did not grow while it ran: %v", spends)
	}
	final, _ := recordedSteps(t, store, childID)
	if final <= steps[2] {
		t.Fatalf("the last live reading was %d and the finished record has %d, so nothing was read early", steps[2], final)
	}
	t.Logf("the parent read the child at three points: steps %v, spend %v, and %d steps once it ended", steps, spends, final)
}

func TestTheChildSpendIsOnItsOwnRowAndAlsoInTheParentTotal(t *testing.T) {
	root := t.TempDir()
	parent, spawn, _ := parentWithAStore(t, root, []llm.Decision{
		costing(spawnCall("call-1", "write the greeting under mine/", "mine/**"), 0.01),
		costing(writeCall("call-2", "mine/hello.txt", "written by the child"), 0.02),
		costing(claimDecision("I wrote it"), 0.03),
		costing(claimDecision("the child did the work"), 0.04),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	child := spawn.Children()[0]
	if !sameMoney(child.TotalCostUSD, 0.05) {
		t.Fatalf("the child row carries $%.4f, want the $0.05 the child itself spent", child.TotalCostUSD)
	}
	if !sameMoney(row.TotalCostUSD, 0.10) {
		t.Fatalf("the parent row carries $%.4f, want its own $0.05 and the child's $0.05", row.TotalCostUSD)
	}
	t.Logf("child %s spent $%.4f, parent %s totals $%.4f", child.ID, child.TotalCostUSD, row.ID, row.TotalCostUSD)
}

func TestAGrandchildIsCountedOnceInTheParentTotal(t *testing.T) {
	root := t.TempDir()
	parent, spawn, _ := parentWithAStore(t, root, []llm.Decision{
		costing(spawnCall("call-1", "hand it on", "mine/**"), 0.01),
		costing(spawnCall("call-2", "do the work", "theirs/**"), 0.02),
		costing(claimDecision("the grandchild is done"), 0.04),
		costing(claimDecision("the child is done"), 0.08),
		costing(claimDecision("the parent is done"), 0.16),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if !sameMoney(row.TotalCostUSD, 0.31) {
		t.Fatalf("the parent row carries $%.4f, want $0.31: every step once and the grandchild not twice", row.TotalCostUSD)
	}
	if len(spawn.Children()) != 2 {
		t.Fatalf("the parent kept %d descendant rows, want the child and the grandchild", len(spawn.Children()))
	}
}

func TestTheParentReadsTheChildReportByFieldAndNotFromItsProse(t *testing.T) {
	root := t.TempDir()
	parent, spawn, _ := parentWithAStore(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		writeCall("call-2", "mine/real.txt", "the file the child actually wrote"),
		claimDecision("I rewrote mine/ghost.txt and ran the whole suite"),
		claimDecision("the child reported"),
	})

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	reports := spawn.Reports()
	if len(reports) != 1 {
		t.Fatalf("the parent holds %d reports, want one", len(reports))
	}
	report := reports[0]
	if len(report.Wrote) != 1 || report.Wrote[0] != "mine/real.txt" {
		t.Fatalf("wrote reads %v, and the only write that happened was mine/real.txt", report.Wrote)
	}
	if len(report.Ran) != 1 || report.Ran[0].Tool != "write" {
		t.Fatalf("ran reads %+v, want the one write call", report.Ran)
	}
	if report.State != "in_review" || report.Outcome != OutcomeStopped {
		t.Fatalf("the report state is %q and the outcome %s", report.State, report.Outcome)
	}
	if report.Mission != "write the greeting under mine/" || len(report.Owns) != 1 || report.Owns[0] != "mine/**" {
		t.Fatalf("the report does not carry the contract the child was given: %+v", report)
	}
	if !strings.Contains(report.Prose, "ghost.txt") {
		t.Fatalf("the child's claim did not reach the parent as prose: %q", report.Prose)
	}
	stripped := report
	stripped.Prose = ""
	if fields, err := json.Marshal(stripped); err != nil || strings.Contains(string(fields), "ghost.txt") {
		t.Fatalf("a claim the child only made in prose reached a field the parent reads: %s %v", fields, err)
	}
	t.Logf("report by field: %+v", report)
}

func TestATurnStartedInsideAnotherTofuTurnIsRefusedAtTheBound(t *testing.T) {
	t.Setenv(subAgentDepthVar, strconv.Itoa(shippedSubAgentProcessDepth+1))

	_, err := Run(context.Background(), baseConfig(t, &stubModel{decisions: []llm.Decision{messageDecision()}}, NewRegistry()))

	var refused ProcessDepthLimitError
	if !errors.As(err, &refused) {
		t.Fatalf("a turn %d processes deep ran anyway: %v", shippedSubAgentProcessDepth+1, err)
	}
	if refused.Limit != shippedSubAgentProcessDepth {
		t.Fatalf("the refusal names the limit %d, want %d", refused.Limit, shippedSubAgentProcessDepth)
	}
	t.Logf("refused: %v", refused)
}

func TestATurnAtTheBoundStillRuns(t *testing.T) {
	t.Setenv(subAgentDepthVar, strconv.Itoa(shippedSubAgentProcessDepth))

	row, err := Run(context.Background(), baseConfig(t, &stubModel{decisions: []llm.Decision{messageDecision()}}, NewRegistry()))
	if err != nil {
		t.Fatalf("a turn at the bound was refused: %v", err)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("the turn ended %s", row.Outcome)
	}
}

func TestTheBashToolTellsEveryProcessItStartsHowDeepItIs(t *testing.T) {
	bash, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Skipf("no posix shell here: %v", err)
	}
	read := func() string {
		result, err := bash.Run(context.Background(), json.RawMessage(`{"command":"echo depth=$`+subAgentDepthVar+`"}`))
		if err != nil {
			t.Fatalf("running the probe: %v", err)
		}
		return strings.TrimSpace(result.Content)
	}

	if top := read(); top != "depth=1" {
		t.Fatalf("a command started by a top level turn reads %q, want depth=1", top)
	}
	t.Setenv(subAgentDepthVar, "1")
	if nested := read(); nested != "depth=2" {
		t.Fatalf("a command started one process deep reads %q, want depth=2", nested)
	}
}
