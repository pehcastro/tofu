package harness

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"boji/internal/judge/jev/wire/openrouter"
)

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
	for _, task := range tasksOf(rows) {
		renderTask(&b, task, rowsForTask(rows, task))
	}
	return b.String()
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
	gateOK, full := renderGates(b, task, rows)
	renderFrontier(b, task, full)
	renderDollarsRatio(b, task, full)
	renderTurnsRatio(b, task, full)
	renderChecklist(b, task, gateOK)
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

func meanAndSpread(values []float64) (mean, spread float64) {
	if len(values) == 0 {
		return 0, 0
	}
	lo, hi, sum := values[0], values[0], 0.0
	for _, v := range values {
		sum += v
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return sum / float64(len(values)), hi - lo
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
	for i := 0; i < len(arms); i++ {
		for j := i + 1; j < len(arms); j++ {
			a, c := arms[i], arms[j]
			kindA, kindC := kindOf(byArm, a), kindOf(byArm, c)
			if kindA != kindC {
				fmt.Fprintf(b, "%s vs %s: wall clock not comparable, credential kinds differ (%s vs %s)\n", a, c, kindA, kindC)
				continue
			}
			wallA, _ := meanAndSpread(wallClockOf(byArm[a]))
			wallC, _ := meanAndSpread(wallClockOf(byArm[c]))
			fmt.Fprintf(b, "%s vs %s: wall clock %.0fms vs %.0fms\n", a, c, wallA, wallC)
		}
	}
}

func renderDollarsRatio(b *strings.Builder, task string, full []Row) {
	fmt.Fprintf(b, "\nDOLLARS PER PASSING RUN %s\n", task)
	arms := sortedArms(full)
	byArm := groupByArm(full)
	mean, spread := map[Arm]float64{}, map[Arm]float64{}
	for _, a := range arms {
		rows := byArm[a]
		judge, _ := meanAndSpread(judgeDollarsOf(rows))
		if rows[0].CredentialKind == CredentialKindSubscription {
			fmt.Fprintf(b, "%s: model n/a, spent as subscription quota. jev decisions $%.4f on the openrouter key\n", a, judge)
			continue
		}
		m, s := meanAndSpread(dollarsOf(rows))
		mean[a], spread[a] = m, s
		fmt.Fprintf(b, "%s: model $%.4f (spread $%.4f, %d runs). jev decisions $%.4f\n", a, m, s, len(rows), judge)
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
			compare(b, a, c, "dollars", "$%.4f", mean[a], mean[c], spread[a], spread[c])
		}
	}
}

func renderTurnsRatio(b *strings.Builder, task string, full []Row) {
	fmt.Fprintf(b, "\nTURNS PER PASSING RUN %s\n", task)
	arms := sortedArms(full)
	byArm := groupByArm(full)
	mean, spread, measured := map[Arm]float64{}, map[Arm]float64{}, map[Arm]bool{}
	for _, a := range arms {
		values := turnsOf(byArm[a])
		if len(values) == 0 {
			fmt.Fprintf(b, "%s: turns not recorded on any passing run, so this arm carries no turn measurement\n", a)
			continue
		}
		m, s := meanAndSpread(values)
		mean[a], spread[a], measured[a] = m, s, true
		fmt.Fprintf(b, "%s: %.2f (spread %.2f, %d runs)\n", a, m, s, len(values))
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
			compare(b, a, c, "turns", "%.2f", mean[a], mean[c], spread[a], spread[c])
		}
	}
}

func compare(b *strings.Builder, a, c Arm, label, format string, meanA, meanC, spreadA, spreadC float64) {
	diff := meanA - meanC
	if diff < 0 {
		diff = -diff
	}
	widest := spreadA
	if spreadC > widest {
		widest = spreadC
	}
	if diff < widest {
		fmt.Fprintf(b, "%s vs %s: no difference (%s diff "+format+", spread "+format+")\n", a, c, label, diff, widest)
		return
	}
	lower := a
	if meanC < meanA {
		lower = c
	}
	fmt.Fprintf(b, "%s vs %s: %s lower on %s by "+format+" (spread "+format+")\n", a, c, lower, label, diff, widest)
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
