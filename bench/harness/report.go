package harness

import (
	_ "embed"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"tofu/internal/judge/jev/wire/openrouter"
)

//go:embed entitlement.txt
var entitlement string

func Detail(row Row, gaps []string, execution Execution, ledgerDir string) string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	passed, total := ChecklistScore(row)
	modelSpend := "subscription quota, no money, so there is no model dollar figure"
	if row.ModelDollars != nil {
		modelSpend = fmt.Sprintf("api key $%.6f", *row.ModelDollars)
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "ROW %s %s v%d run%d\n", row.Arm, row.Task, row.Version, row.Run)
	fmt.Fprintf(b, "source: live, %s of wall clock in the runner, exit code %d, runner end reason %s\n",
		execution.Elapsed().Round(time.Millisecond), execution.ExitCode, execution.EndReason)
	fmt.Fprintf(b, "machine: %s. model credential kind: %s. date: %s. commit: %s. cli: %s\n",
		host, row.CredentialKind, row.Start.Format("2006-01-02"), row.Commit, row.CLIVersion)
	fmt.Fprintf(b, "model: %s. judge wire: %s. judge ledger: %s\n", row.Model, openrouter.Name, ledgerDir)
	fmt.Fprintf(b, "setup %s: %s\n", row.Setup.Name, row.Setup.Line())
	fmt.Fprintf(b, "checklist: %d/%d graded items\n", passed, total)
	fmt.Fprintf(b, "model spend: %s. jev decisions: $%.6f on the openrouter key\n", modelSpend, row.JudgeDollars)
	fmt.Fprintf(b, "wall clock: %d ms. turns: %d. end reason: %s\n", row.WallClockMS, row.Turns, row.EndReason)
	fmt.Fprintf(b, "tokens: %d in, %d out\n", row.BilledInput, row.BilledOutput)
	fmt.Fprintf(b, "tool calls: read %d, write %d, shell %d, other %d, failed %d\n",
		row.ToolCalls.Read, row.ToolCalls.Write, row.ToolCalls.Shell, row.ToolCalls.Other, row.ToolCalls.Failed)
	fmt.Fprintf(b, "caps: wall clock %s, turns %d\n", execution.Plan.Caps.WallClock, execution.Plan.Caps.TurnCap)
	for _, gate := range row.Gates {
		fmt.Fprintf(b, "gate %s: %s %s\n", gate.Name, gate.Status, firstLine(gate.Reason))
	}
	for _, item := range row.Checklist {
		fmt.Fprintf(b, "checklist %s: %t\n", item.Item, item.Passed)
	}
	for _, gap := range gaps {
		fmt.Fprintf(b, "gap: %s\n", gap)
	}
	return b.String()
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 160 {
		return line[:160] + " ..."
	}
	return line
}

func Render(rows []Row) string {
	var b strings.Builder
	kept, missing := readable(rows)
	renderMissing(&b, len(rows), missing)
	for _, task := range tasksOf(kept) {
		renderTask(&b, task, rowsForTask(kept, task))
	}
	b.WriteString("\nWHAT A PASSING EVAL ENTITLES\n" + entitlement)
	return b.String()
}

func renderMissing(b *strings.Builder, offered int, missing []MissingField) {
	fmt.Fprint(b, "ROWS READ\n")
	if len(missing) == 0 {
		fmt.Fprintf(b, "every one of the %d rows offered carries the fields this report reads\n\n", offered)
		return
	}
	named := make([]string, 0, len(missing))
	for _, m := range missing {
		named = append(named, m.Line())
	}
	fmt.Fprintf(b, "%d rows are not read, one missing field each, of the %d offered: %s\n\n",
		len(missing), offered, strings.Join(named, ", "))
}

func renderSetups(b *strings.Builder, task string, rows []Row) {
	fmt.Fprintf(b, "SETUPS %s\n", task)
	bySetup := map[string][]Row{}
	for _, r := range rows {
		bySetup[r.Setup.Name] = append(bySetup[r.Setup.Name], r)
	}
	names := make([]string, 0, len(bySetup))
	for name := range bySetup {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		group := bySetup[name]
		label := name
		if label == "" {
			label = "unrecorded"
		}
		fmt.Fprintf(b, "%s: %s, %d of %d runs passed. %s\n", label, TierOf(group), PassCount(group), len(group), group[0].Setup.Line())
	}
	fmt.Fprintln(b)
}

func tasksOf(rows []Row) []string {
	seen := map[string]bool{}
	var tasks []string
	for _, r := range rows {
		if !seen[r.Task] {
			seen[r.Task] = true
			tasks = append(tasks, r.Task)
		}
	}
	sort.Strings(tasks)
	return tasks
}

func rowsForTask(rows []Row, task string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Task == task {
			out = append(out, r)
		}
	}
	return out
}

