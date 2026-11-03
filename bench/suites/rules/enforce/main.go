package main

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/librarian/rules"
	"tofu/internal/sys"
)

type verdict struct {
	refused, recorded bool
	targets, others   []string
}

func judge(rules []rule.Rule, id string, w write) verdict {
	fires := rule.CheckWrite(rules, w.path, w.before, w.after)
	_, err := rule.Weigh(fires)
	var v verdict
	for _, f := range fires {
		if f.RuleID != id {
			v.others = append(v.others, f.RuleID)
			continue
		}
		v.refused, v.recorded = err != nil && f.Blocked, !f.Blocked
		for _, finding := range f.Lines() {
			v.targets = append(v.targets, finding.Target+" `"+finding.Detail+"`")
		}
	}
	return v
}

func moded(rules []rule.Rule, mode rule.Mode) []rule.Rule {
	out := slices.Clone(rules)
	for i := range out {
		out[i].Mode = mode
	}
	return out
}

type counts struct{ n, refused, shadowRefused, recorded int }

func (c *counts) add(enforced, shadow verdict) {
	c.n++
	if enforced.refused {
		c.refused++
	}
	if shadow.refused {
		c.shadowRefused++
	}
	if shadow.recorded {
		c.recorded++
	}
}

type tally struct{ planted, clean counts }

func (t tally) bar() string {
	var below []string
	if t.planted.refused*10 < t.planted.n*9 {
		below = append(below, "planted below 90%")
	}
	if t.clean.refused*50 > t.clean.n {
		below = append(below, "clean above 2%")
	}
	if below == nil {
		return "yes"
	}
	return "no: " + strings.Join(below, ", ")
}

type outcome struct {
	rules                         []rule.Rule
	tallies                       map[string]*tally
	misses, falses, owed, windows []string
}

func share(n, of int) string {
	if of == 0 {
		return "0 of 0"
	}
	return fmt.Sprintf("%d of %d (%d%%)", n, of, n*100/of)
}

func main() {
	root := sys.SourceRoot()
	dir := flag.String("rules", filepath.Join(root, ".tofu", "rules"), "the folder the desk_refuse rules load from")
	size := flag.Int("window", 40, "lines in each clean write cut from a file at HEAD")
	flag.Parse()
	if err := run(root, *dir, *size); err != nil {
		fmt.Fprintln(os.Stderr, "enforce:", err)
		os.Exit(1)
	}
}

func run(root, dir string, size int) error {
	head, err := git(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return err
	}
	o, err := measure(root, dir, size)
	if err != nil {
		return err
	}
	report(o, dir, strings.TrimSpace(head), size)
	return nil
}

func measure(root, dir string, size int) (outcome, error) {
	o := outcome{tallies: map[string]*tally{}}
	loaded, err := rule.LoadDir(dir)
	if err != nil {
		return o, err
	}
	for _, r := range loaded {
		if strings.HasPrefix(r.ID, "desk_refuse_") {
			o.rules = append(o.rules, r)
		}
	}
	if o.rules == nil {
		return o, fmt.Errorf("%s holds no desk_refuse rule", dir)
	}
	slices.SortFunc(o.rules, func(a, b rule.Rule) int { return strings.Compare(a.ID, b.ID) })
	corpus := filepath.Join(root, "bench", "suites", "rules", "enforce", "corpus")
	writes, err := readPlanted(root, filepath.Join(corpus, "planted.txt"))
	if err != nil {
		return o, err
	}
	held, err := readDebts(filepath.Join(corpus, "debt.txt"))
	if err != nil {
		return o, err
	}
	enforced, shadow := moded(o.rules, rule.ModeEnforced), moded(o.rules, rule.ModeShadow)
	for _, r := range o.rules {
		t := &tally{}
		o.tallies[r.ID] = t
		for _, w := range writes {
			if w.rule != r.ID {
				continue
			}
			on := judge(enforced, r.ID, w)
			t.planted.add(on, judge(shadow, r.ID, w))
			if !on.refused {
				o.misses = append(o.misses, fmt.Sprintf("| %s | %s | %s | `%s` | %s | %s |", r.ID, w.ticket, w.path, w.mark, w.why, cmp.Or(strings.Join(on.others, ", "), "none")))
			}
		}
		cuts, owed, err := clean(root, r, t.planted.n, size, held)
		if err != nil {
			return o, err
		}
		for _, c := range owed {
			o.owed = append(o.owed, fmt.Sprintf("| %s | %s | %s |", r.ID, c.window, strings.Join(c.debts, "; ")))
		}
		for _, c := range cuts {
			o.windows = append(o.windows, fmt.Sprintf("| %s | %s |", r.ID, c.window))
			on := judge(enforced, r.ID, c.write)
			t.clean.add(on, judge(shadow, r.ID, c.write))
			if on.refused {
				o.falses = append(o.falses, fmt.Sprintf("| %s | %s | %s |", r.ID, c.window, strings.Join(on.targets, "; ")))
			}
		}
	}
	return o, nil
}

func report(o outcome, dir, head string, size int) {
	now := time.Now()
	machine, _ := os.Hostname()
	fmt.Printf("# bench enforce: desk_refuse rules on planted desk mistakes and clean desk writes, %s\n\n", now.Format("2006-01-02"))
	fmt.Printf("Machine: %s. Date: %s. Commit: %s. Rules folder: `%s`, %d desk_refuse rules, each arm setting every rule's mode. Checker: `internal/librarian/rules` CheckWrite then Weigh, the call `internal/agent/loop/tool_write.go` makes. No model call, no network, no warm-up. Clean writes: %d lines cut from the middle of a file at HEAD the rule watches, written as an edit that adds them.\n\n", machine, now.Format("2006-01-02 15:04"), head, filepath.Base(dir), len(o.rules), size)
	fmt.Print("## tldr\n\nSource: bench/suites/rules/enforce/corpus/planted.txt (mocked from DESK tickets) and clean cuts from git HEAD, each through CheckWrite and Weigh with the rules in mode enforced, then in mode shadow.\n\n")
	fmt.Println("| Rule | Enforced: planted refused | Enforced: clean refused | Shadow: planted refused | Shadow: clean refused | Shadow: planted recorded | Shadow: clean recorded | Meets 90% and 2% |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	for _, r := range o.rules {
		p, c := o.tallies[r.ID].planted, o.tallies[r.ID].clean
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s | %s |\n", r.ID, share(p.refused, p.n), share(c.refused, c.n),
			share(p.shadowRefused, p.n), share(c.shadowRefused, c.n), share(p.recorded, p.n), share(c.recorded, c.n), o.tallies[r.ID].bar())
	}
	table("Planted writes the enforced arm let through", "Source: bench/suites/rules/enforce/corpus/planted.txt.", "| Rule | Ticket | Path | Line | Why | Other desk rules that fired |\n|---|---|---|---|---|---|", o.misses)
	table("Clean writes the enforced arm refused", "Source: git HEAD, cut by bench/suites/rules/enforce.", "| Rule | Window | Finding |\n|---|---|---|", o.falses)
	table("Windows skipped as debt already in the tree", "Source: bench/suites/rules/enforce/corpus/debt.txt.", "| Rule | Window | Debt lines |\n|---|---|---|", o.owed)
	table("Clean windows", "Source: git HEAD, cut by bench/suites/rules/enforce.", "| Rule | Window |\n|---|---|", o.windows)
}

func table(title, source, header string, rows []string) {
	fmt.Printf("\n## %s\n\n%s\n\n%s\n", title, source, header)
	if rows == nil {
		fmt.Println("| none |")
	}
	for _, row := range rows {
		fmt.Println(row)
	}
}
