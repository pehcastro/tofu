package candidates

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/bench/tools"
	toolscorpus "tofu/bench/tools/corpus"
	"tofu/internal/judge/jev"
)

const (
	jevgrepAnswerLineCap = 20
	jevgrepBroadWordCap  = 3
)

func broadPattern(words []string) string {
	return tools.Alternation(words[:min(jevgrepBroadWordCap, len(words))])
}

func twoPatterns(q toolscorpus.Asked) (string, string) {
	words := tools.ContentWords(q.Text)
	broad := broadPattern(words)
	narrow := broad
	quoted := tools.QuotedTerms(q.Text)
	switch {
	case len(quoted) > 0:
		narrow = tools.Alternation(quoted)
	case len(words) > 0:
		longest := words[0]
		for _, w := range words {
			if len(w) > len(longest) {
				longest = w
			}
		}
		narrow = tools.Alternation([]string{longest})
	}
	return broad, narrow
}

func choosePattern(ctx context.Context, client *jev.Client, question, a, b string) (string, jev.Decision, error) {
	request := jev.Request{
		State: map[string]any{"question": question, "pattern_a": a, "pattern_b": b},
		Questions: []jev.Question{{
			ID:           "pick",
			Kind:         jev.QuestionChoice,
			Instructions: "which of these two go regular expressions is more likely to match the exact line that `question` is asking to find in a source repository",
			Options: []jev.Option{
				{Name: "a", Criteria: map[string]any{"see": "pattern_a"}},
				{Name: "b", Criteria: map[string]any{"see": "pattern_b"}},
			},
		}},
	}
	decision, err := client.Ask(ctx, request)
	if err != nil {
		return a, decision, err
	}
	choice := decision.Answers["pick"].Choice
	if choice == "b" {
		return b, decision, nil
	}
	return a, decision, nil
}

func Jevgrep(ctx context.Context, client *jev.Client, root string, q toolscorpus.Asked) tools.Outcome {
	a, b := twoPatterns(q)
	started := time.Now()
	pattern, decision, err := choosePattern(ctx, client, q.Text, a, b)
	if err != nil {
		return tools.Outcome{Version: "jevgrep", Skipped: err.Error(), Calls: 1, Elapsed: time.Since(started)}
	}
	lines, bytes, err := GitGrep(root, pattern, q.Tree)
	elapsed := time.Since(started)
	if err != nil {
		return tools.Outcome{Version: "jevgrep", Skipped: err.Error(), Calls: 2, Elapsed: elapsed}
	}
	return tools.Outcome{
		Version: "jevgrep",
		Hit:     HitsTarget(lines, q),
		Bytes:   bytes,
		Calls:   2,
		Cost:    decision.Usage.Cost,
		Elapsed: elapsed,
	}
}

func JevgrepTriple(ctx context.Context, client *jev.Client, root string, q toolscorpus.Asked) tools.Outcome {
	a, b := twoPatterns(q)
	words := tools.ContentWords(q.Text)
	c := tools.Alternation(words[:min(2, len(words))])
	patterns := []string{a, b, c}
	started := time.Now()

	results := make([][]string, len(patterns))
	totalBytes := 0
	for i, p := range patterns {
		lines, bytes, err := GitGrep(root, p, q.Tree)
		if err != nil {
			continue
		}
		results[i] = lines
		totalBytes += bytes
	}

	options := make([]jev.Option, len(patterns))
	state := map[string]any{"question": q.Text}
	for i, p := range patterns {
		key := fmt.Sprintf("result_%d", i)
		state[key] = fmt.Sprintf("pattern %q found %d matching lines", p, len(results[i]))
		options[i] = jev.Option{Name: fmt.Sprintf("%d", i), Criteria: map[string]any{"see": key}}
	}
	decision, err := client.Ask(ctx, jev.Request{
		State: state,
		Questions: []jev.Question{{
			ID:           "pick",
			Kind:         jev.QuestionChoice,
			Instructions: "which of these three grep results most likely answers `question`",
			Options:      options,
		}},
	})
	elapsed := time.Since(started)
	if err != nil {
		return tools.Outcome{Version: "jevgrep triple", Skipped: err.Error(), Calls: 3, Elapsed: elapsed}
	}
	choice := decision.Answers["pick"].Choice
	index, err := strconv.Atoi(choice)
	if err != nil || index < 0 || index >= len(results) {
		index = 0
	}
	return tools.Outcome{
		Version: "jevgrep triple",
		Hit:     HitsTarget(results[index], q),
		Bytes:   totalBytes,
		Calls:   4,
		Cost:    decision.Usage.Cost,
		Elapsed: elapsed,
	}
}

