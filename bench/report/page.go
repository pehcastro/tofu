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

type panel struct {
	id      string
	tab     string
	heading string
	body    string
}

func Page(d Data) string {
	panels := []panel{
		{"arms", d.Answers.Arms.Tab, d.Answers.Arms.Question, refusedHTML(d.Answers.Arms)},
		{"versions", d.Answers.Versions.Tab, d.Answers.Versions.Question, refusedHTML(d.Answers.Versions)},
		{"judgments", "3. Jev against the cheap way", "Which decisions Jev wins, which it loses, and by how much", judgmentsHTML(d)},
		{"reports", "Every measurement on disk", "Every dated report under bench/, newest first", reportsHTML(d)},
	}
	open := "judgments"
	page := &strings.Builder{}
	page.WriteString("<!doctype html>\n<html lang=\"en\" class=\"dark\">\n<head>\n<meta charset=\"utf-8\">\n")
	page.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>Tofu bench</title>\n")
	page.WriteString("<style>" + shadcnTokens + shadcnComponents + pageLayout + "</style>\n</head>\n<body>\n<main>\n")
	page.WriteString("<h1>What bench has measured</h1>\n")
	page.WriteString("<div role=\"tablist\" class=\"tab-list\" aria-label=\"the three questions\">\n")
	for _, one := range panels {
		fmt.Fprintf(page, "<button role=\"tab\" class=\"tab-trigger\" id=\"tab-%s\" aria-controls=\"panel-%s\" aria-selected=\"%t\"%s>%s</button>\n",
			one.id, one.id, one.id == open, unlessOpen(one.id == open, " tabindex=\"-1\""), html.EscapeString(one.tab))
	}
	page.WriteString("</div>\n")
	for _, one := range panels {
		fmt.Fprintf(page, "<div role=\"tabpanel\" class=\"tab-content\" id=\"panel-%s\" aria-labelledby=\"tab-%s\" tabindex=\"0\"%s>\n<h2>%s</h2>\n%s</div>\n",
			one.id, one.id, unlessOpen(one.id == open, " hidden"), html.EscapeString(one.heading), one.body)
	}
	fmt.Fprintf(page, "<footer><code>%s</code> &middot; <code>%s</code> &middot; fetches nothing</footer>\n",
		html.EscapeString(d.GeneratedBy), AnswersPath)
	page.WriteString("</main>\n<script>" + tabScript + "</script>\n</body>\n</html>\n")
	return page.String()
}

