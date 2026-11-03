package report

import (
	"fmt"
	"html"
	"math"
	"slices"
	"strconv"
	"strings"
)

const wallTime = "Sub-agent wall time"

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

func rivalsOf(h History) []string {
	var rivals []string
	for _, table := range h.Tables {
		if !slices.Contains(rivals, table.Rival) {
			rivals = append(rivals, table.Rival)
		}
	}
	return rivals
}

func tldrHTML(h History) string {
	body := &strings.Builder{}
	body.WriteString("<ul class=\"tldr\">\n")
	for _, rival := range rivalsOf(h) {
		said := map[Verdict][]string{}
		for _, table := range h.Tables {
			if table.Rival != rival {
				continue
			}
			for _, m := range h.Rows(table) {
				verdict := m.Against(m.Rivals[0])
				if !slices.Contains(said[verdict], m.Short) {
					said[verdict] = append(said[verdict], m.Short)
				}
			}
		}
		fmt.Fprintf(body, "<li><strong>against %s</strong>", html.EscapeString(rival))
		for _, part := range []struct {
			verdict Verdict
			word    string
		}{{Ahead, "wins on"}, {Behind, "loses on"}} {
			fmt.Fprintf(body, "<div>%s %s</div>", pillHTML(part.verdict, fmt.Sprintf("%s %d", part.word, len(said[part.verdict]))),
				html.EscapeString(strings.Join(said[part.verdict], ", ")))
		}
		fmt.Fprintf(body, "<div>%s</div></li>\n", pillHTML(Level, fmt.Sprintf("level on %d", len(said[Level]))))
	}
	body.WriteString("</ul>\n")
	return body.String()
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

func foldHTML(body string) string {
	return "<details><summary>Runs and ranges</summary>\n" + body + "</details>\n"
}

func tableHTML(h History, table Table) string {
	measures := h.Rows(table)
	rows := &strings.Builder{}
	for _, m := range measures {
		ours, theirs := m.Latest(), m.Rivals[0]
		best := bestOf(m, []Run{ours, theirs})
		verdict := m.Against(theirs)
		said := m.Words(ours, theirs)
		if verdict == Level && said != "same" {
			said += ", within run spread"
		}
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s</td>%s%s<td class=\"table-cell\">%s</td></tr>\n",
			html.EscapeString(m.Short), numberCell(cellNumber(ours), "tofu "+bestClass(ours.Value == best)),
			numberCell(cellNumber(theirs), bestClass(theirs.Value == best)), pillHTML(verdict, said))
	}
	return fmt.Sprintf("<h3>%s</h3>\n", html.EscapeString(table.Title)) +
		tableOf("", []string{"Median", "tofu", html.EscapeString(table.Rival), "tofu is"}, rows.String()) +
		fmt.Sprintf("<div class=\"tag\">%s</div>\n", html.EscapeString(table.Tag)) + foldHTML(rangesHTML(table.Rival, measures))
}

func wallHTML(h History) string {
	var measures []Measure
	for _, m := range h.Measures {
		if m.Pairing == wallTime {
			measures = append(measures, m)
		}
	}
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
	return tableOf("", heads, rows.String()) + foldHTML(changesHTML(measures))
}

func versionsHTML(h History) string {
	rows := &strings.Builder{}
	var shown []Measure
	for _, m := range h.Measures {
		if m.Pairing != "" || m.Moved || len(m.Tofu) < 2 || m.Better == BetterNeither {
			continue
		}
		shown = append(shown, m)
		before, after := m.Tofu[0], m.Latest()
		verdict := Level
		switch ratio := m.Ratio(before, after); {
		case strings.HasSuffix(ratio, "better"):
			verdict = Ahead
		case strings.HasSuffix(ratio, "worse"):
			verdict = Behind
		}
		fmt.Fprintf(rows, "<tr class=\"table-row\"><td class=\"table-cell\">%s<span class=\"detail\">%s</span></td>%s%s<td class=\"table-cell\">%s</td></tr>\n",
			html.EscapeString(m.Short+" ("+m.Unit+")"), html.EscapeString("bench/"+m.Bench),
			numberCell(cellNumber(before), ""), numberCell(cellNumber(after), ""), pillHTML(verdict, m.Words(after, before)))
	}
	return tableOf("", []string{"Measure", "Before", "After", "Change"}, rows.String()) + foldHTML(changesHTML(shown))
}

func summarySections(h History) []section {
	var all []section
	for _, rival := range rivalsOf(h) {
		body := &strings.Builder{}
		for _, table := range h.Tables {
			if table.Rival == rival {
				body.WriteString(tableHTML(h, table))
			}
		}
		all = append(all, section{strings.ToLower(strings.ReplaceAll(rival, " ", "-")), "tofu against " + rival, body.String()})
	}
	all = append(all,
		section{"versions", "tofu against its own last build", versionsHTML(h)},
		section{"wall", "Where a sub-agent run's time goes", wallHTML(h)})
	return slices.DeleteFunc(all, func(one section) bool { return one.body == "" })
}
