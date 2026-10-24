package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/bench/stat"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/rule"
)

const (
	breaksAbove     = 0.5
	beforeQuestions = 12
	beforeWallMS    = 395
	beforeReport    = "bench/api/report-2026-09-18.md"
)

var labelledRules = []string{"em_dash", "quote", "report_short", "stay_in_scope"}

type rendered struct {
	body      string
	oversized []string
}

func render(replies []reply, rules []rule.Rule, rec record, recordPath string) (rendered, error) {
	if rec.Wire != typesafe.Name {
		return rendered{}, fmt.Errorf("the record names wire %q and only %s has a list price here", rec.Wire, typesafe.Name)
	}
	emRule, ok := findRule(rules, emDashRule)
	if !ok {
		return rendered{}, fmt.Errorf("the library has no %s rule", emDashRule)
	}
	checked, err := shippedChecker(replies, emRule)
	if err != nil {
		return rendered{}, err
	}
	answers := map[string]map[string]float64{}
	builds := map[string]bool{}
	for _, o := range rec.Outcomes {
		answers[o.ID] = o.Answers
		builds[o.Build] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# bench rules batched: every prose rule as one Jev noul over a finished lead reply, %s\n\n", rec.Date)
	fmt.Fprintf(&b, "Machine: %s. Credential kind: %s. Wire: %s. Model requested: `%s`. Build id the responses reported: `%s`. Date: %s. No first call was discarded as a warm-up; requests ran one at a time.\n\n",
		rec.Machine, rec.Credential, rec.Wire, rec.Model, strings.Join(slices.Sorted(maps.Keys(builds)), ", "), rec.Date)
	fmt.Fprintf(&b, "Rerun, offline, from the record: `go run ./bench/rules/batched -record %s`. A new live run needs the network and the TypeSafe key: `go run ./bench/rules/batched -live`.\n\n", recordPath)
	fmt.Fprintf(&b, "Corpus: %d final lead replies recorded in this project's sessions, text mocked for a public repository, hand labelled on %s. A Jev answer over %.1f counts as breaks. Arms: (a) no check, as shipped, (b) the em_dash checker in internal/rule/check.go, (c) Jev, every applicable rule in one request a reply.\n\n",
		len(replies), strings.Join(labelledRules, ", "), breaksAbove)
	fmt.Fprintf(&b, "Leakage: %d of %d replies quote %d words in a row of a rule they are labelled on.\n\n", len(leaks(replies, rules)), len(replies), leakRunOfWords)
	writeAgreement(&b, replies, answers, checked)
	writeEmDash(&b, replies, answers, checked)
	oversized := writeCost(&b, rec)
	writeUnlabelled(&b, replies, rules, answers)
	return rendered{body: b.String(), oversized: oversized}, nil
}

func writeAgreement(b *strings.Builder, replies []reply, answers map[string]map[string]float64, checked map[string]bool) {
	b.WriteString("## Agreement with the hand label\n\n")
	b.WriteString("| Rule | Replies labelled | Label breaks | (a) no check agrees | (b) em_dash checker agrees | (c) Jev agrees | Jev false flags | Jev misses | Jev p50, label breaks | Jev p50, label keeps |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, id := range labelledRules {
		labelled, checker, jevAgree, falseFlags, misses := 0, 0, 0, 0, 0
		byLabel := map[bool][]float64{}
		for _, r := range replies {
			label, has := r.Labels[id]
			if !has {
				continue
			}
			labelled++
			answer := answers[r.ID][id]
			byLabel[label] = append(byLabel[label], answer)
			checker += boolCount(checked[r.ID] == label)
			switch flagged := answer > breaksAbove; {
			case flagged == label:
				jevAgree++
			case flagged:
				falseFlags++
			default:
				misses++
			}
		}
		checkerCell := "n/a"
		if id == emDashRule {
			checkerCell = share(checker, labelled)
		}
		fmt.Fprintf(b, "| %s | %d | %d | %s | %s | %s | %d | %d | %s | %s |\n", id, labelled, len(byLabel[true]), share(len(byLabel[false]), labelled), checkerCell,
			share(jevAgree, labelled), falseFlags, misses, median(byLabel[true]), median(byLabel[false]))
	}
}

func writeEmDash(b *strings.Builder, replies []reply, answers map[string]map[string]float64, checked map[string]bool) {
	b.WriteString("\n## em_dash, the checker and Jev on the same replies\n\n")
	b.WriteString("| Reply | U+2014 in reply | Label breaks | Bare regex flags | Shipped checker flags | Jev answer |\n|---|---|---|---|---|---|\n")
	labelled, bare, shipped, jevFlags := 0, 0, 0, 0
	for _, r := range replies {
		count := strings.Count(r.Reply, emDash)
		answer := answers[r.ID][emDashRule]
		labelled += boolCount(r.Labels[emDashRule])
		bare += min(count, 1)
		shipped += boolCount(checked[r.ID])
		jevFlags += boolCount(answer > breaksAbove)
		if count > 0 || checked[r.ID] || answer > breaksAbove {
			fmt.Fprintf(b, "| %s | %d | %t | %t | %t | %.2f |\n", r.ID, count, r.Labels[emDashRule], count > 0, checked[r.ID], answer)
		}
	}
	fmt.Fprintf(b, "| all %d | | %d | %d | %d | %d over %.1f |\n", len(replies), labelled, bare, shipped, jevFlags, breaksAbove)
}

func writeCost(b *strings.Builder, rec record) []string {
	var questions, tokens, costs, walls []float64
	total := 0.0
	sources := map[ledger.ChargeSource]int{}
	var oversized []string
	for _, o := range rec.Outcomes {
		charge := ledger.ChargeOf(o.Cost, float64(o.InputTokens)*typesafe.DollarsPerInputToken)
		sources[charge.Source]++
		questions = append(questions, float64(o.Questions))
		tokens = append(tokens, float64(o.InputTokens))
		costs = append(costs, charge.USD)
		walls = append(walls, float64(o.WallMS))
		total += charge.USD
		if o.InputTokens > konst.JudgeRequestTokenCeiling {
			oversized = append(oversized, o.ID)
		}
	}
	fmt.Fprintf(b, "\n## The batched request, per reply, n=%d\n\n", len(rec.Outcomes))
	b.WriteString("| Measure | min | p50 | p95 | max | Before |\n|---|---|---|---|---|---|\n")
	fmt.Fprintf(b, "| Questions | %s | %d (%s) |\n", spread(questions, "%.0f"), beforeQuestions, beforeReport)
	fmt.Fprintf(b, "| Input tokens | %s | |\n", spread(tokens, "%.0f"))
	fmt.Fprintf(b, "| Dollars | %s | |\n", spread(costs, "%.6f"))
	fmt.Fprintf(b, "| Wall ms | %s | %d ms at %d questions |\n", spread(walls, "%.0f"), beforeWallMS, beforeQuestions)
	fmt.Fprintf(b, "\nTotal: %.6f USD over %d requests. Charge source per request, as the product books it (`ledger.ChargeOf`): %v; a list charge is input tokens times the TypeSafe list price, because the response reported no cost. Requests over %d tokens: %d %v.\n",
		total, len(rec.Outcomes), sources, konst.JudgeRequestTokenCeiling, len(oversized), oversized)
	return oversized
}

func writeUnlabelled(b *strings.Builder, replies []reply, rules []rule.Rule, answers map[string]map[string]float64) {
	b.WriteString("\n## Jev on the rules nobody labelled\n\n| Rule | Asked | Jev flags breaks |\n|---|---|---|\n")
	asked, flags := map[string]int{}, map[string]int{}
	for _, r := range replies {
		for _, q := range applicable(rules, r) {
			asked[q.ID]++
			flags[q.ID] += boolCount(answers[r.ID][q.ID] > breaksAbove)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(asked)) {
		if !slices.Contains(labelledRules, id) {
			fmt.Fprintf(b, "| %s | %d | %d |\n", id, asked[id], flags[id])
		}
	}
}

func shippedChecker(replies []reply, emRule rule.Rule) (map[string]bool, error) {
	dir, err := os.MkdirTemp("", "tofu-batched-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	check := rule.Builtins()[emDashRule].Check
	flagged := map[string]bool{}
	for _, r := range replies {
		path := filepath.Join(dir, r.ID+".md")
		if err := os.WriteFile(path, []byte(r.Reply), 0o600); err != nil {
			return nil, err
		}
		findings, err := check(emRule, rule.TextFile{Path: path})
		if err != nil {
			return nil, err
		}
		flagged[r.ID] = len(findings) > 0
	}
	return flagged, nil
}

func findRule(rules []rule.Rule, id string) (rule.Rule, bool) {
	at := slices.IndexFunc(rules, func(r rule.Rule) bool { return r.ID == id })
	if at < 0 {
		return rule.Rule{}, false
	}
	return rules[at], true
}

func spread(values []float64, format string) string {
	low, high := stat.Spread(values)
	cells := []float64{low, stat.Median(values), stat.Percentile(values, 95), high}
	out := make([]string, len(cells))
	for i, v := range cells {
		out[i] = fmt.Sprintf(format, v)
	}
	return strings.Join(out, " | ")
}

func median(values []float64) string {
	if len(values) == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", stat.Median(values))
}

func share(part, whole int) string {
	return fmt.Sprintf("%d of %d (%.0f%%)", part, whole, 100*float64(part)/float64(whole))
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}
