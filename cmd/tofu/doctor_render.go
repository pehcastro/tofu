package main

import (
	"slices"
	"strconv"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/browser"
	"tofu/internal/judge/ledger"
	"tofu/internal/turn"
)

const (
	factSeparator = " · "
	pointCount    = " points"
)

func doctorPage(page cli.Page, report doctorReport) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: report.Verdict.String()}
	if report.Verdict == doctorNotReady {
		verdict.Mark = cli.Fail
	}
	lines := page.Title("Doctor", []string{report.Version, report.Go, report.OS}, verdict)
	if len(report.Blockers) > 0 {
		lines = append(append(lines, ""), blockerRows(page, report.Blockers)...)
	}
	lines = append(lines, "", page.Section("access", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Rows(accessRows(report))...)...)
	lines = append(lines, "", page.Section("shell", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Rows([]cli.Row{proxyRow(report.Proxy)})...)...)
	lines = append(lines, "", page.Section("browser", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Rows(hostRows(report.Browser))...)...)
	lines = append(lines, "", page.Section("rules", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Rows(append(ruleRows(report.Library, report.Rules), overrideRows(report)...))...)...)
	calibration, _, _ := strings.Cut(report.Calibration, ",")
	lines = append(lines, "", page.Section("state", cli.Verdict{}))
	return append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "calibration", Text: calibration}, {Label: "ledger", Text: report.Ledger}})...)...)
}

func blockerRows(page cli.Page, blockers []doctorBlocker) []string {
	rows := make([]cli.Row, len(blockers))
	for i, blocker := range blockers {
		rows[i] = cli.Row{Mark: cli.Fail, Cells: []string{blocker.Label, blocker.What}}
	}
	var lines []string
	for i, line := range page.Rows(rows) {
		lines = append(lines, cli.Indent(line, cli.Gap+page.Hint(blockers[i].Command))...)
	}
	return lines
}

func accessRows(report doctorReport) []cli.Row {
	var rows []cli.Row
	for _, credential := range report.Credentials {
		if credential.State != usageServingState {
			rows = append(rows, cli.Row{Mark: cli.Warn, Cells: []string{credential.Provider, credential.State}})
			continue
		}
		var windows []string
		for _, window := range credential.Windows {
			windows = append(windows, window.ID+" "+window.percent())
		}
		rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{credential.Provider, strings.Join(windows, factSeparator)}})
	}
	where := ""
	switch report.Gate.Source {
	case doctorGateEnv:
		where = report.Gate.Variable
	case doctorGateDotEnv:
		where = relativeToRoot(report.Root, report.Gate.Path)
	case doctorGateStore:
		where = "credential store"
	}
	if where != "" {
		rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{jevName, "key" + factSeparator + where}})
	}
	for _, wire := range report.Wires {
		spend := "subscription"
		if wireSpend(wire.Name) == turn.SpendAPIKey {
			spend = "money"
		}
		rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{wire.Name, spend}})
	}
	return rows
}

func proxyRow(proxy doctorProxy) cli.Row {
	from := "from " + proxy.From
	switch {
	case proxy.Use == "off":
		return cli.Row{Mark: cli.Idle, Cells: []string{"rtk", "off"}, Detail: from}
	case proxy.Unreadable != "":
		return cli.Row{Mark: cli.Warn, Cells: []string{"rtk", "did not answer --version"}, Detail: proxy.Unreadable}
	case proxy.Install != "":
		return cli.Row{Mark: cli.Warn, Cells: []string{"rtk", "not on PATH, so bash commands run as asked"}, Hint: proxy.Install}
	}
	return cli.Row{Mark: cli.Done, Cells: []string{"rtk", proxy.Version + factSeparator + "rewrites every bash command"}, Detail: from}
}

func hostRows(hosts []browser.NativeHost) []cli.Row {
	rows := make([]cli.Row, len(hosts))
	for i, host := range hosts {
		switch host.State {
		case browser.HostInstalled:
			rows[i] = cli.Row{Mark: cli.Done, Cells: []string{host.Browser, "native host installed"}}
		case browser.HostStale:
			rows[i] = cli.Row{Mark: cli.Warn, Cells: []string{host.Browser, "the native host names a tofu that is gone"}, Hint: "tofu browser install"}
		case browser.HostMissing:
			rows[i] = cli.Row{Mark: cli.Warn, Cells: []string{host.Browser, "no native host, so tofu cannot reach its tabs"}, Hint: "tofu browser install"}
		default:
			panic("tofu doctor: unknown native host state " + string(host.State))
		}
	}
	return rows
}

func overrideRows(report doctorReport) []cli.Row {
	if report.Unreadable != "" {
		return []cli.Row{{Mark: cli.Fail, Cells: []string{"overrides", "unreadable"}, Detail: report.Unreadable}}
	}
	if len(report.Overrides) == 0 {
		return nil
	}
	var stale []cli.Row
	for _, o := range report.Overrides {
		if o.Stale {
			stale = append(stale, cli.Row{Mark: cli.Warn, Cells: []string{o.ref(), o.Layer + factSeparator + o.staleWhy()}})
		}
	}
	summary := cli.Row{Mark: cli.Changed, Cells: []string{"overrides", plural(len(report.Overrides), "rule") + " changed by you"}, Hint: "tofu rules overrides"}
	return append([]cli.Row{summary}, stale...)
}

func ruleRows(library doctorLibrary, rules []doctorRule) []cli.Row {
	if library.Unreadable != "" {
		return []cli.Row{{Mark: cli.Fail, Cells: []string{"library", "unreadable"}, Detail: library.Unreadable}}
	}
	source := "binary" + factSeparator + strconv.Itoa(library.Points) + pointCount
	if library.FromProject > 0 {
		source = "project" + factSeparator + strconv.Itoa(library.FromProject) + " of " + strconv.Itoa(library.Points) + pointCount
	}
	rows := []cli.Row{{Mark: cli.Done, Cells: []string{"library", source}}}
	type ruleGroup struct {
		mark    cli.Mark
		settled string
		points  []string
	}
	var groups []ruleGroup
	var odd []cli.Row
	for _, point := range rules {
		switch {
		case point.Unusable != "":
			odd = append(odd, cli.Row{Mark: cli.Fail, Cells: []string{point.Point, "unusable" + factSeparator + point.Unusable}})
			continue
		case point.Fallback != "":
			odd = append(odd, cli.Row{Mark: cli.Warn, Cells: []string{point.Point, point.Mode + factSeparator + point.Fallback}})
			continue
		}
		settled := point.Mode + factSeparator + "thresholds from " + point.ThresholdsFrom
		found := false
		for i := range groups {
			if groups[i].settled == settled {
				groups[i].points, found = append(groups[i].points, point.Point), true
			}
		}
		if !found {
			mark := cli.Idle
			if point.Mode == ledger.ModeEnforced.String() {
				mark = cli.Active
			}
			groups = append(groups, ruleGroup{mark: mark, settled: settled, points: []string{point.Point}})
		}
	}
	for _, group := range groups {
		row := cli.Row{Mark: group.mark, Cells: []string{group.points[0], group.settled}}
		if len(group.points) > 1 {
			row.Cells[0] = strconv.Itoa(len(group.points)) + pointCount
		}
		if len(group.points) > 1 && slices.Contains(group.points, runGatePoint) {
			row.Detail = "gate " + runGatePoint
		}
		rows = append(rows, row)
	}
	return append(rows, odd...)
}
