package recall

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	benchapi "tofu/bench/api"
	"tofu/internal/judge/jev"
	"tofu/internal/sys"
)

const forkRepoRoot = "../.."

func liveJevWire(t *testing.T) jev.Wire {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the last word to the real jev route")
	}
	sys.AllowLiveCredential(t)
	key, err := jev.Key(filepath.Join(forkRepoRoot, ".env"))
	if err != nil {
		t.Skipf("no OPENROUTER_KEY reachable: %v", err)
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		t.Fatalf("NewWire: %v", err)
	}
	return wire
}

func TestJevChoosesTheLastWordAndTwoRunsAgreeOnTheSameCase(t *testing.T) {
	wire := liveJevWire(t)
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	var multiLine *ForkCase
	for i := range cases {
		if len(lastWordCandidates(cases[i].LastWord)) > 1 {
			multiLine = &cases[i]
			break
		}
	}
	if multiLine == nil {
		t.Fatal("no real fork carries more than one candidate line, so there is nothing for a choice question to choose between")
	}

	first, err := JevLastWord(context.Background(), wire, multiLine.Task, multiLine.LastWord)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := JevLastWord(context.Background(), wire, multiLine.Task, multiLine.LastWord)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	t.Logf("case %s->%s, build %s: run 1 chose %q in %d ms at $%.6f; run 2 chose %q in %d ms at $%.6f; agree=%v",
		multiLine.From, multiLine.Into, first.Build,
		first.Chosen, first.LatencyMS, first.CostUSD,
		second.Chosen, second.LatencyMS, second.CostUSD,
		first.Chosen == second.Chosen)
	t.Logf("the fork itself blocked %d microseconds building its whole carry; one jev call over the last word costs %d milliseconds and $%.6f",
		multiLine.BlockedMicros, first.LatencyMS, first.CostUSD)
}

func TestJevLastWordAgainstDistillationOnTheRealTaskFork(t *testing.T) {
	wire := liveJevWire(t)
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	var work *ForkCase
	for i := range cases {
		if cases[i].From == "turn-18d6f8d9e45f8efc" {
			work = &cases[i]
		}
	}
	if work == nil {
		t.Fatal("the real task fork is not in the corpus")
	}
	chosen, err := JevLastWord(context.Background(), wire, work.Task, work.LastWord)
	if err != nil {
		t.Fatalf("JevLastWord: %v", err)
	}
	t.Logf("task %q\nraw       : %q\ndistilled : %q\njev chose : %q (build %s, %d ms, $%.6f)",
		work.Task, work.LastWord, DistilLastWord(work.LastWord), chosen.Chosen, chosen.Build, chosen.LatencyMS, chosen.CostUSD)
}
