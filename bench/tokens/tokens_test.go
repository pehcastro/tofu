package tokens

import (
	"context"
	"encoding/json"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/turn/tools"
)

const sessionsDir = "../../.tofu/sessions"
const repoRoot = "../.."

func TestByteTotalsOverTheRealCorpus(t *testing.T) {
	result, err := Run(sessionsDir, repoRoot)
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir, err)
	}
	if result.Turns == 0 {
		t.Fatal("no turn was read: the sessions directory is empty or the path is wrong")
	}
	if len(result.Totals) == 0 {
		t.Fatal("no tool carried a byte total")
	}
	t.Log(Render(result))
}

func TestGlobStarOnThisRepositoryReturnsACappedList(t *testing.T) {
	glob, err := tools.NewGlob(repoRoot)
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
	result, err := Run(sessionsDir, repoRoot)
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir, err)
	}
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
	result, err := Run(sessionsDir, repoRoot)
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir, err)
	}
	for _, total := range result.Totals {
		if total.AfterBytes > total.BeforeBytes {
			t.Fatalf("%s: after cap %d bytes, before cap %d bytes: the cap must never grow a result", total.Tool, total.AfterBytes, total.BeforeBytes)
		}
	}
}
