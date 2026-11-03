package report

import (
	"fmt"
	"html"
	"strings"
)

const (
	chartLabelWidth = 230.0
	chartBarWidth   = 340.0
	chartRowHeight  = 46.0
	chartWidth      = 800.0
)

func Page(d Data) string {
	shown := summarySections(d.History)
	older := []section{
		{"jev", "Jev against the cheap way", judgmentsHTML(d)},
		{"decides", "What decides what today", wiringHTML(d)},
	}
	page := &strings.Builder{}
	page.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	page.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>tofu, measured</title>\n")
	page.WriteString("<style>" + fontFace() + stylesheet + "</style>\n</head>\n<body>\n<main>\n<h1>tofu against Claude Code, omp and codex</h1>\n<nav class=\"index\">")
	for _, one := range append(append([]section{}, shown...), older...) {
		fmt.Fprintf(page, "<a href=\"#%s\">%s</a>", one.id, html.EscapeString(one.title))
	}
	page.WriteString("</nav>\n" + tldrHTML(d.History) + headlinesHTML(d.History))
	for _, one := range shown {
		fmt.Fprintf(page, "<section id=\"%s\">\n<h2>%s</h2>\n%s</section>\n", one.id, html.EscapeString(one.title), one.body)
	}
	page.WriteString("<section>\n")
	for _, one := range older {
		fmt.Fprintf(page, "<details class=\"older\" id=\"%s\"><summary>%s</summary>\n%s</details>\n", one.id, html.EscapeString(one.title), one.body)
	}
	fmt.Fprintf(page, "</section>\n<footer>tofu %s. Built by <code>%s</code>, fetches nothing.</footer>\n",
		html.EscapeString(d.History.Version), html.EscapeString(d.GeneratedBy))
	page.WriteString("</main>\n<script>" + openTarget + "</script>\n</body>\n</html>\n")
	return page.String()
}

func figureHTML(figure Figure) string { return figureIn("fig", figure) }

func figureIn(class string, figure Figure) string {
	return fmt.Sprintf("<span class=\"%s\" data-figure=\"%s\">%s</span>", class, html.EscapeString(string(figure)), html.EscapeString(string(figure)))
}

func quoteHTML(text, source string) string {
	return fmt.Sprintf("<blockquote>%s<cite>%s</cite></blockquote>\n", html.EscapeString(text), html.EscapeString(source))
}

func statCards(stats []Stat) string {
	cards := &strings.Builder{}
	fmt.Fprintf(cards, "<div class=\"cards\" style=\"--cols:%d\">\n", cardColumns(len(stats)))
	for _, stat := range stats {
		fmt.Fprintf(cards, "<div class=\"card\"><div class=\"card-header\"><p class=\"card-description\">%s</p></div><div class=\"card-content\"><p class=\"card-title\">%s</p><p class=\"card-description\">%s</p></div></div>\n",
			html.EscapeString(stat.Label), figureHTML(stat.Figure), html.EscapeString(stat.Note))
	}
	cards.WriteString("</div>\n")
	return cards.String()
}

func cardColumns(count int) int {
	if count <= 5 {
		return max(count, 1)
	}
	for _, columns := range []int{5, 4, 3} {
		if count%columns == 0 {
			return columns
		}
	}
	return 4
}

func verdictClass(verdict Verdict) string {
	switch verdict {
	case Ahead:
		return "win"
	case Behind:
		return "loss"
	case Level, Described:
		return ""
	default:
		panic("unknown verdict " + string(verdict))
	}
}

func measureHead(m Measure) string {
	return fmt.Sprintf("<td class=\"table-cell\">%s<span class=\"detail\">%s, %s is better</span><span class=\"detail\"><code>bench/%s</code></span></td>",
		html.EscapeString(m.Name), html.EscapeString(m.Task), m.Better, html.EscapeString(m.Bench))
}

