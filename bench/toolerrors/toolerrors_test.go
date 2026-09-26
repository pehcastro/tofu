package toolerrors

import (
	"strings"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/sys"
)

func sessionsDir() string { return sys.RecordedStateDir("sessions") }

func exited(code int) corpus.RecordedCall {
	return corpus.RecordedCall{Tool: "bash", ExitCode: &code}
}

func TestFailedMatchesTheExitCodeAndErrorField(t *testing.T) {
	cases := []struct {
		name string
		call corpus.RecordedCall
		want bool
	}{
		{"a zero exit is not a failure", exited(0), false},
		{"a nonzero exit is a failure", exited(1), true},
		{"no exit code and no error is not a failure", corpus.RecordedCall{Tool: "read"}, false},
		{"an error string is a failure with no exit code", corpus.RecordedCall{Tool: "read", Error: "not found"}, true},
	}
	for _, c := range cases {
		if got := Failed(c.call); got != c.want {
			t.Errorf("%s: Failed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClassifyMatchesTheRealCorpusTextsSeenSoFar(t *testing.T) {
	cases := []struct {
		name string
		call corpus.RecordedCall
		want Category
	}{
		{"no error text at all", exited(1), Unknown},
		{"the bash deadline message", corpus.RecordedCall{Tool: "bash", Error: `bash: degraded stopped: the command was killed at its deadline and its output is gone. "`}, Timeout},
		{"a read that names no file", corpus.RecordedCall{Tool: "read", Error: "read: internal/judge/policy/types.go is not a file under the working directory, and nothing there is named types.go: nothing was run"}, NotFoundInEnvironment},
		{"a search over an ambiguous path", corpus.RecordedCall{Tool: "search", Error: "search: internal/policy is not a path under the working directory, and 2 of them are named policy: catalog/policy, internal/judge/policy. name the one you mean, because a repair is only made when it is the only candidate: nothing was run"}, BadArguments},
		{"an edit anchor that matches no line", corpus.RecordedCall{Tool: "edit", Error: `edit: edit 1 of 1 on a.go: anchor "x" matches no line in the file`}, BadArguments},
		{"an edit anchor that matches several lines", corpus.RecordedCall{Tool: "edit", Error: `edit: edit 1 of 1 on a.go: until "}" matches 22 lines (1, 2, 3)`}, BadArguments},
		{"an edit occurrence beyond what the file holds", corpus.RecordedCall{Tool: "edit", Error: `edit: edit 1 of 1 on a.go: until "x" was asked for occurrence 2 and the file holds 1`}, BadArguments},
		{"a bash call cancelled by its context", corpus.RecordedCall{Tool: "bash", Error: "bash: context canceled"}, Timeout},
		{"a made up refusal text", corpus.RecordedCall{Tool: "bash", Error: "refused by policy: no network access"}, RefusedByRule},
		{"a made up provider text", corpus.RecordedCall{Tool: "bash", Error: "provider returned a 503"}, ProviderError},
		{"an error with no matching phrase", corpus.RecordedCall{Tool: "bash", Error: "something else went wrong"}, Unknown},
	}
	for _, c := range cases {
		if got := Classify(c.call); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestABareExitCodeStillRecordsWhyRatherThanUnknown(t *testing.T) {
	cases := []struct {
		name string
		code int
		want Category
	}{
		{"exit 127 with no error text", 127, NotFoundInEnvironment},
		{"exit 126 with no error text", 126, NotFoundInEnvironment},
	}
	for _, c := range cases {
		if got := Classify(exited(c.code)); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRunCountsCallsAndFailuresPerTool(t *testing.T) {
	result, err := Run(sessionsDir())
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir(), err)
	}
	if result.Sessions == 0 {
		t.Fatal("no session was read: the path is wrong or the corpus is empty")
	}
	if result.Calls == 0 {
		t.Fatal("no tool call was read out of a nonempty corpus")
	}
	sumCalls, sumFailures := 0, 0
	for _, row := range result.Tools {
		sumCalls += row.Calls
		sumFailures += row.Failures
		if row.Failures > row.Calls {
			t.Fatalf("%s: %d failures over %d calls", row.Tool, row.Failures, row.Calls)
		}
		categorySum := 0
		for _, n := range row.ByCategory {
			categorySum += n
		}
		if categorySum != row.Failures {
			t.Fatalf("%s: category counts sum to %d, failures is %d", row.Tool, categorySum, row.Failures)
		}
	}
	if sumCalls != result.Calls {
		t.Fatalf("tool rows sum to %d calls, Result.Calls is %d", sumCalls, result.Calls)
	}
	if sumFailures != result.Failures {
		t.Fatalf("tool rows sum to %d failures, Result.Failures is %d", sumFailures, result.Failures)
	}
	categoryTotal := 0
	for _, n := range result.ByCategory {
		categoryTotal += n
	}
	if categoryTotal != result.Failures {
		t.Fatalf("categories sum to %d, Result.Failures is %d", categoryTotal, result.Failures)
	}
}

func TestWorstToolHasAtLeastTheCallFloor(t *testing.T) {
	result, err := Run(sessionsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.WorstFound {
		t.Fatal("no tool reached the minimum call count, so no worst tool was named")
	}
	if result.Worst.Calls < minCallsForWorst {
		t.Fatalf("worst tool %s has %d calls, under the floor of %d", result.Worst.Tool, result.Worst.Calls, minCallsForWorst)
	}
	for _, row := range result.Tools {
		if row.Calls < minCallsForWorst {
			continue
		}
		if row.Rate() > result.Worst.Rate() {
			t.Fatalf("%s has a higher failure rate (%.3f) than the reported worst %s (%.3f)", row.Tool, row.Rate(), result.Worst.Tool, result.Worst.Rate())
		}
	}
	if result.Worst.Example.Tool != result.Worst.Tool {
		t.Fatalf("worst case example is for %s, worst tool is %s", result.Worst.Example.Tool, result.Worst.Tool)
	}
}

func TestReportOverTheRealCorpus(t *testing.T) {
	result, err := Run(sessionsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rendered := Render(result)
	for _, want := range []string{"unknown share", "worst tool", "bench/corpus.WalkSessions"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the report carries no %q section", want)
		}
	}
	t.Log("\n" + rendered)
}
