package doctor

import (
	"slices"
	"strconv"
	"strings"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/tools/web/browser/driver"
	"tofu/library/questions"
)

const pointCount = " points"

func reportPage(page cli.Page, report host.DoctorReport) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: string(report.Verdict)}
	if report.Verdict == host.DoctorNotReady {
		verdict.Mark = cli.Fail
	}
	lines := page.Title("Doctor", []string{report.Version, report.Go, report.OS}, verdict)
	if len(report.Blockers) > 0 {
		lines = append(append(lines, ""), wiring.BlockerRows(page, report.Blockers)...)
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

func stepLines(page cli.Page, report host.DoctorReport) []string {
	facts, unattributed := make([]cli.Fact, 0, len(report.Steps)+2), report.TotalMS
	for _, step := range report.Steps {
		facts, unattributed = append(facts, cli.Fact{Label: step.Name, Text: strconv.FormatInt(step.MS, 10) + " ms"}), unattributed-step.MS
	}
	facts = append(facts, cli.Fact{Label: "unattributed", Text: strconv.FormatInt(unattributed, 10) + " ms"}, cli.Fact{Label: "total", Text: strconv.FormatInt(report.TotalMS, 10) + " ms"})
	return append([]string{"", page.Section("time", cli.Verdict{})}, cli.Indent(page.Facts(facts)...)...)
}

func boardLines(page cli.Page, unruled []cli.Problem, fixed []string) []string {
	if len(unruled)+len(fixed) == 0 {
		return nil
	}
	lines := []string{"", page.Section("boards", cli.Verdict{})}
	for _, problem := range unruled {
		said := []string{page.Rows([]cli.Row{{Mark: cli.Warn, Cells: []string{problem.What}}})[0]}
		if problem.Hint != "" {
			said = append(said, cli.Gap+page.Hint(problem.Hint))
		}
		lines = append(lines, cli.Indent(said...)...)
	}
	for _, line := range fixed {
		lines = append(lines, cli.Indent(page.Rows([]cli.Row{{Mark: cli.Done, Cells: []string{line}}})...)...)
	}
	return lines
}

func accessRows(report host.DoctorReport) []cli.Row {
	var rows []cli.Row
	for _, credential := range report.Credentials {
		if credential.State != wiring.UsageServingState {
			rows = append(rows, cli.Row{Mark: cli.Warn, Cells: []string{credential.Provider, credential.State}})
			continue
		}
		var windows []string
		for _, window := range credential.Windows {
			windows = append(windows, window.ID+" "+wiring.WindowPercent(window))
		}
		rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{credential.Provider, strings.Join(windows, output.FactSeparator)}})
	}
	where := ""
	switch report.Gate.Source {
	case gateEnv:
		where = report.Gate.Variable
	case gateDotEnv:
		where = output.RelativeToRoot(report.Root, report.Gate.Path)
	case gateStore:
		where = "credential store"
	}
	if where != "" {
		rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{wiring.JevName, "key" + output.FactSeparator + where}})
	}
	for _, wire := range report.Wires {
		spend := "subscription"
		if wiring.WireSpend(wire.Name) == loop.SpendAPIKey {
			spend = "money"
		}
		rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{wire.Name, spend}})
	}
	return rows
}

func proxyRow(proxy host.DoctorProxy) cli.Row {
	from := "from " + proxy.From
	switch {
	case proxy.Use == "off":
		return cli.Row{Mark: cli.Idle, Cells: []string{"rtk", "off"}, Detail: from}
	case proxy.Unreadable != "":
		return cli.Row{Mark: cli.Warn, Cells: []string{"rtk", "did not answer --version"}, Detail: proxy.Unreadable}
	case proxy.Install != "":
		return cli.Row{Mark: cli.Warn, Cells: []string{"rtk", "not on PATH, so bash commands run as asked"}, Hint: proxy.Install}
	}
	return cli.Row{Mark: cli.Done, Cells: []string{"rtk", proxy.Version + output.FactSeparator + "rewrites every bash command"}, Detail: from}
}

func hostRows(hosts []driver.NativeHost) []cli.Row {
	rows := make([]cli.Row, len(hosts))
	for i, native := range hosts {
		switch native.State {
		case driver.HostInstalled:
			rows[i] = cli.Row{Mark: cli.Done, Cells: []string{native.Browser, "native host installed"}}
		case driver.HostStale:
			rows[i] = cli.Row{Mark: cli.Warn, Cells: []string{native.Browser, "the native host is stale: it runs tofu itself or a copy that is gone"}, Hint: "tofu browser install"}
		case driver.HostMissing:
			rows[i] = cli.Row{Mark: cli.Warn, Cells: []string{native.Browser, "no native host, so tofu cannot reach its tabs"}, Hint: "tofu browser install"}
		default:
			panic("tofu doctor: unknown native host state " + string(native.State))
		}
	}
	return rows
}

func overrideRows(report host.DoctorReport) []cli.Row {
	if report.Unreadable != "" {
		return []cli.Row{{Mark: cli.Fail, Cells: []string{"overrides", "unreadable"}, Detail: report.Unreadable}}
	}
	if len(report.Overrides) == 0 {
		return nil
	}
	var stale []cli.Row
	for _, o := range report.Overrides {
		if o.Stale {
			stale = append(stale, cli.Row{Mark: cli.Warn, Cells: []string{wiring.OverrideRef(o), o.Layer + output.FactSeparator + wiring.OverrideStaleWhy(o)}})
		}
	}
	summary := cli.Row{Mark: cli.Changed, Cells: []string{"overrides", output.Plural(len(report.Overrides), "rule") + " changed by you"}, Hint: "tofu rules overrides"}
	return append([]cli.Row{summary}, stale...)
}

func ruleRows(library host.DoctorLibrary, rules []host.DoctorRule) []cli.Row {
	if library.Unreadable != "" {
		return []cli.Row{{Mark: cli.Fail, Cells: []string{"library", "unreadable"}, Detail: library.Unreadable}}
	}
	source := "binary" + output.FactSeparator + strconv.Itoa(library.Points) + pointCount
	if library.FromProject > 0 {
		source = "project" + output.FactSeparator + strconv.Itoa(library.FromProject) + " of " + strconv.Itoa(library.Points) + pointCount
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
			odd = append(odd, cli.Row{Mark: cli.Fail, Cells: []string{point.Point, "unusable" + output.FactSeparator + point.Unusable}})
			continue
		case point.Fallback != "":
			odd = append(odd, cli.Row{Mark: cli.Warn, Cells: []string{point.Point, point.Mode + output.FactSeparator + point.Fallback}})
			continue
		}
		settled := point.Mode + output.FactSeparator + "thresholds from " + point.ThresholdsFrom
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
		if len(group.points) > 1 && slices.Contains(group.points, questions.ToolGatePin) {
			row.Detail = "gate " + questions.ToolGatePin
		}
		rows = append(rows, row)
	}
	return append(rows, odd...)
}
