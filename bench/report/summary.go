package report

import (
	"fmt"
	"html"
	"math"
	"slices"
	"strconv"
	"strings"
)

const (
	perSubAgent = "Per sub-agent"
	wallTime    = "Sub-agent wall time"
)

type section struct {
	id    string
	title string
	body  string
}

func cellNumber(run Run) string {
	head, _, _ := strings.Cut(run.Figure, " [")
	if rest, over := strings.CutPrefix(head, "over "); over {
		return ">" + rest
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(head, ",", ""), 64)
	switch {
	case err != nil && strings.Contains(head, " and "):
		return strconv.FormatFloat(run.Value, 'g', 3, 64)
	case err != nil:
		return head
	case value >= 10000 && value == math.Trunc(value):
		return withThousands(int(value))
	default:
		return head
	}
}

func withThousands(value int) string {
	digits := strconv.Itoa(value)
	for at := len(digits) - 3; at > 0; at -= 3 {
		digits = digits[:at] + "," + digits[at:]
	}
	return digits
}

func measuresIn(h History, pairing string) []Measure {
	var found []Measure
	for _, m := range h.Measures {
		if m.Pairing == pairing {
			found = append(found, m)
		}
	}
	return found
}

func headlinesHTML(h History) string {
	body := &strings.Builder{}
	body.WriteString("<div class=\"stats\">\n")
	for _, one := range h.Stated {
		fmt.Fprintf(body, "<div class=\"stat\"><div class=\"stat-value\">%s</div><div class=\"stat-line\">%s</div></div>\n",
			html.EscapeString(one.Value), html.EscapeString(one.Says))
	}
	body.WriteString("</div>\n")
	return body.String()
}

func tldrHTML(h History) string {
	shorts := map[Verdict][]string{}
	for _, one := range h.Standings() {
		short := one.Measure.Short
		if one.Verdict == Behind {
			labels := make([]string, 0, len(one.Rivals))
			for _, rival := range one.Rivals {
				labels = append(labels, rival.Label)
			}
			short += " vs " + strings.Join(labels, ", ")
		}
		if !slices.Contains(shorts[one.Verdict], short) {
			shorts[one.Verdict] = append(shorts[one.Verdict], short)
		}
	}
	body := &strings.Builder{}
	body.WriteString("<ul class=\"tldr\">\n")
	for _, verdict := range ranked {
		fmt.Fprintf(body, "<li><span class=\"pill %s\">%s on %d</span> %s</li>\n",
			verdictClass(verdict), verdict, len(shorts[verdict]), html.EscapeString(strings.Join(shorts[verdict], ", ")))
	}
	body.WriteString("</ul>\n")
	return body.String()
}

func measuredOn(measures []Measure) string {
	var said []string
	for _, m := range measures {
		latest := m.Latest()
		line := "tofu " + latest.Version + ", " + latest.Label + ", " + latest.Date + ", bench/" + m.Bench
		if !slices.Contains(said, line) {
			said = append(said, line)
		}
	}
	return fmt.Sprintf("<div class=\"tag\">measured on %s</div>\n", html.EscapeString(strings.Join(said, "; ")))
}

func foldHTML(body string, measures []Measure) string {
	return "<details><summary>Runs and ranges</summary>\n" + body + measuredOn(measures) + "</details>\n"
}

func numberCell(text, class string) string {
	return fmt.Sprintf("<td class=\"table-cell num %s\">%s</td>", class, html.EscapeString(text))
}

func bestClass(best bool) string {
	if best {
		return "best"
	}
	return ""
}

func bestOf(m Measure, runs []Run) float64 {
	best := runs[0].Value
	for _, run := range runs[1:] {
		if (m.Better == BetterLower) == (run.Value < best) {
			best = run.Value
		}
	}
	return best
}

func pillHTML(verdict Verdict, text string) string {
	return fmt.Sprintf("<span class=\"pill %s\">%s</span>", verdictClass(verdict), html.EscapeString(text))
}