func tableOf(caption string, heads []string, rows string) string {
	table := &strings.Builder{}
	table.WriteString("<div class=\"table-container\"><table class=\"table\">\n")
	if caption != "" {
		fmt.Fprintf(table, "<caption class=\"table-caption\">%s</caption>\n", html.EscapeString(caption))
	}
	table.WriteString("<thead><tr class=\"table-row\">")
	for _, head := range heads {
		fmt.Fprintf(table, "<th class=\"table-head\">%s</th>", head)
	}
	table.WriteString("</tr></thead>\n<tbody>\n" + rows + "</tbody></table></div>\n")
	return table.String()
}

func rangesHTML(rival string, measures []Measure) string {
	rows := &strings.Builder{}
	for _, m := range measures {
		fmt.Fprintf(rows, "<tr class=\"table-row\">%s<td class=\"table-cell\">%s</td><td class=\"table-cell %s\">%s</td></tr>\n",
			measureHead(m), figureHTML(m.Shown(m.Latest())), verdictClass(m.Against(m.Rivals[0])), figureHTML(m.Shown(m.Rivals[0])))
	}
	return tableOf("Median, lowest and highest in brackets.", []string{"What is measured", "tofu", html.EscapeString(rival)}, rows.String())
}

func changesHTML(measures []Measure) string {
	rows := &strings.Builder{}
	for _, m := range measures {
		rows.WriteString("<tr class=\"table-row\">" + measureHead(m) + "<td class=\"table-cell\"><ul>")
		for at, run := range m.Tofu {
			fmt.Fprintf(rows, "<li><strong>%s</strong> %s<span class=\"detail\">%s</span></li>",
				html.EscapeString(run.Label), figureHTML(m.Shown(run)), html.EscapeString(m.Change(at)))
		}
		rows.WriteString("</ul></td></tr>\n")
	}
	return tableOf("Each run and the change from the run before it.",
		[]string{"What is measured", "Every run, oldest first"}, rows.String())
}

func wiringHTML(d Data) string {
	body := &strings.Builder{}
	fmt.Fprintf(body, "<p class=\"lede\">%s decide anything when you run tofu today, read from <code>%s</code>.</p>\n",
		figureHTML(d.PointsOn()), html.EscapeString(d.MethodTable))
	rows := &strings.Builder{}
	for _, row := range d.Wiring {
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\"><code>%s</code><span class=\"detail\"><code>%s</code></span></td>",
			html.EscapeString(row.Point), html.EscapeString(row.Measured))
		fmt.Fprintf(rows, "<td class=\"table-cell\"><span class=\"badge\" data-variant=\"%s\">%s</span></td><td class=\"table-cell\">%s</td>",
			useVariant(row.On), html.EscapeString(row.SwitchedOn), quoteHTML(row.Costs, d.MethodTable))
		fmt.Fprintf(rows, "<td class=\"table-cell\">%s</td><td class=\"table-cell\"><code>%s</code></td></tr>\n",
			quoteHTML(row.Why, d.MethodTable), html.EscapeString(row.CalledBy))
	}
	body.WriteString(tableOf("One row per decision point, what decides it now, and what that costs a session.",
		[]string{"What decides", "Switched on", "What it costs", "Why", "Called by"}, rows.String()))
	return body.String()
}

