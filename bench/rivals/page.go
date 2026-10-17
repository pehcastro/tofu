package main

import (
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
)

const pageStyle = `
:root{--bg:#fafaf9;--panel:#fff;--ink:#141517;--soft:#3d4148;--muted:#6f747c;--line:#e6e6e3;--track:#f0f0ed;
--tofu:#0e9f77;--rival:#9aa0a8;--good:#0b7a5b;--good-tint:#e2f4ed;--bad:#b3261e;--bad-tint:#fbe9e7;color-scheme:light dark}
@media (prefers-color-scheme:dark){:root{--bg:#0e0f11;--panel:#16181b;--ink:#eef0f2;--soft:#c3c7cd;--muted:#8d929a;--line:#272a2f;--track:#1f2226;
--tofu:#33d1a2;--rival:#6d737c;--good:#5fe0b6;--good-tint:#10322a;--bad:#ff8a80;--bad-tint:#3a1a19}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;-webkit-font-smoothing:antialiased}
main{max-width:1120px;margin:0 auto;padding:48px 24px 80px}
.kicker{color:var(--muted);font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.08em;margin:0 0 6px}
h1{font-size:34px;line-height:1.15;letter-spacing:-.02em;margin:0 0 10px}
.lede{color:var(--soft);font-size:16px;max-width:720px;margin:0}
section{margin-top:64px}
h2{font-size:22px;letter-spacing:-.01em;margin:0 0 4px}
.sub{color:var(--muted);margin:0 0 20px}
h3{font-size:15px;margin:0 0 12px}
section>h3{margin:32px 0 10px}
p{margin:0 0 10px}
code{font:12.5px ui-monospace,SFMono-Regular,Consolas,monospace;background:var(--track);padding:1px 5px;border-radius:5px}
.n{white-space:nowrap;font-variant-numeric:tabular-nums}
.foot{color:var(--muted);font-size:12.5px;margin-top:12px}
.grid3{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}
.panel{background:var(--panel);border:1px solid var(--line);border-radius:16px;padding:22px}
.hero{margin-top:36px}
.hero .panel{display:flex;flex-direction:column;gap:14px}
.headline{font-size:16px;color:var(--soft);line-height:1.3}
.headline b{display:block;font-size:44px;line-height:1;letter-spacing:-.03em;color:var(--tofu);margin-bottom:6px}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:12px 0;border-top:1px solid var(--line);border-bottom:1px solid var(--line)}
.pair span{display:block;color:var(--muted);font-size:12px}
.pair strong{font-size:20px}
.pair .mine strong{color:var(--tofu)}
.record{font-size:13px;color:var(--soft)}
.summary{margin:18px 0 0;font-size:13px;color:var(--soft);line-height:1.9}
.summary .n{white-space:normal}
.w{color:var(--good);font-weight:700}.l{color:var(--bad);font-weight:700}
.lo{display:block;color:var(--muted);font-size:11.5px;white-space:nowrap;font-weight:400}
.measure{display:grid;grid-template-columns:200px minmax(0,1fr);gap:20px;padding:18px 0;border-top:1px solid var(--line)}
.measure h3{margin:0}
.groups{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(300px,100%),1fr));gap:12px 28px}
.model{color:var(--muted);font-size:11.5px;font-weight:600;text-transform:uppercase;letter-spacing:.05em;margin-bottom:2px}
.val{line-height:1.25}
.val .n{font-size:13px;font-weight:600}
.scroll{overflow-x:auto;background:var(--panel);border:1px solid var(--line);border-radius:16px}
table{border-collapse:collapse;width:100%;font-size:14px}
th,td{padding:11px 14px;text-align:left;border-bottom:1px solid var(--line);vertical-align:top}
thead th{color:var(--muted);font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.04em;white-space:nowrap}
tr:last-child td{border-bottom:0}
td.v{text-align:right}
th.v{text-align:right}
.dir{display:block;color:var(--muted);font-size:11.5px;font-weight:400}
.tag{font-size:11px;font-weight:700;padding:2px 8px;border-radius:999px;white-space:nowrap;background:var(--track);color:var(--soft)}
.tag.bad{background:var(--bad-tint);color:var(--bad)}
.tag.good{background:var(--good-tint);color:var(--good)}
.row{display:grid;grid-template-columns:92px minmax(0,1fr);gap:10px;align-items:center;margin:6px 0;min-height:30px}
.row .who{font-size:13px;color:var(--soft);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.row.mine .who{color:var(--ink);font-weight:600}
.bar{display:flex;align-items:center;gap:8px;min-width:0}
.fill{height:18px;border-radius:4px;background:var(--rival);flex:none}
.row.mine .fill{background:var(--tofu)}
.up{color:var(--bad)}.down{color:var(--good)}
details{background:var(--panel);border:1px solid var(--line);border-radius:16px;padding:16px 22px;margin:12px 0}
summary{cursor:pointer;font-weight:600}
details h2{font-size:18px;margin:28px 0 10px}details h3{font-size:15px;margin:22px 0 8px}
details .scroll{border-radius:10px;margin-bottom:8px}
details table{font-size:12.5px}details th,details td{padding:7px 10px}
details thead th{text-transform:none;letter-spacing:0}
@media (max-width:860px){.grid3,.measure{grid-template-columns:minmax(0,1fr)}.measure{gap:10px}main{padding:28px 14px 56px}h1{font-size:26px}.headline b{font-size:38px}}
`

