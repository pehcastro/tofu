package wrongpath

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

const sessionsDir = "../../.tofu/sessions"

func call(tool string, args any) corpus.RecordedCall {
	encoded, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return corpus.RecordedCall{Tool: tool, Args: encoded}
}

func exited(code int) corpus.RecordedCall {
	return corpus.RecordedCall{Tool: "bash", ExitCode: &code}
}

func TestCountOverHandBuiltSequences(t *testing.T) {
	cases := []struct {
		name  string
		calls []corpus.RecordedCall
		want  Counts
	}{
		{"one read of one path is nothing",
			[]corpus.RecordedCall{call("read", callArgs{Path: "a.go"})},
			Counts{}},
		{"the same path read twice whole is a re-read and an identical one",
			[]corpus.RecordedCall{call("read", callArgs{Path: "a.go"}), call("read", callArgs{Path: "./a.go"})},
			Counts{ReReads: 1, IdenticalReReads: 1}},
		{"disjoint line ranges are not a re-read",
			[]corpus.RecordedCall{
				call("read", callArgs{Path: "a.go", StartLine: 1, EndLine: 40}),
				call("read", callArgs{Path: "a.go", StartLine: 41, EndLine: 80}),
			},
			Counts{}},
		{"overlapping line ranges are a re-read but not an identical one",
			[]corpus.RecordedCall{
				call("read", callArgs{Path: "a.go", StartLine: 1, EndLine: 40}),
				call("read", callArgs{Path: "a.go", StartLine: 30, EndLine: 80}),
			},
			Counts{ReReads: 1}},
		{"an edit between two reads clears the re-read",
			[]corpus.RecordedCall{
				call("read", callArgs{Path: "a.go"}),
				call("edit", callArgs{Path: "a.go", OldString: "x", NewString: "y"}),
				call("read", callArgs{Path: "a.go"}),
			},
			Counts{}},
		{"a second write to the same path is a revert",
			[]corpus.RecordedCall{
				call("write", callArgs{Path: "a.go", Content: "one"}),
				call("write", callArgs{Path: "a.go", Content: "two"}),
			},
			Counts{Reverts: 1}},
		{"an edit that swaps an earlier edit back is an exact undo",
			[]corpus.RecordedCall{
				call("edit", callArgs{Path: "a.go", OldString: "x", NewString: "y"}),
				call("edit", callArgs{Path: "a.go", OldString: "y", NewString: "x"}),
			},
			Counts{Reverts: 1, ExactUndos: 1}},
		{"a failure after a success is a contradiction",
			[]corpus.RecordedCall{exited(0), exited(1)},
			Counts{Contradictions: 1}},
		{"a run of failures is one contradiction",
			[]corpus.RecordedCall{exited(0), exited(1), exited(2), exited(1)},
			Counts{Contradictions: 1}},
		{"the first call failing contradicts nothing",
			[]corpus.RecordedCall{exited(1), exited(0)},
			Counts{}},
		{"an error string counts as a failure",
			[]corpus.RecordedCall{{Tool: "read"}, {Tool: "read", Error: "no such file"}},
			Counts{Contradictions: 1}},
	}
	for _, c := range cases {
		if got := count(c.calls); got != c.want {
			t.Errorf("%s: Count = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestMeasureOverAKnownSeries(t *testing.T) {
	spread := measure([]int{0, 0, 1, 2, 3, 10})
	if spread.Sum != 16 || spread.Zeros != 2 || spread.Worst != 10 || spread.Range != 10 {
		t.Fatalf("%+v", spread)
	}
	if spread.Median != 1.5 {
		t.Fatalf("median %v, want 1.5", spread.Median)
	}
}

const turnsGainedFromSplitRestarts = 7

func TestEveryCorpusEntryIsEitherASessionOrANamedSkip(t *testing.T) {
	result, err := Run(sessionsDir)
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir, err)
	}
	if result.Sessions == 0 {
		t.Fatal("no session was read: the path is wrong or the corpus is empty")
	}
	if result.Sessions+len(result.Skips) != result.Entries+turnsGainedFromSplitRestarts {
		t.Fatalf("%d entries, %d sessions and %d skips: an entry went unaccounted for beyond the %d extra turns a restarted session directory now splits into", result.Entries, result.Sessions, len(result.Skips), turnsGainedFromSplitRestarts)
	}
	for _, skip := range result.Skips {
		if skip.Reason == "" {
			t.Fatalf("%s was skipped with no reason", skip.Path)
		}
	}
}

func TestNoStrictVariantExceedsItsLooseOne(t *testing.T) {
	result, err := Run(sessionsDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, row := range result.Rows {
		if row.Counts.ExactUndos > row.Counts.Reverts {
			t.Fatalf("%s: %d exact undos over %d reverts", row.ID, row.Counts.ExactUndos, row.Counts.Reverts)
		}
		if row.Counts.IdenticalReReads > row.Counts.ReReads {
			t.Fatalf("%s: %d identical re-reads over %d re-reads", row.ID, row.Counts.IdenticalReReads, row.Counts.ReReads)
		}
		if total(row.Counts) > row.Calls*2 {
			t.Fatalf("%s: %d shapes counted over %d calls", row.ID, total(row.Counts), row.Calls)
		}
	}
}

func TestReportOverTheRealCorpus(t *testing.T) {
	result, err := Run(sessionsDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rendered := Render(result)
	for _, want := range []string{"the three shapes over every session", "false positive", "could this separate two arms", "bench/corpus.WalkSessions"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the report carries no %q section", want)
		}
	}
	t.Log("\n" + rendered)
}
