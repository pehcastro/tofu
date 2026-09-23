package turn

import (
	"context"
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

	if review.reviewed != 1 {
		t.Fatalf("the judged arm ran %d times", review.reviewed)
	}
	if len(children) != 2 || children[1].ID != "turn-parent-c1-r" {
		t.Fatalf("the reopen verdict did not reopen the child: %d rows", len(children))
	}
	if !strings.Contains(children[1].Task, "work_remains 0.93") {
		t.Fatalf("the reopened child was not told the answer that reopened it: %q", children[1].Task)
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