func renderPage(rep report, appendix string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>%s</title><style>%s</style></head><body><main>\n", html.EscapeString(rep.title()), pageStyle)
	fmt.Fprintf(&b, "<header><p class=\"kicker\">bench rivals · batch %s · %s</p><h1>%s</h1><p class=\"lede\">%s</p></header>\n", html.EscapeString(rep.batch), html.EscapeString(rep.days()), html.EscapeString(rep.title()), inline(rep.scope()))
	hero(&b, rep)
	b.WriteString("<p class=\"summary\">")
	for i, p := range rep.matchups {
		if i > 0 {
			b.WriteString(" · ")
		}
		fmt.Fprintf(&b, "<span class=\"n\">against %s on %s: <span class=\"w\">%s</span>, %s, <span class=\"l\">%s</span></span>", toolName(p.rival.Tool), html.EscapeString(p.model), counted(p.count(won), "win", "wins"), counted(p.count(tied), "tie", "ties"), counted(p.count(lost), "loss", "losses"))
	}
	b.WriteString("</p>\n")
	measureRows(&b, rep)
	changes(&b, rep)
	fresh, method := splitSection(rep.method, "## Fresh input, by source")
	losses(&b, rep, fresh)
	loadSection(&b, rep)
	b.WriteString("<section><h2>Method and raw data</h2><p class=\"sub\">Everything below is folded and printed from the run files as they are.</p>\n")
	folded(&b, "Every verdict, with spreads", verdictTable(rep, won)+verdictTable(rep, tied)+verdictTable(rep, lost))
	folded(&b, "Method, parity and conditions", method+conditionsTable(rep)+setAsideTable(rep))
	folded(&b, "Appendix: every run, every call, other tasks", appendix)
	b.WriteString("</section></main></body></html>\n")
	return b.String()
}

func folded(b *strings.Builder, summary, markdown string) {
	fmt.Fprintf(b, "<details><summary>%s</summary>%s</details>\n", html.EscapeString(summary), markdownHTML(markdown))
}

func withUnit(m measure, v float64) string {
	return strings.TrimSpace(m.figure(v) + " " + strings.TrimPrefix(m.unit, "points"))
}

func rangeTitle(m measure, s stat) string {
	return html.EscapeString(fmt.Sprintf("median %s, lowest %s, highest %s, from %s", m.figure(s.mid), m.figure(s.low), m.figure(s.high), strings.Join(s.files, ", ")))
}

func (p matchup) lead() duel {
	if wall := p.wall(); wall.result == won {
		return wall
	}
	if best := p.biggestWins(1); len(best) > 0 {
		return best[0]
	}
	return p.wall()
}