func renderTask(b *strings.Builder, task string, rows []Row) {
	renderSetups(b, task, rows)
	gateOK, full := renderGates(b, task, rows)
	if renderUnstable(b, task, rows) {
		return
	}
	renderFrontier(b, task, full)
	renderDollarsRatio(b, task, full)
	renderTurnsRatio(b, task, full)
	renderChecklist(b, task, gateOK)
}

func renderUnstable(b *strings.Builder, task string, rows []Row) bool {
	var unstable []Stability
	for _, s := range StabilityOf(rows) {
		if s.Unstable() {
			unstable = append(unstable, s)
		}
	}
	if len(unstable) == 0 {
		return false
	}
	fmt.Fprintf(b, "\nUNSTABLE %s\n", task)
	for _, s := range unstable {
		fmt.Fprintln(b, s.Line())
	}
	fmt.Fprint(b, "this task ranks no arm: a door that opens on one repeat and shuts on the next is the finding, "+
		"and a median taken across it stands for nothing\n\n")
	return true
}

func evaluateGates(r Row) (bool, []string) {
	var reasons []string
	for _, g := range r.Gates {
		if g.Status != GateStatusPassed {
			reasons = append(reasons, g.Name)
		}
	}
	return len(reasons) == 0, reasons
}

func checklistFull(r Row) (bool, []string) {
	var misses []string
	for _, c := range r.Checklist {
		if !c.Passed {
			misses = append(misses, "checklist: "+c.Item)
		}
	}
	return len(misses) == 0, misses
}

func renderGates(b *strings.Builder, task string, rows []Row) (gateOK, full []Row) {
	fmt.Fprintf(b, "GATES %s\n", task)
	sorted := append([]Row(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Arm != sorted[j].Arm {
			return sorted[i].Arm < sorted[j].Arm
		}
		if sorted[i].Version != sorted[j].Version {
			return sorted[i].Version < sorted[j].Version
		}
		return sorted[i].Run < sorted[j].Run
	})
	for _, r := range sorted {
		gatesOK, gateReasons := evaluateGates(r)
		checklistOK, misses := checklistFull(r)
		label := fmt.Sprintf("%s v%d run%d", r.Arm, r.Version, r.Run)
		switch {
		case gatesOK && checklistOK:
			fmt.Fprintf(b, "%s: PASS\n", label)
			gateOK = append(gateOK, r)
			full = append(full, r)
		case gatesOK:
			fmt.Fprintf(b, "%s: FAIL %s\n", label, strings.Join(misses, ", "))
			gateOK = append(gateOK, r)
		default:
			fmt.Fprintf(b, "%s: FAIL %s\n", label, strings.Join(gateReasons, ", "))
		}
	}
	return gateOK, full
}

func sortedArms(rows []Row) []Arm {
	seen := map[Arm]bool{}
	var arms []Arm
	for _, r := range rows {
		if !seen[r.Arm] {
			seen[r.Arm] = true
			arms = append(arms, r.Arm)
		}
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i] < arms[j] })
	return arms
}

func groupByArm(rows []Row) map[Arm][]Row {
	byArm := map[Arm][]Row{}
	for _, r := range rows {
		byArm[r.Arm] = append(byArm[r.Arm], r)
	}
	return byArm
}

func kindOf(byArm map[Arm][]Row, a Arm) CredentialKind {
	return byArm[a][0].CredentialKind
}

func dollarsOf(rows []Row) []float64 {
	var values []float64
	for _, r := range rows {
		if r.ModelDollars != nil {
			values = append(values, *r.ModelDollars)
		}
	}
	return values
}

func judgeDollarsOf(rows []Row) []float64 {
	values := make([]float64, len(rows))
	for i, r := range rows {
		values[i] = r.JudgeDollars
	}
	return values
}

func wallClockOf(rows []Row) []float64 {
	values := make([]float64, len(rows))
	for i, r := range rows {
		values[i] = float64(r.WallClockMS)
	}
	return values
}

func turnsOf(rows []Row) []float64 {
	var values []float64
	for _, r := range rows {
		if r.Turns > 0 {
			values = append(values, float64(r.Turns))
		}
	}
	return values
}

func renderFrontier(b *strings.Builder, task string, full []Row) {
	fmt.Fprintf(b, "\nFRONTIER %s (passing runs only)\n", task)
	arms := sortedArms(full)
	byArm := groupByArm(full)
	wall := map[Arm]Spread{}
	for _, a := range arms {
		wall[a] = SpreadOf(wallClockOf(byArm[a]))
		fmt.Fprintf(b, "%s: wall clock %s\n", a, wall[a].Line("%.0f ms"))
	}
	for i := 0; i < len(arms); i++ {
		for j := i + 1; j < len(arms); j++ {
			a, c := arms[i], arms[j]
			kindA, kindC := kindOf(byArm, a), kindOf(byArm, c)
			if kindA != kindC {
				fmt.Fprintf(b, "%s vs %s: wall clock not comparable, credential kinds differ (%s vs %s)\n", a, c, kindA, kindC)
				continue
			}
			compare(b, a, c, "wall clock", "%.0f ms", wall[a], wall[c])
		}
	}
}

