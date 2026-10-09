package main

import (
	"cmp"
	"fmt"
	"math"
	"math/bits"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
)

const (
	forkStateWhy = "the working state the fork carries"
	shownRunes   = 70
	forksNeeded  = 2
	perHundred   = 100
)

var numberedLine = regexp.MustCompile(`^\s*\**(\d+)\**[.):]\s*(.*)$`)

func answerLines(answer string, count int) ([]string, bool) {
	lines := make([]string, count)
	found := 0
	for _, line := range strings.Split(answer, "\n") {
		match := numberedLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if at, err := strconv.Atoi(match[1]); err == nil && at >= 1 && at <= count && lines[at-1] == "" {
			lines[at-1] = strings.TrimSpace(match[2])
			found++
		}
	}
	if found == count {
		return lines, true
	}
	for i := range lines {
		lines[i] = answer
	}
	return lines, false
}

type armScore struct {
	arm                   arm
	chains, short, failed int
	questions             int
	right, unparsed       int
	turnTokens            []int
	prompt, read          int
	messages              int
	forkCalls, forkTokens int
	forkUSD               float64
	episodeCalls          int
	forks                 []int
	models                []string
	refused               int
}

func (r chainRun) keys(q question, s sources) ([]string, string) {
	if q.Capture == nil {
		return q.Key, ""
	}
	if len(r.replies) == 0 {
		return nil, "this arm runs no first session, so the lead settled nothing"
	}
	value := q.Capture.from(r.replies[0][q.Capture.Say])
	if value == "" {
		return nil, "the lead never wrote " + q.Capture.Label + "="
	}
	if found := s.leaksOf(r.chain.ID, value); len(found) > 0 {
		return []string{value}, found[0].where
	}
	return []string{value}, ""
}

func episodeCalls(r chainRun) int {
	var items []string
	for i, said := range r.chain.Sessions {
		items = append(items, said.Says...)
		if i < len(r.replies) {
			items = append(items, r.replies[i]...)
		}
	}
	items = append(items, r.chain.ask(), r.answer)
	calls := len(items) - bits.OnesCount(uint(len(items)))
	for _, text := range items {
		if len("lead: "+text) > konst.MemtreeLineBytes {
			calls++
		}
	}
	return calls
}

func (s *armScore) add(r chainRun) string {
	var row strings.Builder
	lines, parsed := answerLines(r.answer, len(r.chain.Questions))
	from := r.chain.sources()
	for _, later := range r.replies[min(1, len(r.replies)):] {
		from.later = append(from.later, later...)
	}
	right, counted := 0, 0
	for i, q := range r.chain.Questions {
		keys, out := r.keys(q, from)
		mark := "out"
		if out == "" {
			counted++
			mark = "wrong"
			if slices.ContainsFunc(keys, func(value string) bool { return holds(lines[i], value) }) {
				mark = "right"
				right++
			}
		}
		shown := []rune(strings.Join(strings.Fields(lines[i]), " "))
		fmt.Fprintf(&row, "    %d %-5s key %-18s said %s\n", i+1, mark, cmp.Or(keys...), string(shown[:min(len(shown), shownRunes)]))
		if out != "" {
			fmt.Fprintf(&row, "      not counted: %s\n", out)
		}
	}
	if s.arm == armTree {
		s.episodeCalls += episodeCalls(r)
	}
	if !parsed {
		s.unparsed++
		row.WriteString("    the answer had no numbered line per question, so each question was graded against the whole answer\n")
	}
	turns := map[string]int{}
	calls := map[string]int{}
	for _, trace := range r.traces {
		s.messages += len(trace.Messages)
		for _, call := range trace.Calls {
			if call.RecordedIn == "" {
				calls[call.Tool+" "+cmp.Or(call.Gate, "ungated")]++
			}
		}
		for _, request := range trace.Requests {
			if request.RecordedIn != "" {
				continue
			}
			used := request.Usage
			prompt := used.InputTokens + used.CacheReadTokens + used.CacheWriteTokens
			s.prompt += prompt
			s.read += used.CacheReadTokens
			if request.Why == forkStateWhy {
				s.forkCalls++
				s.forkTokens += prompt + used.OutputTokens
				s.forkUSD += request.CostUSD
				continue
			}
			turns[request.Turn] += prompt + used.OutputTokens
			if request.Model != "" && !slices.Contains(s.models, request.Model) {
				s.models = append(s.models, request.Model)
			}
		}
	}
	for _, tokens := range turns {
		s.turnTokens = append(s.turnTokens, tokens)
	}
	forks := len(r.generations) - 1
	s.chains++
	if s.arm != armFloor && forks < forksNeeded {
		s.short++
		fmt.Fprintf(&row, "    SHORT: %d forks before the answer and a chain needs %d, so its answers are not counted\n", forks, forksNeeded)
	} else {
		s.questions += counted
		s.right += right
	}
	s.forks = append(s.forks, forks)
	s.refused += r.refused
	asked := slices.Index(r.generations, r.askedIn) + 1
	return fmt.Sprintf("%-6s %-16s right %d/%d, %d forks, asked in generation %d of %d, %d turns, %d asks refused\n    context used before each turn %v\n    tool calls %v\n",
		s.arm, r.chain.ID, right, len(r.chain.Questions), forks, asked, len(r.generations), len(turns), r.refused, r.used, calls) + row.String()
}