func JevgrepAnswer(ctx context.Context, client *jev.Client, root string, q toolscorpus.Asked) tools.Outcome {
	broad := broadPattern(tools.ContentWords(q.Text))
	started := time.Now()
	lines, _, err := GitGrep(root, broad, q.Tree)
	if err != nil {
		return tools.Outcome{Version: "jevgrep answer", Skipped: err.Error(), Calls: 1, Elapsed: time.Since(started)}
	}
	if len(lines) == 0 {
		return tools.Outcome{Version: "jevgrep answer", Hit: false, Calls: 1, Elapsed: time.Since(started)}
	}
	capped := lines
	if len(capped) > jevgrepAnswerLineCap {
		capped = capped[:jevgrepAnswerLineCap]
	}
	state := map[string]any{"question": q.Text}
	options := make([]jev.Option, len(capped))
	for i, l := range capped {
		key := fmt.Sprintf("line_%d", i)
		state[key] = l
		options[i] = jev.Option{Name: fmt.Sprintf("%d", i), Criteria: map[string]any{"see": key}}
	}
	decision, err := client.Ask(ctx, jev.Request{
		State: state,
		Questions: []jev.Question{{
			ID:           "pick",
			Kind:         jev.QuestionChoice,
			Instructions: "which of these matched lines is the single line that answers `question`",
			Options:      options,
		}},
	})
	elapsed := time.Since(started)
	if err != nil {
		return tools.Outcome{Version: "jevgrep answer", Skipped: err.Error(), Calls: 2, Elapsed: elapsed}
	}
	choice := decision.Answers["pick"].Choice
	index, convErr := strconv.Atoi(choice)
	if convErr != nil || index < 0 || index >= len(capped) {
		index = 0
	}
	answer := capped[index]
	return tools.Outcome{
		Version: "jevgrep answer",
		Hit:     HitsTarget([]string{answer}, q),
		Bytes:   len(answer),
		Calls:   2,
		Cost:    decision.Usage.Cost,
		Elapsed: elapsed,
	}
}

func topLevelDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

func JevgrepNarrowed(ctx context.Context, client *jev.Client, root string, q toolscorpus.Asked) tools.Outcome {
	started := time.Now()
	dirs := topLevelDirs(root)
	if len(dirs) == 0 {
		return tools.Outcome{Version: "jevgrep narrowed", Skipped: "no top level directory to narrow into", Elapsed: time.Since(started)}
	}
	dirOptions := make([]jev.Option, len(dirs))
	for i, d := range dirs {
		dirOptions[i] = jev.Option{Name: d, Criteria: fmt.Sprintf("the answer to `question` sits under the %s directory", d)}
	}
	dirDecision, err := client.Ask(ctx, jev.Request{
		State: map[string]any{"question": q.Text},
		Questions: []jev.Question{{
			ID:           "dir",
			Kind:         jev.QuestionChoice,
			Instructions: "which top level directory of this go repository most likely holds the file that answers `question`",
			Options:      dirOptions,
		}},
	})
	if err != nil {
		return tools.Outcome{Version: "jevgrep narrowed", Skipped: err.Error(), Calls: 1, Elapsed: time.Since(started)}
	}
	dir := dirDecision.Answers["dir"].Choice

	a, b := twoPatterns(q)
	pattern, patternDecision, err := choosePattern(ctx, client, q.Text, a, b)
	if err != nil {
		return tools.Outcome{Version: "jevgrep narrowed", Skipped: err.Error(), Calls: 2, Elapsed: time.Since(started)}
	}
	lines, bytes, err := GitGrep(root+"/"+dir, pattern, q.Tree)
	elapsed := time.Since(started)
	if err != nil {
		return tools.Outcome{Version: "jevgrep narrowed", Skipped: err.Error(), Calls: 3, Elapsed: elapsed}
	}
	prefixed := make([]string, len(lines))
	for i, l := range lines {
		prefixed[i] = dir + "/" + l
	}
	return tools.Outcome{
		Version: "jevgrep narrowed",
		Hit:     HitsTarget(prefixed, q),
		Bytes:   bytes,
		Calls:   3,
		Cost:    dirDecision.Usage.Cost + patternDecision.Usage.Cost,
		Elapsed: elapsed,
	}
}