func renderDollarsRatio(b *strings.Builder, task string, full []Row) {
	fmt.Fprintf(b, "\nDOLLARS PER PASSING RUN %s\n", task)
	arms := sortedArms(full)
	byArm := groupByArm(full)
	spread := map[Arm]Spread{}
	for _, a := range arms {
		rows := byArm[a]
		judge := SpreadOf(judgeDollarsOf(rows))
		if rows[0].CredentialKind == CredentialKindSubscription {
			fmt.Fprintf(b, "%s: model n/a, spent as subscription quota. jev decisions $%.4f on the openrouter key\n", a, judge.Median)
			continue
		}
		spread[a] = SpreadOf(dollarsOf(rows))
		fmt.Fprintf(b, "%s: model %s. jev decisions $%.4f\n", a, spread[a].Line("$%.4f"), judge.Median)
	}
	for i := 0; i < len(arms); i++ {
		for j := i + 1; j < len(arms); j++ {
			a, c := arms[i], arms[j]
			kindA, kindC := kindOf(byArm, a), kindOf(byArm, c)
			if kindA != kindC {
				fmt.Fprintf(b, "%s vs %s: dollars not comparable, credential kinds differ (%s vs %s)\n", a, c, kindA, kindC)
				continue
			}
			if kindA == CredentialKindSubscription {
				continue
			}
			compare(b, a, c, "dollars", "$%.4f", spread[a], spread[c])
		}
	}
}

func renderTurnsRatio(b *strings.Builder, task string, full []Row) {
	fmt.Fprintf(b, "\nTURNS PER PASSING RUN %s\n", task)
	arms := sortedArms(full)
	byArm := groupByArm(full)
	spread, measured := map[Arm]Spread{}, map[Arm]bool{}
	for _, a := range arms {
		values := turnsOf(byArm[a])
		if len(values) == 0 {
			fmt.Fprintf(b, "%s: turns not recorded on any passing run, so this arm carries no turn measurement\n", a)
			continue
		}
		spread[a], measured[a] = SpreadOf(values), true
		fmt.Fprintf(b, "%s: %s\n", a, spread[a].Line("%.2f"))
	}
	for i := 0; i < len(arms); i++ {
		for j := i + 1; j < len(arms); j++ {
			a, c := arms[i], arms[j]
			var missing []string
			if !measured[a] {
				missing = append(missing, string(a))
			}
			if !measured[c] {
				missing = append(missing, string(c))
			}
			if len(missing) > 0 {
				fmt.Fprintf(b, "%s vs %s: turns not comparable, %s recorded none\n", a, c, strings.Join(missing, " and "))
				continue
			}
			compare(b, a, c, "turns", "%.2f", spread[a], spread[c])
		}
	}
}

func compare(b *strings.Builder, a, c Arm, label, unit string, spreadA, spreadC Spread) {
	var single []string
	if !spreadA.Separable() {
		single = append(single, string(a))
	}
	if !spreadC.Separable() {
		single = append(single, string(c))
	}
	if len(single) > 0 {
		carries := "carries"
		if len(single) > 1 {
			carries = "carry"
		}
		fmt.Fprintf(b, "%s vs %s: %s not separable, %s %s fewer than two passing repeats and one run has no spread\n",
			a, c, label, strings.Join(single, " and "), carries)
		return
	}
	diff := math.Abs(spreadA.Median - spreadC.Median)
	widest := math.Max(spreadA.Width(), spreadC.Width())
	if diff < widest {
		fmt.Fprintf(b, "%s vs %s: no difference (%s diff "+unit+", widest range "+unit+")\n", a, c, label, diff, widest)
		return
	}
	lower := a
	if spreadC.Median < spreadA.Median {
		lower = c
	}
	fmt.Fprintf(b, "%s vs %s: %s lower on %s by "+unit+" (widest range "+unit+")\n", a, c, lower, label, diff, widest)
}

func renderChecklist(b *strings.Builder, task string, gateOK []Row) {
	fmt.Fprintf(b, "\nCHECKLIST %s\n", task)
	arms := sortedArms(gateOK)
	byArm := groupByArm(gateOK)
	for _, a := range arms {
		var passed, total int
		for _, r := range byArm[a] {
			for _, c := range r.Checklist {
				total++
				if c.Passed {
					passed++
				}
			}
		}
		fmt.Fprintf(b, "%s: %d/%d\n", a, passed, total)
	}
}