func pairingHTML(h History, pairing string) string {
	measures := measuresIn(h, pairing)
	if len(measures) == 0 {
		return ""
	}
	labels := make([]string, 0, len(measures[0].Rivals))
	for _, rival := range measures[0].Rivals {
		labels = append(labels, rival.Label)
	}
	rows := &strings.Builder{}
	for _, m := range measures {
		runs := append([]Run{m.Latest()}, m.Rivals...)
		best := bestOf(m, runs)
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s</td>", html.EscapeString(m.Short))
		rows.WriteString(numberCell(cellNumber(m.Latest()), "tofu "+bestClass(m.Latest().Value == best)))
		var pills []string
		for _, label := range labels {
			at := slices.IndexFunc(m.Rivals, func(rival Run) bool { return rival.Label == label })
			if at < 0 {
				rows.WriteString(numberCell("-", ""))
				continue
			}
			rival := m.Rivals[at]
			rows.WriteString(numberCell(cellNumber(rival), bestClass(rival.Value == best)))
			said := m.Words(m.Latest(), rival)
			if m.Against(rival) == Level && said != "same" {
				said += ", within run spread"
			}
			if len(labels) > 1 {
				said += " vs " + label
			}
			pills = append(pills, pillHTML(m.Against(rival), said))
		}
		fmt.Fprintf(rows, "<td class=\"table-cell\">%s</td></tr>\n", strings.Join(pills, " "))
	}
	heads := append(append([]string{"Median", "tofu"}, labels...), "tofu is")
	return tableOf("", heads, rows.String()) + foldHTML(rivalsHTML(measures), measures)
}

func scalingHTML(h History) string {
	measures := measuresIn(h, perSubAgent)
	if len(measures) == 0 {
		return ""
	}
	tools := []string{"tofu"}
	for _, rival := range measures[0].Rivals {
		tools = append(tools, rival.Label)
	}
	rows := &strings.Builder{}
	for _, tool := range tools {
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s</td>", html.EscapeString(tool))
		for _, m := range measures {
			run := m.Latest()
			if at := slices.IndexFunc(m.Rivals, func(rival Run) bool { return rival.Label == tool }); at >= 0 {
				run = m.Rivals[at]
			}
			rows.WriteString(numberCell(cellNumber(run), bestClass(run.Value == bestOf(m, append([]Run{m.Latest()}, m.Rivals...)))))
		}
		rows.WriteString("</tr>\n")
	}
	heads := []string{"Tool"}
	for _, m := range measures {
		heads = append(heads, m.Short)
	}
	return tableOf("", heads, rows.String()) + foldHTML(rivalsHTML(measures), measures)
}

func wallHTML(h History) string {
	measures := measuresIn(h, wallTime)
	if len(measures) == 0 {
		return ""
	}
	rows := &strings.Builder{}
	for at, row := range measures[0].Tofu {
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s</td>", html.EscapeString(row.Label))
		for _, m := range measures {
			rows.WriteString(numberCell(cellNumber(m.Tofu[at]), ""))
		}
		rows.WriteString("</tr>\n")
	}
	heads := []string{"Run"}
	for _, m := range measures {
		heads = append(heads, m.Short)
	}
	return tableOf("", heads, rows.String()) + foldHTML(changesHTML(measures), measures)
}

func versionsHTML(h History) string {
	rows := &strings.Builder{}
	var shown, rest []Measure
	for _, m := range h.Measures {
		if m.Pairing == wallTime {
			continue
		}
		if m.Pairing == perSubAgent || m.Moved || len(m.Tofu) < 2 || m.Better == BetterNeither {
			rest = append(rest, m)
			continue
		}
		shown = append(shown, m)
		before, after := m.Tofu[0], m.Latest()
		name := m.Short + " (" + m.Unit + ")"
		if m.Pairing != "" {
			name = m.Short + ", " + m.Pairing
		}
		verdict := Level
		switch ratio := m.Ratio(before, after); {
		case strings.HasSuffix(ratio, "better"):
			verdict = Ahead
		case strings.HasSuffix(ratio, "worse"):
			verdict = Behind
		}
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s<span class=\"detail\">%s</span></td><td class=\"table-cell\">%s</td>%s%s<td class=\"table-cell\">%s</td></tr>\n",
			html.EscapeString(name), html.EscapeString("bench/"+m.Bench), html.EscapeString(before.Version+" to "+after.Version),
			numberCell(cellNumber(before), ""), numberCell(cellNumber(after), ""), pillHTML(verdict, m.Words(after, before)))
	}
	if len(shown) == 0 {
		return ""
	}
	return tableOf("", []string{"Measure", "tofu version", "Before", "After", "Change"}, rows.String()) +
		foldHTML(changesHTML(append(shown, rest...)), shown)
}

func summarySections(h History) []section {
	all := []section{
		{"sonnet", "Sonnet 5.5: tofu, Claude Code and omp on one task", pairingHTML(h, "Sonnet 5.5")},
		{"gpt", "GPT 5.6 sol: tofu and codex on one task", pairingHTML(h, "GPT 5.6 sol")},
		{"per-agent", "Cost of sub-agents", scalingHTML(h)},
		{"wall", "Where a sub-agent run's time goes, tofu 0.5.14+dev, live", wallHTML(h)},
		{"versions", "Changes by version", versionsHTML(h)},
	}
	return slices.DeleteFunc(all, func(one section) bool { return one.body == "" })
}