func hero(b *strings.Builder, rep report) {
	b.WriteString("<div class=\"grid3 hero\">\n")
	for _, p := range rep.matchups {
		d := p.lead()
		rival := toolName(p.rival.Tool)
		what := d.m.name + " than " + rival
		big, word, _ := strings.Cut(d.howMuch(), " ")
		if d.m.name == wallTime {
			what = "than " + rival
		}
		fmt.Fprintf(b, "<div class=\"panel\"><p class=\"kicker\">%s</p><div class=\"headline\"><b class=\"n\">%s %s</b>%s</div>", html.EscapeString(p.model), big, word, html.EscapeString(what))
		fmt.Fprintf(b, "<div class=\"pair\"><div class=\"mine\" title=\"%s\"><span>tofu, %s</span><strong class=\"n\">%s</strong></div><div title=\"%s\"><span>%s, %s</span><strong class=\"n\">%s</strong></div></div>",
			rangeTitle(d.m, d.tofu), d.m.name, withUnit(d.m, d.tofu.mid), rangeTitle(d.m, d.rival), rival, d.m.name, withUnit(d.m, d.rival.mid))
		if wall := p.wall(); d.m.name != wallTime {
			fmt.Fprintf(b, "<p class=\"record\">Wall time is a %s: <span class=\"n\">%s</span> against <span class=\"n\">%s</span>.</p>", wall.result, withUnit(wall.m, wall.tofu.mid), withUnit(wall.m, wall.rival.mid))
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</div>\n")
}

func models(rep report) []string {
	var out []string
	for _, a := range rep.present {
		if model := rep.now[a.Name][0].Model; !slices.Contains(out, model) {
			out = append(out, model)
		}
	}
	return out
}

func armsOn(rep report, model string) []arm {
	return slices.DeleteFunc(slices.Clone(rep.present), func(a arm) bool { return rep.now[a.Name][0].Model != model })
}

func measureRows(b *strings.Builder, rep report) {
	b.WriteString("<section><h2>Every measure, every arm</h2><p class=\"sub\">One row per measure, every arm on the same scale, grouped by model. The value is the median of three runs and the range under it is lowest to highest. <span class=\"tag good\">best</span> marks the winner of a model group: best median, and no other arm's range reaches it.</p>\n")
	for _, m := range measures {
		scale := 0.0
		for _, a := range rep.present {
			if s, ok := statOf(rep.now[a.Name], m); ok {
				scale = max(scale, s.mid)
			}
		}
		direction := "lower is better"
		if m.higherWins {
			direction = "higher is better"
		}
		fmt.Fprintf(b, "<div class=\"measure\"><div class=\"what\"><h3>%s</h3><span class=\"dir\">%s</span></div><div class=\"groups\">", html.EscapeString(m.name), direction)
		for _, model := range models(rep) {
			on := armsOn(rep, model)
			stats := make([]stat, len(on))
			recorded := make([]bool, len(on))
			for i, a := range on {
				stats[i], recorded[i] = statOf(rep.now[a.Name], m)
			}
			winner := rowWinner(m, stats, recorded)
			fmt.Fprintf(b, "<div class=\"group\"><div class=\"model\">%s</div>", html.EscapeString(model))
			for i, a := range on {
				class := "row"
				if a.Tool == "tofu" {
					class += " mine"
				}
				if !recorded[i] {
					fmt.Fprintf(b, "<div class=\"%s\"><span class=\"who\">%s</span><span class=\"lo\">not recorded</span></div>", class, html.EscapeString(toolName(a.Tool)))
					continue
				}
				best, spread := "", ""
				if i == winner {
					best = " <span class=\"tag good\">best</span>"
				}
				if stats[i].low != stats[i].high {
					spread = fmt.Sprintf("<span class=\"lo\">%s to %s</span>", m.figure(stats[i].low), m.figure(stats[i].high))
				}
				fmt.Fprintf(b, "<div class=\"%s\" title=\"%s\"><span class=\"who\">%s</span><div class=\"bar\"><div class=\"fill\" style=\"width:%.1f%%\"></div><span class=\"val\"><span class=\"n\">%s</span>%s%s</span></div></div>",
					class, rangeTitle(m, stats[i]), html.EscapeString(toolName(a.Tool)), share(stats[i].mid, scale), withUnit(m, stats[i].mid), best, spread)
			}
			b.WriteString("</div>")
		}
		b.WriteString("</div></div>\n")
	}
	b.WriteString("</section>\n")
}

func share(v, scale float64) float64 {
	if scale <= 0 {
		return 0
	}
	return max(v, 0) / scale * 62
}

func rowWinner(m measure, stats []stat, recorded []bool) int {
	better := func(x, y stat) bool {
		if m.higherWins {
			return x.mid > y.mid
		}
		return x.mid < y.mid
	}
	winner, count := -1, 0
	for i, s := range stats {
		if !recorded[i] {
			continue
		}
		count++
		if winner < 0 || better(s, stats[winner]) {
			winner = i
		}
	}
	if count < 2 {
		return -1
	}
	for i, s := range stats {
		if i != winner && recorded[i] && judge(m, stats[winner], s) != won {
			return -1
		}
	}
	return winner
}

func changes(b *strings.Builder, rep report) {
	shown := []string{wallTime, "total input", "peak memory", "CPU time", "model requests"}
	fmt.Fprintf(b, "<section><h2>Since %s</h2><p class=\"sub\">Median now, with the median on %s and the change under it. Green moved the better way.</p><div class=\"scroll\"><table><thead><tr><th>arm</th>", rep.since, rep.since)
	for _, name := range shown {
		fmt.Fprintf(b, "<th class=\"v\">%s</th>", name)
	}
	b.WriteString("</tr></thead><tbody>\n")
	for _, a := range rep.present {
		fmt.Fprintf(b, "<tr><td><strong>%s</strong><span class=\"dir\">%s</span></td>", html.EscapeString(toolName(a.Tool)), html.EscapeString(rep.now[a.Name][0].Model))
		for _, name := range shown {
			m := measures[slices.IndexFunc(measures, func(m measure) bool { return m.name == name })]
			after, _ := statOf(rep.now[a.Name], m)
			before, ok := statOf(rep.then[a.Name], m)
			note := "<span class=\"lo\">not recorded then</span>"
			if ok && before.mid != 0 {
				delta := (after.mid - before.mid) / before.mid * 100
				class := "down"
				if (delta > 0) != m.higherWins && delta != 0 {
					class = "up"
				}
				note = fmt.Sprintf("<span class=\"lo\">was %s, <span class=\"%s\">%+.0f%%</span></span>", m.figure(before.mid), class, delta)
			}
			fmt.Fprintf(b, "<td class=\"v\"><span class=\"n\">%s</span>%s</td>", withUnit(m, after.mid), note)
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div></section>\n")
}

func splitSection(markdown, heading string) (string, string) {
	before, after, found := strings.Cut(markdown, heading+"\n")
	if !found {
		return "", markdown
	}
	section, rest, _ := strings.Cut(after, "\n## ")
	if rest != "" {
		rest = "\n## " + rest
	}
	return heading + "\n" + section, before + rest
}

func dropHeading(markdown string) string {
	_, body, _ := strings.Cut(strings.TrimLeft(markdown, "\n"), "\n")
	return body
}

type barItem struct {
	who   string
	mine  bool
	value float64
	text  string
}

func barChart(b *strings.Builder, title, note string, items []barItem) {
	scale := 0.0
	for _, item := range items {
		scale = max(scale, item.value)
	}
	fmt.Fprintf(b, "<div class=\"panel\"><h3>%s</h3><p class=\"foot\">%s</p>", html.EscapeString(title), html.EscapeString(note))
	for _, item := range items {
		class := "row"
		if item.mine {
			class += " mine"
		}
		fmt.Fprintf(b, "<div class=\"%s\"><span class=\"who\">%s</span><div class=\"bar\"><div class=\"fill\" style=\"width:%.1f%%\"></div><span class=\"val\"><span class=\"n\">%s</span></span></div></div>", class, html.EscapeString(item.who), share(item.value, scale), html.EscapeString(item.text))
	}
	b.WriteString("</div>\n")
}

func loadSection(b *strings.Builder, rep report) {
	l := rep.load
	fmt.Fprintf(b, "<section><h2>Load: many sub-agents on one machine</h2><p class=\"sub\">From earlier benches on older builds: <code>%s</code> for every tool, <code>%s</code> for tofu alone in one session holding hundreds of sub-agents.</p><div class=\"grid3\">\n", ladderFile, crowdFile)
	var slopes, tops, cores []barItem
	for _, one := range l.ladders {
		top := one.top()
		who := toolName(one.tool)
		slopes = append(slopes, barItem{who, one.tool == "tofu", one.perAgent, fmt.Sprintf("%.1f MiB", one.perAgent)})
		tops = append(tops, barItem{who, one.tool == "tofu", top.PeakRSSMB, fmt.Sprintf("%.0f MiB", top.PeakRSSMB)})
		cores = append(cores, barItem{who, one.tool == "tofu", top.CPUS / top.WallS, fmt.Sprintf("%.2f cores", top.CPUS/top.WallS)})
	}
	top := l.ladders[0].top().Agents
	barChart(b, "Memory per extra sub-agent", "Slope of peak RSS over every rung. Shorter is better.", slopes)
	barChart(b, fmt.Sprintf("Peak memory at %d sub-agents", top), "Whole process tree. Shorter is better.", tops)
	barChart(b, fmt.Sprintf("CPU use at %d sub-agents", top), "CPU seconds over wall seconds. Shorter is better.", cores)
	b.WriteString("</div>\n" + markdownHTML(dropHeading(loadMarkdown(rep))) + "</section>\n")
}

func losses(b *strings.Builder, rep report, fresh string) {
	b.WriteString("<section><h2>Where tofu loses</h2><p class=\"sub\">Decided losses only: the two ranges do not meet.</p><div class=\"scroll\"><table><thead><tr><th>measure</th><th>against</th><th class=\"v\">tofu</th><th class=\"v\">rival</th><th class=\"v\">tofu is</th></tr></thead><tbody>\n")
	for _, p := range rep.matchups {
		for _, d := range p.duels {
			if d.result != lost {
				continue
			}
			fmt.Fprintf(b, "<tr><td>%s</td><td>%s<span class=\"dir\">%s</span></td><td class=\"v\" title=\"%s\"><span class=\"n\">%s</span></td><td class=\"v\" title=\"%s\"><span class=\"n\">%s</span></td><td class=\"v\"><span class=\"tag bad\">%s</span></td></tr>\n",
				html.EscapeString(d.m.name), toolName(p.rival.Tool), html.EscapeString(p.model), rangeTitle(d.m, d.tofu), withUnit(d.m, d.tofu.mid), rangeTitle(d.m, d.rival), withUnit(d.m, d.rival.mid), html.EscapeString(d.howMuch()))
		}
	}
	b.WriteString("</tbody></table></div>\n" + markdownHTML(strings.Replace(fresh, "## ", "### ", 1)) + markdownHTML(strings.Replace(sideCostTable(rep), "## ", "### ", 1)) + "</section>\n")
}

var (
	codeSpan = regexp.MustCompile("`([^`]+)`")
	boldSpan = regexp.MustCompile(`\*\*([^*]+)\*\*`)
)

func inline(text string) string {
	escaped := html.EscapeString(strings.TrimSpace(text))
	return boldSpan.ReplaceAllString(codeSpan.ReplaceAllString(escaped, "<code>$1</code>"), "<strong>$1</strong>")
}

func cells(line string) []string {
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "| "), " |"), " | ")
}

func markdownHTML(markdown string) string {
	var b strings.Builder
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "### "):
			b.WriteString("<h3>" + inline(line[4:]) + "</h3>\n")
		case strings.HasPrefix(line, "## "):
			b.WriteString("<h2>" + inline(line[3:]) + "</h2>\n")
		case strings.HasPrefix(line, "- "):
			b.WriteString("<p>" + inline(line[2:]) + "</p>\n")
		case strings.HasPrefix(line, "|"):
			b.WriteString("<div class=\"scroll\"><table><thead><tr>")
			for _, head := range cells(line) {
				b.WriteString("<th>" + inline(head) + "</th>")
			}
			b.WriteString("</tr></thead><tbody>\n")
			for i += 2; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
				b.WriteString("<tr>")
				for _, cell := range cells(lines[i]) {
					b.WriteString("<td>" + inline(cell) + "</td>")
				}
				b.WriteString("</tr>\n")
			}
			i--
			b.WriteString("</tbody></table></div>\n")
		case strings.TrimSpace(line) != "":
			b.WriteString("<p>" + inline(line) + "</p>\n")
		}
	}
	return b.String()
}