func judgmentsHTML(d Data) string {
	body := &strings.Builder{}
	stats := []Stat{
		{Label: "Compared", Figure: d.outOfDecisions(d.ComparedDecisions()), Note: "a cheaper method measured beside Jev"},
		{Label: "Not compared", Figure: d.outOfDecisions(len(d.Judgments) - d.ComparedDecisions()), Note: "no usable score on one side"},
		{Label: "Switched on", Figure: d.PointsOn(), Note: "read from " + d.MethodTable},
	}
	body.WriteString(statCards(append(stats, d.Answers.JevCosts...)))
	body.WriteString(gapChart(d.Judgments))
	rows := &strings.Builder{}
	for _, row := range d.Judgments {
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s<span class=\"detail\">%s</span><span class=\"detail\"><code>%s</code></span></td>",
			html.EscapeString(row.Question), html.EscapeString(row.Axis), html.EscapeString(row.Point))
		if row.Compared() {
			fmt.Fprintf(rows, "%s%s<td class=\"table-cell %s\">%s</td><td class=\"table-cell\">%s, %s</td>",
				methodCell(row.Without), methodCell(row.With), winnerClass(row), html.EscapeString(row.Winner),
				figureHTML(row.Gap), figureHTML(row.Multiple))
		} else {
			fmt.Fprintf(rows, "<td class=\"table-cell missing\" colspan=\"4\"><strong>Not compared.</strong> %s</td>", html.EscapeString(row.NotCompared))
		}
		fmt.Fprintf(rows, "<td class=\"table-cell\">%s</td><td class=\"table-cell\"><span class=\"badge\" data-variant=\"%s\">%s</span></td></tr>\n",
			figureIn("fig sample", row.Sample), useVariant(row.Wired), html.EscapeString(row.SwitchedOn))
	}
	body.WriteString(tableOf("Each row is one decision, the cheapest method anyone has written for it, and Jev, on the same set of cases.",
		[]string{"What decides", "Method", "Method", "Winner", "By how much", "Measured on", "Switched on"}, rows.String()))
	return body.String()
}

func methodCell(method Reading) string {
	return fmt.Sprintf("<td class=\"table-cell\"><strong>%s</strong> %s<span class=\"detail\">%s</span></td>",
		html.EscapeString(method.Name), figureHTML(method.Reading), html.EscapeString(method.Does))
}

func winnerClass(row JudgmentRow) string {
	switch row.Winner {
	case row.With.Name:
		return "win"
	case aTie:
		return ""
	default:
		return "loss"
	}
}

func useVariant(wired bool) string {
	if wired {
		return "default"
	}
	return "outline"
}

func gapChart(rows []JudgmentRow) string {
	var drawn []JudgmentRow
	for _, row := range rows {
		if row.Compared() {
			drawn = append(drawn, row)
		}
	}
	height := float64(len(drawn))*chartRowHeight + 34
	chart := &strings.Builder{}
	fmt.Fprintf(chart, "<svg class=\"gapchart\" viewBox=\"0 0 %.0f %.0f\" role=\"img\" aria-label=\"the cheaper method against Jev on every decision where both were measured\">\n", chartWidth, height)
	fmt.Fprintf(chart, "<rect class=\"free\" x=\"%.0f\" y=\"6\" width=\"11\" height=\"11\" rx=\"2\"/><text x=\"%.0f\" y=\"16\">the cheaper method</text>",
		chartLabelWidth, chartLabelWidth+18)
	fmt.Fprintf(chart, "<rect class=\"judged\" x=\"%.0f\" y=\"6\" width=\"11\" height=\"11\" rx=\"2\"/><text x=\"%.0f\" y=\"16\">Jev</text>\n",
		chartLabelWidth+150, chartLabelWidth+168)
	for i, row := range drawn {
		top := 34 + float64(i)*chartRowHeight
		fmt.Fprintf(chart, "<text class=\"point\" x=\"%.0f\" y=\"%.0f\">%s</text>\n", chartLabelWidth-14, top+18, html.EscapeString(row.Question))
		for at, method := range []struct {
			class   string
			reading Reading
		}{{"free", row.Without}, {"judged", row.With}} {
			width := method.reading.Percent / 100 * chartBarWidth
			y := top + float64(at)*17
			fmt.Fprintf(chart, "<rect class=\"%s\" x=\"%.0f\" y=\"%.1f\" width=\"%.1f\" height=\"13\" rx=\"2\"/><text class=\"reading\" data-figure=\"%s\" x=\"%.1f\" y=\"%.1f\">%s, %s</text>\n",
				method.class, chartLabelWidth, y, width, html.EscapeString(string(method.reading.Reading)),
				chartLabelWidth+width+7, y+11, html.EscapeString(method.reading.Name), html.EscapeString(string(method.reading.Reading)))
		}
	}
	chart.WriteString("</svg>\n")
	return chart.String()
}
