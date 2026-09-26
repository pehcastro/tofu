package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/turn/tools"
)

func sessionsDir() string { return sys.RecordedStateDir("sessions") }

const repoRoot = "../.."
const replayFailuresInTheReport = 6

func replayed(t *testing.T) Result {
	t.Helper()
	result, err := Run(sessionsDir(), repoRoot)
	if errors.Is(err, ErrNoPinnedTree) {
		t.Skipf("skip, named: commit %s is not in this checkout: %v", PinnedCommit, err)
	}
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir(), err)
	}
	return result
}

func TestByteTotalsOverTheRealCorpus(t *testing.T) {
	result := replayed(t)
	if result.Turns == 0 {
		t.Fatal("no turn was read: the sessions directory is empty or the path is wrong")
	}
	if len(result.Totals) == 0 {
		t.Fatal("no tool carried a byte total")
	}
	t.Log(Render(result))
}

func TestGlobStarOnThePinnedTreeReturnsACappedList(t *testing.T) {
	tree, remove, err := pinnedTree(repoRoot)
	if errors.Is(err, ErrNoPinnedTree) {
		t.Skipf("skip, named: commit %s is not in this checkout: %v", PinnedCommit, err)
	}
	if err != nil {
		t.Fatalf("extracting %s: %v", PinnedCommit, err)
	}
	defer remove()
	glob, err := tools.NewGlob(tree)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := glob.Run(context.Background(), json.RawMessage(`{"pattern":"*"}`))
	if err != nil {
		t.Fatalf("glob *: %v", err)
	}
	t.Log(result.Content)
}

func TestEveryRecordedGlobCallIsCountedAndNoneReturnsMoreThanTheCap(t *testing.T) {
	result := replayed(t)
	if len(result.GlobCalls) == 0 {
		t.Fatal("no glob call was counted: the report cannot give a path count per call")
	}
	for _, call := range result.GlobCalls {
		if call.Returned > konst.GlobPathsResultCap {
			t.Fatalf("%s step %d %s: %d paths returned against a cap of %d", call.Turn, call.Step, call.Args, call.Returned, konst.GlobPathsResultCap)
		}
		if call.Returned > call.Matched {
			t.Fatalf("%s step %d %s: %d returned out of %d matched", call.Turn, call.Step, call.Args, call.Returned, call.Matched)
		}
	}
}

func TestTheGlobCapNeverRaisesAToolsByteTotal(t *testing.T) {
	result := replayed(t)
	for _, total := range result.Totals {
		if total.AfterBytes > total.BeforeBytes {
			t.Fatalf("%s: after cap %d bytes, before cap %d bytes: the cap must never grow a result", total.Tool, total.AfterBytes, total.BeforeBytes)
		}
	}
}

func TestTheCallsThatCannotReplayAreTheCountTheReportStates(t *testing.T) {
	result := replayed(t)
	if result.ReplayFailures != replayFailuresInTheReport {
		t.Fatalf("%d glob calls cannot replay against %s, and the report states %d",
			result.ReplayFailures, PinnedCommit, replayFailuresInTheReport)
	}
}

func TestTwoReplaysOfThePinnedTreeGiveTheSamePathCounts(t *testing.T) {
	first, second := replayed(t), replayed(t)
	if len(first.GlobCalls) != len(second.GlobCalls) {
		t.Fatalf("%d glob calls on the first replay and %d on the second", len(first.GlobCalls), len(second.GlobCalls))
	}
	for i, call := range first.GlobCalls {
		other := second.GlobCalls[i]
		if call.Matched != other.Matched || call.Returned != other.Returned {
			t.Fatalf("%s step %d %s: %d matched and %d returned on the first replay, %d and %d on the second",
				call.Turn, call.Step, call.Args, call.Matched, call.Returned, other.Matched, other.Returned)
		}
	}
}
