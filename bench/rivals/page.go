package main

import (
	"cmp"
	"fmt"
	"html"
	"math"
	"regexp"
	"slices"
	"strings"
)

const pageStyle = `
:root{--bg:#f7f7f5;--panel:#fff;--ink:#17181a;--muted:#6b6f76;--line:#e4e4e0;--track:#efefeb;
--tofu:#0f9d76;--rival:#8a8f98;--then:#c9ccd1;--win:#e3f5ec;--win-ink:#0b7a5b;--loss:#fbe7e6;--loss-ink:#b3261e;--tie:#eef0f3;--tie-ink:#4a5059;
--s1:#0f9d76;--s2:#6cc9a9;--s3:#f2a65a;--s4:#9aa3ff;--s5:#c9ccd1;color-scheme:light dark}
@media (prefers-color-scheme:dark){:root{--bg:#0f1012;--panel:#17191c;--ink:#ecedee;--muted:#9a9ea6;--line:#2a2d32;--track:#22252a;
--tofu:#34d3a4;--rival:#7d838c;--then:#3b3f46;--win:#12342a;--win-ink:#5fe0b6;--loss:#3a1a19;--loss-ink:#ff8a80;--tie:#23262b;--tie-ink:#b8bcc3;
--s1:#34d3a4;--s2:#1f8a6a;--s3:#d98c3f;--s4:#7c86f0;--s5:#4a4f57}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif}
main{max-width:1080px;margin:0 auto;padding:32px 20px 64px}
h1{font-size:28px;line-height:1.2;margin:4px 0 8px;letter-spacing:-.01em}
h2{font-size:19px;margin:40px 0 14px}
h3{font-size:15px;margin:0 0 6px}
p{margin:0 0 10px}
code{font:12.5px ui-monospace,SFMono-Regular,Consolas,monospace;background:var(--track);padding:1px 5px;border-radius:5px}
.eyebrow{color:var(--muted);font-size:13px;text-transform:uppercase;letter-spacing:.06em}
.lede{color:var(--muted);max-width:760px}
.cards{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:12px}
.card,.panel{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:16px}
.big{font-size:38px;font-weight:700;line-height:1;color:var(--tofu);letter-spacing:-.02em}
.word{font-weight:600;margin:4px 0 8px}
.small{color:var(--muted);font-size:13px}
.panels{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(320px,100%),1fr));gap:14px}
.wtl{display:flex;height:26px;border-radius:8px;overflow:hidden;margin:10px 0 14px;font-size:12px;font-weight:600}
.wtl span{display:flex;align-items:center;justify-content:center;white-space:nowrap;overflow:hidden}
.w{background:var(--win);color:var(--win-ink)}.t{background:var(--tie);color:var(--tie-ink)}.l{background:var(--loss);color:var(--loss-ink)}
.duel{padding:8px 0;border-top:1px solid var(--line)}
.duel-head{display:flex;justify-content:space-between;gap:8px;font-size:13px;margin-bottom:4px}
.pill{font-size:11px;font-weight:700;padding:1px 8px;border-radius:999px;white-space:nowrap}
.bar{display:grid;grid-template-columns:70px 1fr 92px;gap:8px;align-items:center;font-size:12px;margin:2px 0}
.bar .num{text-align:right;font-variant-numeric:tabular-nums}
.who{color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.track{position:relative;height:10px;background:var(--track);border-radius:5px}
.fill{position:absolute;left:0;top:0;bottom:0;border-radius:5px}
.range{position:absolute;top:4px;height:2px;background:var(--ink);opacity:.45}
.tofu{background:var(--tofu)}.rival{background:var(--rival)}.then{background:var(--then)}
.stack{display:flex;height:16px;border-radius:5px;overflow:hidden;background:var(--track)}
.legend{display:flex;flex-wrap:wrap;gap:14px;font-size:12px;color:var(--muted);margin:8px 0 12px}
.legend i{display:inline-block;width:10px;height:10px;border-radius:3px;margin-right:5px;vertical-align:-1px}
.scroll{overflow-x:auto;background:var(--panel);border:1px solid var(--line);border-radius:14px;margin:0 0 14px}
table{border-collapse:collapse;width:100%;font-size:13px;font-variant-numeric:tabular-nums}
th,td{padding:8px 10px;text-align:left;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--muted);font-weight:600;white-space:nowrap}
tr:last-child td{border-bottom:0}
details{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:12px 16px;margin:12px 0}
details[open]{padding-bottom:16px}
summary{cursor:pointer;font-weight:600}
details .scroll{background:var(--bg)}
@media (max-width:560px){main{padding:20px 12px 48px}h1{font-size:23px}.big{font-size:32px}.bar{grid-template-columns:56px 1fr 78px}}
`

