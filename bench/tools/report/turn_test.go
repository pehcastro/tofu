package report

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	toolscorpus "tofu/bench/tools/corpus"
	"tofu/bench/tools/turn"
)

const turnSampleSize = 6

func turnSample(qs []toolscorpus.Asked) []toolscorpus.Asked {
	byBand := map[string][]toolscorpus.Asked{}
	for _, q := range qs {
		byBand[q.Band] = append(byBand[q.Band], q)
	}
	perBand := turnSampleSize / 3
	var sample []toolscorpus.Asked
	for _, band := range []string{toolscorpus.BandNamed, toolscorpus.BandDescribed, toolscorpus.BandIntent} {
		pool := byBand[band]
		step := len(pool) / perBand
		if step == 0 {
			step = 1
		}
		taken := 0
		for i := 0; i < len(pool) && taken < perBand; i += step {
			sample = append(sample, pool[i])
			taken++
		}
	}
	return sample
}

type turnTally struct {
	arm     string
	hits    int
	total   int
	tokens  int64
	cost    float64
	calls   int
	skips   int
	elapsed []time.Duration
}

func (a *turnTally) add(o turn.Outcome) {
	a.total++
	if o.Skipped != "" {
		a.skips++
		return
	}
	if o.Hit {
		a.hits++
	}
	a.tokens += int64(o.TotalTokens())
	a.cost += o.Cost
	a.calls += o.ToolCalls
	a.elapsed = append(a.elapsed, o.Elapsed)
}

func (a *turnTally) median() time.Duration {
	if len(a.elapsed) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), a.elapsed...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func (a *turnTally) log(t *testing.T) {
	avgTokens := int64(0)
	if a.total-a.skips > 0 {
		avgTokens = a.tokens / int64(a.total-a.skips)
	}
	t.Logf("%-14s %2d/%-2d correct  median wall clock %10s  avg total tokens %8d  cost $%.6f  avg tool calls %d  skips %d",
		a.arm, a.hits, a.total, a.median().Round(time.Second), avgTokens, a.cost, a.calls, a.skips)
}

func TestTurnAgainstTurn(t *testing.T) {
	t.Log("this table compares whole turns: tofu run against claude -p and codex exec, each searching, reading and deciding on its own. It is not the tool table below and the two are never mixed.")
	if os.Getenv("TOFU_TOOLS_VENDOR") != "1" {
		t.Skip("set TOFU_TOOLS_VENDOR=1 to drive tofu run, claude -p and codex exec from their own command lines, spending subscription quota")
	}
	questions := loadQuestions(t)
	sample := turnSample(questions)
	t.Logf("turn table ran on %d of %d questions: a full turn per vendor is slow and this ticket's session budget does not cover all %d three times over", len(sample), len(questions), len(questions))

	tofuArm := &turnTally{arm: "tofu run"}
	codexArm := &turnTally{arm: "codex exec"}
	claudeArm := &turnTally{arm: "claude -p"}

	for _, q := range sample {
		dir := toolscorpus.TreeRoot(tofuRoot, q.Tree)
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		tofuOut := turn.RunTofu(ctx, dir, "codex", q)
		cancel()
		tofuArm.add(tofuOut)
		t.Logf("tofu run   %s hit=%v elapsed=%s tokens=%d skipped=%q", q.ID, tofuOut.Hit, tofuOut.Elapsed.Round(time.Second), tofuOut.TotalTokens(), tofuOut.Skipped)

		ctx, cancel = context.WithTimeout(context.Background(), 180*time.Second)
		codexOut := turn.RunCodex(ctx, dir, q)
		cancel()
		codexArm.add(codexOut)
		t.Logf("codex exec %s hit=%v elapsed=%s tokens=%d skipped=%q", q.ID, codexOut.Hit, codexOut.Elapsed.Round(time.Second), codexOut.TotalTokens(), codexOut.Skipped)

		ctx, cancel = context.WithTimeout(context.Background(), 180*time.Second)
		claudeOut := turn.RunClaude(ctx, dir, q)
		cancel()
		claudeArm.add(claudeOut)
		t.Logf("claude -p  %s hit=%v elapsed=%s tokens=%d skipped=%q", q.ID, claudeOut.Hit, claudeOut.Elapsed.Round(time.Second), claudeOut.TotalTokens(), claudeOut.Skipped)
	}

	for _, a := range []*turnTally{tofuArm, codexArm, claudeArm} {
		a.log(t)
	}
}