func percentile(values []int, share float64) int {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	return sorted[max(0, int(math.Ceil(share*float64(len(sorted))))-1)]
}

func per(count, messages int) float64 {
	if messages == 0 {
		return 0
	}
	return float64(count) * perHundred / float64(messages)
}

func table(scores []armScore) string {
	var out strings.Builder
	header := "%-6s %9s %9s %11s %9s %9s %9s %10s %12s %14s %12s %14s %9s %8s  %s\n"
	fmt.Fprintf(&out, header, "arm", "chains", "questions", "right", "turn p50", "turn p95", "turn max", "cache read",
		"fork calls", "fork tokens", "fork usd", "episode calls", "forks", "refused", "model")
	fmt.Fprintf(&out, header, "", "ok-short", "counted", "", "tokens", "tokens", "tokens", "share", "/100 msgs", "/100 msgs", "/100 msgs", "/100, at most", "min-max", "asks", "as reported")
	fmt.Fprintf(&out, header, "", "-failed", "", "", "", "", "", "", "", "", "", "", "", "", "")
	for _, s := range scores {
		readShare := 0.0
		if s.prompt > 0 {
			readShare = float64(s.read) * perHundred / float64(s.prompt)
		}
		forks := "-"
		if len(s.forks) > 0 {
			forks = fmt.Sprintf("%d-%d", slices.Min(s.forks), slices.Max(s.forks))
		}
		fmt.Fprintf(&out, header, s.arm, fmt.Sprintf("%d-%d-%d", s.chains, s.short, s.failed), fmt.Sprint(s.questions),
			fmt.Sprintf("%d (%.0f%%)", s.right, per(s.right, s.questions)),
			fmt.Sprint(percentile(s.turnTokens, 0.5)), fmt.Sprint(percentile(s.turnTokens, 0.95)), fmt.Sprint(percentile(s.turnTokens, 1)),
			fmt.Sprintf("%.1f%%", readShare),
			fmt.Sprintf("%.1f", per(s.forkCalls, s.messages)), fmt.Sprintf("%.0f", per(s.forkTokens, s.messages)),
			fmt.Sprintf("%.4f", s.forkUSD*perHundred/float64(max(s.messages, 1))),
			fmt.Sprintf("%.1f", per(s.episodeCalls, s.messages)),
			forks, fmt.Sprint(s.refused), strings.Join(s.models, ","))
	}
	for _, s := range scores {
		if s.unparsed > 0 {
			fmt.Fprintf(&out, "%s: %d answers had no numbered line per question and were graded whole\n", s.arm, s.unparsed)
		}
	}
	return out.String()
}