func renderPage(rep report, appendix string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>%s</title><style>%s</style></head><body><main>\n", html.EscapeString(rep.title()), pageStyle)
	fmt.Fprintf(&b, "<header><p class=\"eyebrow\">bench rivals · batch %s</p><h1>%s</h1><p class=\"lede\">%s</p></header>\n", html.EscapeString(rep.batch), html.EscapeString(rep.title()), inline(rep.scope()))
	winCards(&b, rep)
	scoreboard(&b, rep)
	changeCharts(&b, rep)
	wallChart(&b, rep)
	b.WriteString(markdownHTML(verdictTable(rep, lost)) + markdownHTML(verdictTable(rep, tied)))
	folded(&b, "Every win as a table", verdictTable(rep, won))
	folded(&b, "Medians per arm, with spread", mediansTable(rep))
	folded(&b, "Method, parity and conditions", rep.method+conditionsTable(rep)+setAsideTable(rep))
	folded(&b, "Appendix: every run, every call, other tasks", appendix)
	b.WriteString("</main></body></html>\n")
	return b.String()
}

func folded(b *strings.Builder, summary, markdown string) {
	fmt.Fprintf(b, "<details><summary>%s</summary>%s</details>\n", html.EscapeString(summary), markdownHTML(markdown))
}

func winCards(b *strings.Builder, rep report) {
	type win struct {
		p matchup
		d duel
	}
	var wins []win
	for _, p := range rep.matchups {
		for _, d := range p.duels {
			if d.result == won {
				wins = append(wins, win{p, d})
			}
		}
	}
	order := func(w win) float64 {
		r, _ := w.d.ratio()
		if w.d.m.name == wallTime {
			return math.Inf(1)
		}
		return r
	}
	slices.SortStableFunc(wins, func(x, y win) int { return cmp.Compare(order(y), order(x)) })
	b.WriteString("<h2>Where tofu wins</h2><div class=\"cards\">\n")
	for _, w := range wins {
		big, word, _ := strings.Cut(w.d.howMuch(), " ")
		unit := strings.TrimSpace(" " + w.d.m.unit)
		fmt.Fprintf(b, "<div class=\"card\" title=\"%s\"><div class=\"big\">%s</div><div class=\"word\">%s</div><div>%s</div><div class=\"small\">tofu %s %s, %s %s %s</div><div class=\"small\">%s against %s</div></div>\n",
			html.EscapeString(strings.Join(append(w.d.tofu.files, w.d.rival.files...), "\n")), big, word, html.EscapeString(w.d.m.name),
			w.d.m.figure(w.d.tofu.mid), unit, toolName(w.p.rival.Tool), w.d.m.figure(w.d.rival.mid), unit, html.EscapeString(w.p.model), toolName(w.p.rival.Tool))
	}
	b.WriteString("</div>\n")
}

