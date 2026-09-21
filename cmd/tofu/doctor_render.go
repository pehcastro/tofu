package main

import (
	"fmt"
	"strings"

	"tofu/internal/turn"
)

func doctorText(report doctorReport, shade palette) string {
	verdict := report.Verdict.String()
	painted := shade.settled(verdict)
	if report.Verdict == doctorNotReady {
		painted = shade.unsettled(verdict)
	}
	var body strings.Builder
	body.WriteString(headline("tofu "+report.Version, painted, len(verdict)) + "\n")
	for _, group := range doctorGroups(report) {
		body.WriteString("\n")
		for _, line := range group {
			body.WriteString(line + "\n")
		}
	}
	return body.String() + "\n" + reportIndent + report.Go + ", " + report.OS + ", in " + report.Root + "\n"
}

func doctorGroups(report doctorReport) [][]string {
	line := func(label, text string) []string { return wrapped(label, relativeToRoot(report.Root, text)) }
	var groups [][]string
	var blockers []string
	for _, blocker := range report.Blockers {
		blockers = append(blockers, blockerLines(blocker.Label, blocker.What, blocker.Command)...)
	}
	if len(blockers) > 0 {
		groups = append(groups, blockers)
	}

	var access []string
	for _, credential := range report.Credentials {
		if credential.State != usageServingState {
			access = append(access, line(credential.Provider, credential.State)...)
			continue
		}
		access = append(access, labelled(credential.Provider, credentialText(credential)))
	}
	if report.Gate.Source == doctorGateEnv {
		access = append(access, labelled(jevName, "key from "+report.Gate.Variable+" in the environment"))
	}
	if report.Gate.Source == doctorGateDotEnv {
		access = append(access, line(jevName, "key from "+report.Gate.Path)...)
	}
	access = append(access, line("wires", wireText(report.Wires))...)
	groups = append(groups, access)

	state := line("catalog", catalogText(report.Catalog))
	state = append(state, ruleText(report.Rules, report.Root)...)
	state = append(state, line("calibration", report.Calibration)...)
	state = append(state, line("ledger", report.Ledger)...)
	return append(groups, state)
}

func credentialText(credential credentialReport) string {
	used := ""
	for _, window := range credential.Windows {
		column := window.ID + " " + window.percent()
		pad := doctorWindowColumn - len(column)
		if pad < 1 {
			pad = 1
		}
		used += column + strings.Repeat(" ", pad)
	}
	return strings.TrimRight(used, " ")
}

func wireText(wires []doctorWire) string {
	var free, paid []string
	for _, wire := range wires {
		if wireSpend(wire.Name) == turn.SpendAPIKey {
			paid = append(paid, wire.Name)
			continue
		}
		free = append(free, wire.Name)
	}
	text := strings.Join(free, " and ") + " spend subscription quota"
	if len(paid) == 0 {
		return text
	}
	return text + ", " + strings.Join(paid, " and ") + " spends money"
}

func catalogText(catalog doctorCatalog) string {
	if catalog.Unreadable != "" {
		return doctorUnreadable + catalog.Unreadable
	}
	if catalog.FromProject > 0 {
		return fmt.Sprintf("the project's own, %d of %d points", catalog.FromProject, catalog.Points)
	}
	return fmt.Sprintf("the one in the binary, %d points, nothing overrides it in %s", catalog.Points, catalog.Dir)
}

type ruleGroup struct {
	settled string
	count   int
	point   string
	decides bool
}

func ruleText(rules []doctorRule, root string) []string {
	var groups []ruleGroup
	var odd []string
	for _, point := range rules {
		if point.Unusable != "" {
			odd = append(odd, point.Point+" unusable, "+point.Unusable)
			continue
		}
		if point.Fallback != "" {
			odd = append(odd, point.Point+" "+point.Mode+", "+point.Fallback)
			continue
		}
		settled := point.Mode + ", thresholds from " + point.ThresholdsFrom
		found := false
		for index := range groups {
			if groups[index].settled != settled {
				continue
			}
			groups[index].count++
			groups[index].decides = groups[index].decides || point.Point == runGatePoint
			found = true
		}
		if !found {
			groups = append(groups, ruleGroup{settled: settled, count: 1, point: point.Point, decides: point.Point == runGatePoint})
		}
	}
	var lines []string
	label := "rules"
	for _, group := range groups {
		head := group.point + " " + group.settled
		if group.count > 1 {
			head = fmt.Sprintf("%d points, all %s", group.count, group.settled)
		}
		lines = append(lines, wrapped(label, relativeToRoot(root, head))...)
		label = ""
		if group.decides && group.count > 1 {
			lines = append(lines, labelled("", runGatePoint+doctorGateDecides))
		}
	}
	for _, line := range odd {
		lines = append(lines, wrapped(label, relativeToRoot(root, line))...)
		label = ""
	}
	return lines
}
