package candidates

import (
	"time"

	"tofu/bench/tools"
	toolscorpus "tofu/bench/tools/corpus"
)

func RipgrepRaw(root string, q toolscorpus.Question) tools.Outcome {
	return runRipgrepPattern("ripgrep raw", root, tools.RealisticPattern(q.Text), q)
}

func RipgrepQuoted(root string, q toolscorpus.Question) tools.Outcome {
	terms := tools.QuotedTerms(q.Text)
	if len(terms) == 0 {
		return tools.Outcome{Version: "ripgrep quoted", Skipped: "the question carries no quoted term"}
	}
	return runRipgrepPattern("ripgrep quoted", root, tools.Alternation(terms), q)
}

func runRipgrepPattern(version, root, pattern string, q toolscorpus.Question) tools.Outcome {
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