func unlessOpen(open bool, attribute string) string {
	if open {
		return ""
	}
	return attribute
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

func refusedHTML(section Refused) string {
	body := &strings.Builder{}
	fmt.Fprintf(body, "<p class=\"lede\">No answer today. %s</p>\n<div class=\"cards\" style=\"--cols:2\">\n", html.EscapeString(section.Because))
	for _, group := range []struct {
		title  string
		why    string
		claims []Claim
	}{
		{"What exists", "read from " + section.Source, section.WhatExists},
		{"What it would take", "the run that would fill this table", section.WhatItWouldTake},
	} {
		fmt.Fprintf(body, "<div class=\"card\"><div class=\"card-header\"><h3 class=\"card-title\">%s</h3><p class=\"card-description\">%s</p></div><div class=\"card-content\"><ul>\n",
			html.EscapeString(group.title), html.EscapeString(group.why))
		for _, claim := range group.claims {
			fmt.Fprintf(body, "<li>%s</li>\n", html.EscapeString(claim.Text))
		}
		body.WriteString("</ul></div></div>\n")
	}
	body.WriteString("</div>\n")
	return body.String()
}

func judgmentsHTML(d Data) string {
	body := &strings.Builder{}
	body.WriteString(statCards(append([]Stat{
		{Label: "Compared", Figure: d.outOfDecisions(d.ComparedDecisions()), Note: "a cheaper method measured beside Jev"},
		{Label: "Not compared", Figure: d.outOfDecisions(len(d.Judgments) - d.ComparedDecisions()), Note: "no usable score on one side"},
		{Label: "Switched on", Figure: d.outOfDecisions(d.SwitchedOnDecisions()), Note: "every rule file ships switched off"},
	}, d.Answers.JevCosts...)))
	body.WriteString(gapChart(d.Judgments))
	body.WriteString("<div class=\"table-container\"><table class=\"table\">\n<caption class=\"table-caption\">Each row is one decision, the cheapest method anyone has written for it, and Jev, on the same set of cases.</caption>\n")
	body.WriteString("<thead><tr class=\"table-row\">")
	for _, head := range []string{"What decides", "Method", "Method", "Winner", "By how much", "Measured on", "Switched on"} {
		fmt.Fprintf(body, "<th class=\"table-head\">%s</th>", head)
	}
	body.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range d.Judgments {
		fmt.Fprintf(body, "<tr class=\"table-row\"><td class=\"table-cell\">%s<span class=\"detail\">%s</span><span class=\"detail\"><code>%s</code></span></td>",
			html.EscapeString(row.Question), html.EscapeString(row.Axis), html.EscapeString(row.Point))
		if row.Compared() {
			fmt.Fprintf(body, "%s%s<td class=\"table-cell %s\">%s</td><td class=\"table-cell\">%s, %s</td>",
				methodCell(row.Without), methodCell(row.With), winnerClass(row), html.EscapeString(row.Winner),
				figureHTML(row.Gap), figureHTML(row.Multiple))
		} else {
			fmt.Fprintf(body, "<td class=\"table-cell missing\" colspan=\"4\"><strong>Not compared.</strong> %s</td>", html.EscapeString(row.NotCompared))
		}
		fmt.Fprintf(body, "<td class=\"table-cell\">%s</td><td class=\"table-cell\"><span class=\"badge\" data-variant=\"%s\">%s</span><span class=\"detail\"><code>%s</code></span></td></tr>\n",
			figureIn("fig sample", row.Sample), useVariant(row.Wired), html.EscapeString(row.SwitchedOn), html.EscapeString(row.Source))
	}
	body.WriteString("</tbody></table></div>\n")
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

func reportsHTML(d Data) string {
	body := &strings.Builder{}
	benches := d.Counts.MeasuredPackage + d.Counts.NoReport
	body.WriteString(statCards([]Stat{
		{Label: "Written up", Figure: countFigure(d.Counts.Reports, "dated reports"), Note: "one file each, under bench/"},
		{Label: "Benches measuring", Figure: countFigure(d.Counts.MeasuredPackage, "of "+fmt.Sprint(benches)), Note: "the rest are named below, not hidden"},
		{Label: "Withdrawn", Figure: countFigure(d.Counts.Withdrawn, "of "+fmt.Sprint(d.Counts.Reports)), Note: "struck rather than deleted"},
		{Label: "Stale", Figure: countFigure(d.Counts.Stale, "of "+fmt.Sprint(d.Counts.Reports)), Note: "measured against a corpus that moved"},
		{Label: "Naming no answer", Figure: countFigure(d.Counts.Unparsed, "of "+fmt.Sprint(d.Counts.Reports)), Note: "no heading names the file's own conclusion"},
	}))
	for _, report := range d.Reports {
		fmt.Fprintf(body, "<details><summary>%s, %s <span class=\"path\">%s</span>%s</summary>\n",
			html.EscapeString(report.Package), html.EscapeString(report.Date), html.EscapeString(report.Source), stateBadge(report.State))
		if report.StateNote != "" {
			body.WriteString(quoteHTML(report.StateNote, string(report.State)+", said by "+report.StateSource))
		}
		body.WriteString("<h3>What it found, in the report's own words</h3>\n")
		body.WriteString(quoteHTML(report.Conclusion, report.Source))
		body.WriteString("<h3>What it rests on</h3>\n")
		body.WriteString(quoteHTML(report.Sample, report.Source))
		if report.Skips != "" {
			body.WriteString(quoteHTML("Skipped: "+report.Skips, report.Source))
		}
		fmt.Fprintf(body, "<span class=\"badge\" data-variant=\"outline\">built from %s</span>\n</details>\n", html.EscapeString(string(report.BuiltFrom)))
	}
	body.WriteString("<h3>Benches with no dated report</h3>\n<ul>\n")
	for _, pkg := range d.NoReport {
		fmt.Fprintf(body, "<li><code>bench/%s</code>, %s: %s</li>\n", html.EscapeString(pkg.Package), html.EscapeString(string(pkg.Kind)), html.EscapeString(pkg.Note))
	}
	body.WriteString("</ul>\n")
	return body.String()
}

func stateBadge(state State) string {
	if state.Stands() {
		return ""
	}
	return fmt.Sprintf("<span class=\"badge\" data-variant=\"secondary\">%s</span>", html.EscapeString(string(state)))
}
