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

func reviewedSubAgent(t *testing.T, named string) (*stubReview, []Row) {
	t.Helper()
	root := t.TempDir()
	review := &stubReview{writer: ledger.NewWriter(filepath.Join(root, "ledger")), verdict: DoneReopen}
	_, spawn := orchestratorTurn(t, root, []llm.Decision{
		claimDecision("all done"),
		claimDecision("done again"),
		claimDecision("done a third time"),
	})
	spawn.Review = review
	spawn.Methods = methodTable(t, named)
	spawnGreeting(t, spawn)
	return review, spawn.SubAgentRows()
}

func spawnGreeting(t *testing.T, spawn *SpawnTool) {
	t.Helper()
	if _, err := spawn.Run(context.Background(), spawnCall("call-1", "write the greeting under mine/", "mine/**").ToolCalls[0].Arguments); err != nil {
		t.Fatalf("spawn returned an error: %v", err)
	}
	reported(t, spawn)
}

func TestTheTableSendingStopCheckToTheJudgedMethodActsOnTheAnswer(t *testing.T) {
	review, subAgents := reviewedSubAgent(t, string(method.Judged))

	if review.reviewed != 3 {
		t.Fatalf("the judged arm ran %d times, want 3: a review that always reopens should run out against the round cap, not stop early", review.reviewed)
	}
	if len(subAgents) != 3 || subAgents[1].ID != "sub-1-r2" || subAgents[2].ID != "sub-1-r3" {
		t.Fatalf("the reopen verdict did not carry the sub-agent to the round cap: %d rows", len(subAgents))
	}
	if !strings.Contains(subAgents[1].Task, "work_remains 0.93") {
		t.Fatalf("the reopened sub-agent was not told the answer that reopened it: %q", subAgents[1].Task)
	}
	warned := strings.Join(subAgents[2].Warnings, " ")
	if !strings.Contains(warned, "round cap") {
		t.Fatalf("the fourth round was not refused with the cap named: %q", warned)
	}
}

func TestAnUnwiredStopCheckLeavesTheSubAgentsOwnClaimStanding(t *testing.T) {
	review, subAgents := reviewedSubAgent(t, string(method.Unwired))

	if review.reviewed != 0 {
		t.Fatalf("an unwired point still asked: %d times", review.reviewed)
	}
	if len(subAgents) != 1 {
		t.Fatalf("an unwired point reopened the sub-agent: %d rows", len(subAgents))
	}
	warned := strings.Join(subAgents[0].Warnings, " ")
	if !strings.Contains(warned, "the sub-agent's own claim stands") || !strings.Contains(warned, "unwired") {
		t.Fatalf("the sub-agent does not say why nothing reviewed it: %q", warned)
	}
	t.Log(warned)
}

func TestTheStopCheckMethodComesFromTheShippedTableWhenNobodyPassesOne(t *testing.T) {
	root := t.TempDir()
	review := &stubReview{writer: ledger.NewWriter(filepath.Join(root, "ledger")), verdict: DoneAccepted}
	_, spawn := orchestratorTurn(t, root, []llm.Decision{claimDecision("all done")})
	spawn.Review = review
	spawnGreeting(t, spawn)

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
	if held := onlySubAgent(t, spawn); held.State != subagent.Finished {
		t.Fatalf("the accepted verdict left the sub-agent %s", held.State)
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

func orchestratorTurn(t *testing.T, root string, decisions []llm.Decision) (Config, *SpawnTool) {
	t.Helper()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the write tool: %v", err)
	}
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}
	const orchestratorID = "turn-orchestrator"
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(read, write),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return orchestratorID },
	}
	spawn := NewSpawnTool(orchestratorID, base, &subagent.Roster{})
	orchestrator := base
	orchestrator.Task = "hand the work to a sub-agent"
	orchestrator.Tools, orchestrator.Inbox = NewRegistry(read, write, spawn), spawn.Inbox
	return orchestrator, spawn
}

func onlySubAgent(t *testing.T, spawn *SpawnTool) subagent.SubAgent {
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

func (r *stubReview) Review(_ context.Context, subAgent Row) (DoneDecision, error) {
	r.reviewed++
	row, err := r.writer.Append(ledger.Row{
		Point:     "stop_check@1",
		Questions: "stop_check",
		Version:   1,
		Build:     "jev-test",
		Verdict:   ledger.VerdictAsk,
		TurnID:    subAgent.ID,
		Answers: []ledger.Answer{{
			Question: "work_remains", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.93,
		}},
	})
	if err != nil {
		return DoneDecision{}, err
	}
	return DoneDecision{ID: row.ID, Verdict: r.verdict, Reason: "work_remains 0.93"}, nil
}