func scoreboard(b *strings.Builder, rep report) {
	b.WriteString("<h2>Scoreboard</h2><p class=\"small\">Bars are medians; the thin line is the lowest to highest run.</p><div class=\"panels\">\n")
	for _, p := range rep.matchups {
		fmt.Fprintf(b, "<div class=\"panel\"><h3>%s</h3><div class=\"wtl\">", html.EscapeString(p.title()))
		for _, o := range []outcome{won, tied, lost} {
			if n := p.count(o); n > 0 {
				fmt.Fprintf(b, "<span class=\"%s\" style=\"flex:%d\">%d %s</span>", pillClass(o), n, n, map[outcome]string{won: "wins", tied: "ties", lost: "losses"}[o])
			}
		}
		b.WriteString("</div>\n")
		for _, d := range p.duels {
			scale := max(d.tofu.high, d.rival.high)
			fmt.Fprintf(b, "<div class=\"duel\"><div class=\"duel-head\"><span>%s</span><span class=\"pill %s\">%s</span></div>", html.EscapeString(d.m.label()), pillClass(d.result), html.EscapeString(d.howMuch()))
			bar(b, "tofu", "tofu", d.m, d.tofu, scale)
			bar(b, toolName(p.rival.Tool), "rival", d.m, d.rival, scale)
			b.WriteString("</div>\n")
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</div>\n")
}

func pillClass(o outcome) string {
	switch o {
	case won:
		return "w"
	case lost:
		return "l"
	case tied, unrecorded:
		return "t"
	}
	panic("unknown outcome " + o)
}

func share(v, scale float64) float64 {
	if scale <= 0 {
		return 0
	}
	return max(v, 0) / scale * 100
}

func bar(b *strings.Builder, who, class string, m measure, s stat, scale float64) {
	fmt.Fprintf(b, "<div class=\"bar\" title=\"%s\"><span class=\"who\">%s</span><div class=\"track\"><div class=\"fill %s\" style=\"width:%.1f%%\"></div><div class=\"range\" style=\"left:%.1f%%;width:%.1f%%\"></div></div><span class=\"num\">%s</span></div>",
		html.EscapeString(strings.Join(s.files, "\n")), html.EscapeString(who), class, share(s.mid, scale), share(s.low, scale), share(s.high-s.low, scale), m.figure(s.mid))
}

func changeCharts(b *strings.Builder, rep report) {
	fmt.Fprintf(b, "<h2>Change since %s</h2><div class=\"legend\"><span><i class=\"then\"></i>%s</span><span><i class=\"tofu\"></i>now, tofu</span><span><i class=\"rival\"></i>now, rival</span></div><div class=\"panels\">\n", rep.since, rep.since)
	for _, name := range []string{wallTime, "total input", "output", "model requests", "peak memory", "CPU time"} {
		m := measures[slices.IndexFunc(measures, func(m measure) bool { return m.name == name })]
		scale := 0.0
		for _, a := range rep.present {
			before, _ := statOf(rep.then[a.Name], m)
			after, _ := statOf(rep.now[a.Name], m)
			scale = max(scale, before.mid, after.mid)
		}
		fmt.Fprintf(b, "<div class=\"panel\"><h3>%s</h3>", html.EscapeString(m.label()))
		for _, a := range rep.present {
			before, okBefore := statOf(rep.then[a.Name], m)
			after, _ := statOf(rep.now[a.Name], m)
			class := "rival"
			if a.Tool == "tofu" {
				class = "tofu"
			}
			fmt.Fprintf(b, "<div class=\"duel\"><div class=\"duel-head\"><span>%s</span><span class=\"small\">%s</span></div>", a.Name, html.EscapeString(change(m, rep.then[a.Name], rep.now[a.Name])))
			if okBefore {
				fmt.Fprintf(b, "<div class=\"bar\"><span class=\"who\">%s</span><div class=\"track\"><div class=\"fill then\" style=\"width:%.1f%%\"></div></div><span class=\"num\">%s</span></div>", rep.since[5:], share(before.mid, scale), m.figure(before.mid))
			}
			fmt.Fprintf(b, "<div class=\"bar\"><span class=\"who\">now</span><div class=\"track\"><div class=\"fill %s\" style=\"width:%.1f%%\"></div></div><span class=\"num\">%s</span></div></div>", class, share(after.mid, scale), m.figure(after.mid))
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</div>\n")
}

func wallChart(b *strings.Builder, rep report) {
	parts := []struct{ name, color string }{{"to first token", "--s1"}, {"after first token", "--s2"}, {"tools", "--s3"}, {"own work", "--s4"}, {"outside", "--s5"}}
	b.WriteString("<h2>Where the wall time goes</h2><div class=\"legend\">")
	for _, part := range parts {
		fmt.Fprintf(b, "<span><i style=\"background:var(%s)\"></i>%s</span>", part.color, part.name)
	}
	b.WriteString("</div><div class=\"panel\">\n")
	var splits []wallSplit
	scale := 0.0
	for _, a := range rep.present {
		for _, w := range wallSplits(rep.now[a.Name]) {
			splits = append(splits, w)
			scale = max(scale, seconds(w.r.WallMS), w.model+w.tools+max(w.own, 0)+w.outside)
		}
	}
	for _, w := range splits {
		fmt.Fprintf(b, "<div class=\"bar\" title=\"%s\"><span class=\"who\">%s %d</span><div class=\"stack\">", w.r.file(), w.r.Arm, w.r.Rep)
		for i, v := range []float64{w.firstToken, w.afterFirst, w.tools, w.own, w.outside} {
			fmt.Fprintf(b, "<span style=\"width:%.1f%%;background:var(%s)\" title=\"%s %.1f s\"></span>", share(v, scale), parts[i].color, parts[i].name, v)
		}
		fmt.Fprintf(b, "</div><span class=\"num\">%.1f s</span></div>\n", seconds(w.r.WallMS))
	}
	b.WriteString("<p class=\"small\">Claude Code and omp do not time their requests, so only tofu and codex have a split.</p></div>\n")
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
		case strings.HasPrefix(line, "# "):
			b.WriteString("<h1>" + inline(line[2:]) + "</h1>\n")
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
