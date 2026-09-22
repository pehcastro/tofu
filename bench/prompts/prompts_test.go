package prompts

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"tofu/bench/corpus"
)

const sessionsDir = "../../.tofu/sessions"

func call(tool string, rendered int64, args map[string]string) corpus.RecordedCall {
	encoded, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return corpus.RecordedCall{Tool: tool, RenderedBytes: rendered, Args: encoded}
}

func bash(command string, rendered int64, exit int) corpus.RecordedCall {
	return corpus.RecordedCall{Tool: "bash", Command: command, RenderedBytes: rendered, ExitCode: &exit}
}

func turnOf(calls ...corpus.RecordedCall) corpus.Turn {
	return corpus.Turn{RecordedTurn: corpus.RecordedTurn{
		ID:    "turn-fixture",
		Task:  "add a route and a test for it",
		Steps: []corpus.RecordedStep{{Index: 1, ToolCalls: calls}},
	}}
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s: want %.9f, got %.9f", name, want, got)
	}
}

func TestMeasureOverAHandBuiltSession(t *testing.T) {
	path := map[string]string{"path": "internal/a.go"}
	session := measure(turnOf(
		call("read", 4000, path),
		call("glob", 400, map[string]string{"pattern": "*.go"}),
		bash("cat internal/a.go | head -40", 800, 0),
		bash("go test ./internal/...", 200, 1),
		call("write", 0, path),
		call("write", 0, path),
		call("write", 0, map[string]string{"path": "internal/b.go"}),
	))
	if session.ReadCalls != 3 {
		t.Fatalf("read calls: want 3, got %d", session.ReadCalls)
	}
	if session.ReadTokens != (4000+400+800)/4 {
		t.Fatalf("read tokens: want %d, got %d", (4000+400+800)/4, session.ReadTokens)
	}
	if session.Retreats != 1 {
		t.Fatalf("retreats: want 1, got %d", session.Retreats)
	}
	if session.Contradictions != 1 {
		t.Fatalf("contradictions: want 1, got %d", session.Contradictions)
	}
	if session.WrongDirections() != 2 {
		t.Fatalf("wrong directions: want 2, got %d", session.WrongDirections())
	}
	if session.Calls != 7 {
		t.Fatalf("calls: want 7, got %d", session.Calls)
	}
}

func TestMeasureFallsBackToResultBytes(t *testing.T) {
	session := measure(turnOf(corpus.RecordedCall{Tool: "read", ResultBytes: 2000}))
	if session.ReadTokens != 500 {
		t.Fatalf("read tokens: want 500, got %d", session.ReadTokens)
	}
}

func TestPearsonOnHandComputedPairs(t *testing.T) {
	near(t, "perfectly rising", pearson([]float64{1, 2, 3, 4}, []float64{2, 4, 6, 8}), 1)
	near(t, "perfectly falling", pearson([]float64{1, 2, 3, 4}, []float64{8, 6, 4, 2}), -1)
	near(t, "one swap", pearson([]float64{1, 2, 3, 4}, []float64{1, 3, 2, 4}), 0.8)
	if got := pearson([]float64{1, 1, 1}, []float64{1, 2, 3}); !math.IsNaN(got) {
		t.Fatalf("a constant column has no correlation: got %.3f", got)
	}
	if got := pearson([]float64{1}, []float64{1}); !math.IsNaN(got) {
		t.Fatalf("one pair has no correlation: got %.3f", got)
	}
}

func TestSpearmanSharesRanksAcrossTies(t *testing.T) {
	ranked := ranks([]float64{1, 1, 4, 3})
	for i, want := range []float64{1.5, 1.5, 4, 3} {
		near(t, "rank", ranked[i], want)
	}
	near(t, "spearman", spearman([]float64{1, 2, 3, 4}, []float64{1, 1, 4, 3}), 3.5/math.Sqrt(5*4.5))
}

func TestCrossCountsTheFourCells(t *testing.T) {
	sessions := []Session{
		{ID: "a", Task: "one", ReadTokens: 20000, Retreats: 1},
		{ID: "b", Task: "two", ReadTokens: 20000},
		{ID: "c", Task: "three", ReadTokens: 100, Contradictions: 2},
		{ID: "d", Task: "four", ReadTokens: 100},
		{ID: "e", Task: "one", ReadTokens: 10000, Retreats: 1},
	}
	table := cross(sessions, headlineThresholdTokens)
	if table.VolumeAndWrong != 2 || table.VolumeOnly != 1 || table.WrongOnly != 1 || table.Neither != 1 {
		t.Fatalf("cells: got both %d, volume %d, wrong %d, neither %d",
			table.VolumeAndWrong, table.VolumeOnly, table.WrongOnly, table.Neither)
	}
	if table.DistinctBoth != 1 {
		t.Fatalf("two sessions in the both cell share one task text: want 1, got %d", table.DistinctBoth)
	}
	near(t, "phi", table.Phi, 1.0/6.0)
}

func TestSeparableArithmetic(t *testing.T) {
	entry := separable("fixture", []int64{0, 0, 4, 4})
	near(t, "mean", entry.Mean, 2)
	near(t, "sd", entry.SD, 2)
	near(t, "difference", entry.Difference, 4*math.Sqrt(2.0/armSessions))
}

func TestWithoutTheWorstDropsOneSession(t *testing.T) {
	kept := withoutTheWorst([]Session{
		{ID: "a", Retreats: 1},
		{ID: "b", Retreats: 9},
		{ID: "c"},
	})
	if len(kept) != 2 || kept[0].ID != "a" || kept[1].ID != "c" {
		t.Fatalf("want a and c, got %v", kept)
	}
}

func TestLeakOfNamesTheTerm(t *testing.T) {
	if got := leakOf("did the benchmark had any results?"); got != "bench" {
		t.Fatalf("want bench, got %q", got)
	}
	if got := leakOf("add a route and a test for it"); got != "" {
		t.Fatalf("want no leak, got %q", got)
	}
}

func TestReportOverTheRealCorpus(t *testing.T) {
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("skipped, named: the recorded corpus is not on this machine at %s", sessionsDir)
	}
	result, err := Run(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) == 0 {
		t.Fatal("the corpus read as zero sessions")
	}
	t.Log("\n" + Render(result))
}
