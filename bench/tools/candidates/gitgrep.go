package candidates

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"tofu/bench/tools"
	toolscorpus "tofu/bench/tools/corpus"
)

const gitGrepTimeout = 30 * time.Second

func GitGrep(root, pattern, tree string) ([]string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitGrepTimeout)
	defer cancel()
	args := []string{"-C", root, "grep"}
	if tree != "" && tree != toolscorpus.TreeTofu {
		args = append(args, "--no-index")
	}
	args = append(args, "-nE", pattern)
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		err = nil
	}
	if err != nil {
		return nil, 0, err
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, len(out), nil
	}
	return strings.Split(trimmed, "\n"), len(out), nil
}

func hitsOne(lines []string, a toolscorpus.Answer) bool {
	want := a.File + ":" + strconv.Itoa(a.Line) + ":"
	for _, l := range lines {
		if strings.HasPrefix(l, want) {
			return true
		}
	}
	return false
}

func HitsTarget(lines []string, q toolscorpus.Asked) bool {
	answers := q.AllAnswers()
	if len(answers) == 0 {
		return false
	}
	if q.ScoreRule == toolscorpus.ScoreAll {
		for _, a := range answers {
			if !hitsOne(lines, a) {
				return false
			}
		}
		return true
	}
	for _, a := range answers {
		if hitsOne(lines, a) {
			return true
		}
	}
	return false
}

func Realistic(root string, q toolscorpus.Asked) tools.Outcome {
	return runPattern("git grep realistic", root, tools.RealisticPattern(q.Text), q)
}

func Quoted(root string, q toolscorpus.Asked) tools.Outcome {
	terms := tools.QuotedTerms(q.Text)
	if len(terms) == 0 {
		return tools.Outcome{Version: "git grep quoted", Skipped: "the question carries no backtick quoted term"}
	}
	return runPattern("git grep quoted", root, tools.Alternation(terms), q)
}

func runPattern(version, root, pattern string, q toolscorpus.Asked) tools.Outcome {
	started := time.Now()
	lines, bytes, err := GitGrep(root, pattern, q.Tree)
	elapsed := time.Since(started)
	if err != nil {
		return tools.Outcome{Version: version, Skipped: err.Error(), Calls: 1, Elapsed: elapsed}
	}
	return tools.Outcome{
		Version: version,
		Hit:     HitsTarget(lines, q),
		Bytes:   bytes,
		Calls:   1,
		Elapsed: elapsed,
	}
}
