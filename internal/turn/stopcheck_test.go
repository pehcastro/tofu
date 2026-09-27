package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/judge/method"
	"tofu/internal/llm"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

func reviewedChild(t *testing.T, named string) (*stubReview, []Row) {
	t.Helper()
	root := t.TempDir()
	review := &stubReview{writer: ledger.NewWriter(filepath.Join(root, "ledger")), verdict: DoneReopen}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		claimDecision("all done"),
		claimDecision("done again"),
		claimDecision("done a third time"),
		messageDecision(),
	})
	spawn.Review = review
	spawn.Methods = methodTable(t, named)

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	return review, spawn.Children()
}

func TestTheTableSendingStopCheckToTheJudgedMethodActsOnTheAnswer(t *testing.T) {
	review, children := reviewedChild(t, string(method.Judged))

	if review.reviewed != 3 {
		t.Fatalf("the judged arm ran %d times, want 3: a review that always reopens should run out against the round cap, not stop early", review.reviewed)
	}
	if len(children) != 3 || children[1].ID != "sub-1-r2" || children[2].ID != "sub-1-r3" {
		t.Fatalf("the reopen verdict did not carry the child to the round cap: %d rows", len(children))
	}
	if !strings.Contains(children[1].Task, "work_remains 0.93") {
		t.Fatalf("the reopened child was not told the answer that reopened it: %q", children[1].Task)
	}
	warned := strings.Join(children[2].Warnings, " ")
	if !strings.Contains(warned, "round cap") {
		t.Fatalf("the fourth round was not refused with the cap named: %q", warned)
	}
}

func TestAnUnwiredStopCheckLeavesTheChildsOwnClaimStanding(t *testing.T) {
	review, children := reviewedChild(t, string(method.Unwired))

	if review.reviewed != 0 {
		t.Fatalf("an unwired point still asked: %d times", review.reviewed)
	}
	if len(children) != 1 {
		t.Fatalf("an unwired point reopened the child: %d rows", len(children))
	}
	warned := strings.Join(children[0].Warnings, " ")
	if !strings.Contains(warned, "the child's own claim stands") || !strings.Contains(warned, "unwired") {
		t.Fatalf("the child does not say why nothing reviewed it: %q", warned)
	}
	t.Log(warned)
}

func TestTheStopCheckMethodComesFromTheShippedTableWhenNobodyPassesOne(t *testing.T) {
	root := t.TempDir()
	review := &stubReview{writer: ledger.NewWriter(filepath.Join(root, "ledger")), verdict: DoneAccepted}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		claimDecision("all done"),
		messageDecision(),
	})
	spawn.Review = review

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	table, err := method.Load(shipped.Files())
	if err != nil {
		t.Fatalf("loading the shipped table: %v", err)
	}
	chosen, err := table.Of("stop_check")
	if err != nil {
		t.Fatal(err)
	}
	if chosen.Method != method.Judged {
		t.Skipf("the shipped table names %s for stop_check, and this test reads the judged row", chosen.Method)
	}
	if review.reviewed != 1 {
		t.Fatalf("the shipped table says judged and the judged arm ran %d times", review.reviewed)
	}
	if held := onlyChild(t, spawn); held.State != subagent.Finished {
		t.Fatalf("the accepted verdict left the child %s", held.State)
	}
}

func methodTable(t *testing.T, named string) method.Table {
	t.Helper()
	rows := "kind: method_table\ntable_version: 1\nnotes: a fixture\nmethods:\n"
	for _, point := range []string{shellSiftPoint, "stop_check"} {
		rows += "  " + point + ":\n    method: " + named + "\n    why: a fixture\n"
		if named != string(method.Unwired) {
			rows += "    cost: nothing, it is a fixture\n"
		}
	}
	table, err := method.Parse([]byte(rows), "testdata/methods@1.yaml")
	if err != nil {
		t.Fatalf("parsing the fixture table: %v", err)
	}
	return table
}

func claimDecision(text string) llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: text}
}

func spawnCall(id, task string, owns ...string) llm.Decision {
	args, err := json.Marshal(spawnArgs{Task: task, Owns: owns})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "spawn", Arguments: args})
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
	spawn := NewSpawnTool(parentID, base, &subagent.Roster{})
	parent := base
	parent.Task = "hand the work to a child"
	parent.Tools = NewRegistry(read, write, spawn)
	return parent, spawn
}

func onlyChild(t *testing.T, spawn *SpawnTool) subagent.SubAgent {
	t.Helper()
	held := spawn.roster.SubAgents()
	if len(held) != 1 {
		t.Fatalf("the roster holds %d sub-agents, want 1", len(held))
	}
	return held[0]
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
