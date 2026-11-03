package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	settingShipped     = "shipped"
	settingRival       = "rival"
	thinkingSummarized = "summarized"
	thinkingOmitted    = "omitted"
)

const wallTime = "wall time"

var kinds = []string{"read", "edit", "write", "search", "bash", "spawn", "other"}

type measure struct {
	name, unit, better, worse string
	digits                    int
	higherWins                bool
	value                     func(row) (float64, bool)
}

func always(value func(row) float64) func(row) (float64, bool) {
	return func(r row) (float64, bool) { return value(r), true }
}

const memorySummaries = "fresh input, tofu's memory summaries"

func newMeasures(summaries map[string]tokens) []measure {
	conversation := func(value func(r row, summary tokens) float64) func(row) (float64, bool) {
		return func(r row) (float64, bool) {
			if r.Tool != "tofu" {
				return value(r, tokens{}), true
			}
			summary, ok := summaries[r.file()]
			return value(r, summary), ok
		}
	}
	return []measure{
		{"quality", "points", "higher", "lower", 0, true, always(func(r row) float64 { got, _ := r.Score.quality(); return float64(got) })},
		{wallTime, "s", "faster", "slower", 1, false, always(func(r row) float64 { return seconds(r.WallMS) })},
		{"CPU time", "s", "less", "more", 1, false, always(func(r row) float64 { return seconds(r.Tree.CPUMS) })},
		{"CPU use", "cores", "less", "more", 2, false, always(func(r row) float64 { return float64(r.Tree.CPUMS) / float64(r.WallMS) })},
		{"peak memory", "MiB", "less", "more", 0, false, always(func(r row) float64 { return math.Round(megabytes(r.Tree.PeakTreeRSS)) })},
		{"peak reserved memory", "MiB", "less", "more", 0, false, always(func(r row) float64 { return math.Round(megabytes(r.Tree.PeakCommit)) })},
		{"processes started", "", "fewer", "more", 0, false, always(func(r row) float64 { return float64(r.Tree.Processes) })},
		{"model requests", "", "fewer", "more", 0, false, always(func(r row) float64 { return float64(r.Requests) })},
		{"total input, conversation", "tokens", "fewer", "more", 0, false, conversation(func(r row, s tokens) float64 { return float64(r.Tokens.in() - s.FreshIn) })},
		{"fresh input, conversation", "tokens", "fewer", "more", 0, false, conversation(func(r row, s tokens) float64 { return float64(r.Tokens.FreshIn - s.FreshIn) })},
		{"output, conversation", "tokens", "fewer", "more", 0, false, conversation(func(r row, s tokens) float64 { return float64(r.Tokens.Out - s.Out) })},
		{memorySummaries, "tokens", "fewer", "more", 0, false, func(r row) (float64, bool) {
			s, ok := summaries[r.file()]
			return float64(s.FreshIn), ok && r.Tool == "tofu"
		}},
		{"failed tool calls", "", "fewer", "more", 0, false, always(func(r row) float64 { return float64(failedCalls(r)) })},
		{"tool time", "s", "less", "more", 1, false, always(toolSeconds)},
	}
}

func summaryTokens(r row) (tokens, bool) {
	home := filepath.Join(r.Work, "home")
	if _, err := os.Stat(home); r.Tool != "tofu" || r.Work == "" || err != nil {
		return tokens{}, false
	}
	repo := filepath.Join(r.Work, "repo")
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "APPDATA="+filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"))
	sessions := list(decode(toolOutput(repo, env, "tofu", "session", "list", "--json"))["data"], "sessions")
	if len(sessions) == 0 {
		return tokens{}, false
	}
	var used tokens
	for _, m := range list(decode(toolOutput(repo, env, "tofu", "session", "trace", str(sessions[0], "id"), "--json"))["data"], "memory") {
		used.add(num(m, "usage", "input_tokens"), 0, 0, num(m, "usage", "output_tokens"))
	}
	return used, true
}

const (
	observationsHeading = "## Observations"
	everyModel          = "every model"
)

type observation struct {
	measure, model, text string
}

func readObservations(section string) []observation {
	var out []observation
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| measure ") {
			continue
		}
		if parts := cells(line); len(parts) == 3 {
			out = append(out, observation{parts[0], parts[1], parts[2]})
		}
	}
	return out
}

func (rep report) measure(name string) measure {
	return rep.measures[slices.IndexFunc(rep.measures, func(m measure) bool { return m.name == name })]
}

var perAgent = measure{"memory per sub-agent", "MiB", "less", "more", 0, false, func(r row) (float64, bool) {
	return math.Round(megabytes(r.Tree.PeakTreeRSS) / float64(r.Agents)), r.Agents > 0
}}

func toolSeconds(r row) float64 {
	var total int64
	for _, c := range r.Calls {
		total += max(c.MS, 0)
	}
	return seconds(total)
}

type stat struct {
	mid, low, high float64
	files          []string
}

func statOf(rows []row, m measure) (stat, bool) {
	var values []float64
	var files []string
	for _, r := range rows {
		if v, ok := m.value(r); ok {
			values, files = append(values, v), append(files, r.file())
		}
	}
	if len(values) == 0 {
		return stat{}, false
	}
	return stat{median(values), slices.Min(values), slices.Max(values), files}, true
}

type outcome string

const (
	won        outcome = "win"
	tied       outcome = "tie"
	lost       outcome = "loss"
	unrecorded outcome = "not recorded"
)

type duel struct {
	m           measure
	tofu, rival stat
	result      outcome
	before      outcome
}

func judge(m measure, tofu, rival stat) outcome {
	tofuLower := tofu.high < rival.low
	switch {
	case !tofuLower && tofu.low <= rival.high:
		return tied
	case tofuLower != m.higherWins:
		return won
	}
	return lost
}

func (d duel) tofuAhead() bool {
	return (d.tofu.mid < d.rival.mid) != d.m.higherWins
}

func (d duel) ratio() (float64, bool) {
	low, high := min(d.tofu.mid, d.rival.mid), max(d.tofu.mid, d.rival.mid)
	return high / low, low > 0
}

func (d duel) howMuch() string {
	if d.tofu.mid == d.rival.mid {
		return "level"
	}
	word := d.m.worse
	if d.tofuAhead() {
		word = d.m.better
	}
	if r, ok := d.ratio(); ok {
		return ratioText(r) + " " + word
	}
	return d.m.figure(math.Abs(d.tofu.mid-d.rival.mid)) + " " + word
}

func ratioText(r float64) string {
	if r >= 10 {
		return fmt.Sprintf("%.0fx", r)
	}
	return fmt.Sprintf("%.1fx", r)
}

func (d duel) difference() string {
	gap := d.tofu.mid - d.rival.mid
	sign := "+"
	if gap < 0 {
		sign = "-"
	}
	return strings.TrimSpace(sign + d.m.figure(math.Abs(gap)) + " " + d.m.unit)
}

func (m measure) figure(v float64) string {
	digits := m.digits
	if digits == 0 && v != math.Trunc(v) {
		digits = 1
	}
	text := strconv.FormatFloat(v, 'f', digits, 64)
	whole, fraction, _ := strings.Cut(text, ".")
	for i := len(whole) - 3; i > 0 && whole[i-1] != '-'; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if fraction != "" {
		whole += "." + fraction
	}
	return whole
}

func (m measure) spread(s stat) string {
	if s.low == s.high {
		return m.figure(s.mid)
	}
	return fmt.Sprintf("%s [%s to %s]", m.figure(s.mid), m.figure(s.low), m.figure(s.high))
}

func (m measure) label() string {
	if m.unit == "" || m.unit == "points" || m.unit == "tokens" {
		return m.name
	}
	return m.name + ", " + m.unit
}

type matchup struct {
	rival arm
	model string
	duels []duel
}

func (p matchup) title() string {
	return fmt.Sprintf("tofu against %s on %s", toolName(p.rival.Tool), p.model)
}

func (p matchup) count(o outcome) int {
	n := 0
	for _, d := range p.duels {
		if d.result == o {
			n++
		}
	}
	return n
}

func (p matchup) wall() duel {
	return p.duels[slices.IndexFunc(p.duels, func(d duel) bool { return d.m.name == wallTime })]
}

func (p matchup) biggestWins(n int) []duel {
	wins := slices.DeleteFunc(slices.Clone(p.duels), func(d duel) bool { _, ok := d.ratio(); return d.result != won || !ok || d.m.name == wallTime })
	slices.SortFunc(wins, func(x, y duel) int { a, _ := x.ratio(); b, _ := y.ratio(); return cmp.Compare(b, a) })
	return wins[:min(n, len(wins))]
}

func toolName(tool string) string {
	switch tool {
	case "claude":
		return "Claude Code"
	case "tofu", "omp", "codex":
		return tool
	}
	panic("unknown tool " + tool)
}

type report struct {
	batch, since string
	now, then    map[string][]row
	subagents    map[string][]row
	load         load
	measures     []measure
	observations []observation
	present      []arm
	matchups     []matchup
	setAside     []row
	method       string
}

func (r report) days() string {
	var dates []string
	for _, a := range r.present {
		for _, one := range r.now[a.Name] {
			dates = append(dates, one.Date)
		}
	}
	slices.Sort(dates)
	return dates[0] + " to " + dates[len(dates)-1][11:]
}

func (r report) runs() int {
	n := 0
	for _, rows := range r.now {
		n += len(rows)
	}
	return n
}

func writeReport(args []string) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	batch := flags.String("batch", "", "folder under runs whose rows make the report, every arm in it one row of each table")
	since := flags.String("since", "2026-10-13", "day of the rows directly under runs, with no setting, that the change columns compare against")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *batch == "" {
		return errors.New("report needs -batch")
	}
	now, err := readRows(filepath.Join(here(), "runs", *batch, "*.json"))
	if err != nil {
		return err
	}
	earlier, err := readRows(filepath.Join(here(), "runs", "*.json"))
	if err != nil {
		return err
	}
	earlier = slices.DeleteFunc(earlier, func(r row) bool { return r.Setting != "" || !strings.HasPrefix(r.Date, *since) })
	setAside, err := readRows(filepath.Join(here(), "runs", *batch, "discarded", "*.json"))
	if err != nil {
		return err
	}
	subagents, err := readRows(filepath.Join(here(), "runs", "subagents", "*.json"))
	if err != nil {
		return err
	}
	method, err := os.ReadFile(filepath.Join(here(), "judgement.md"))
	if err != nil {
		return err
	}
	machine, err := readLoad()
	if err != nil {
		return err
	}
	summaries := map[string]tokens{}
	for _, r := range append(slices.Clone(now), earlier...) {
		if used, ok := summaryTokens(r); ok {
			summaries[r.file()] = used
		}
	}
	notes, rest := splitSection(string(method), observationsHeading)
	rep := report{batch: *batch, since: *since, now: byArm(now), then: byArm(earlier), subagents: byArm(subagents), load: machine, measures: newMeasures(summaries),
		observations: readObservations(notes), setAside: setAside, method: rest}
	rep.present = present(rep.now)
	rep.matchups = matchups(rep)
	appendix, err := appendixMarkdown(rep)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(here(), "report.md"), []byte(renderMarkdown(rep)+appendix), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(here(), "report.html"), []byte(renderPage(rep, appendix)), 0o644)
}

func byArm(rows []row) map[string][]row {
	grouped := map[string][]row{}
	for _, r := range rows {
		grouped[r.Arm] = append(grouped[r.Arm], r)
	}
	return grouped
}

func matchups(rep report) []matchup {
	var out []matchup
	for _, mine := range rep.present {
		for _, theirs := range rep.present {
			if mine.Tool != "tofu" || theirs.Tool == "tofu" || rep.now[mine.Name][0].Model != rep.now[theirs.Name][0].Model {
				continue
			}
			p := matchup{rival: theirs, model: rep.now[mine.Name][0].Model}
			for _, m := range rep.measures {
				tofu, okTofu := statOf(rep.now[mine.Name], m)
				rival, okRival := statOf(rep.now[theirs.Name], m)
				if !okTofu || !okRival {
					continue
				}
				d := duel{m: m, tofu: tofu, rival: rival, result: judge(m, tofu, rival), before: unrecorded}
				tofuThen, okTofu := statOf(rep.then[mine.Name], m)
				rivalThen, okRival := statOf(rep.then[theirs.Name], m)
				if okTofu && okRival {
					d.before = judge(m, tofuThen, rivalThen)
				}
				p.duels = append(p.duels, d)
			}
			out = append(out, p)
		}
	}
	return out
}

func (r row) file() string {
	return path.Join("runs", r.Batch, r.Task, r.name()+".json")
}

func readRows(pattern string) ([]row, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	rows := make([]row, 0, len(paths))
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r row
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func present(byArm map[string][]row) []arm {
	return slices.DeleteFunc(slices.Clone(arms), func(a arm) bool { return len(byArm[a.Name]) == 0 })
}

func (r row) build() string {
	if r.Tool == "tofu" {
		return fmt.Sprintf("%s commit %s", r.Version, cmp.Or(fmt.Sprintf("%.8s", r.Commit), "not recorded"))
	}
	return r.Version
}

func distinct(rows []row, field func(row) string) string {
	var seen []string
	for _, r := range rows {
		if v := field(r); !slices.Contains(seen, v) {
			seen = append(seen, v)
		}
	}
	return strings.Join(seen, "; ")
}

func (r row) notes() string {
	notes := append(slices.Clone(r.Notes), r.Score.Failures...)
	if len(r.Score.Stray) > 0 {
		notes = append(notes, "stray: "+strings.Join(r.Score.Stray, ", "))
	}
	if len(r.Score.HarnessFiles) > 0 {
		notes = append(notes, "harness files left in the repo: "+strings.Join(r.Score.HarnessFiles, ", "))
	}
	return strings.ReplaceAll(strings.Join(notes, "; "), "|", "/")
}

func failedCalls(r row) int {
	n := 0
	for _, c := range r.Calls {
		if c.Failed {
			n++
		}
	}
	return n
}

func valuesOf(rows []row, value func(row) float64) []float64 {
	values := make([]float64, 0, len(rows))
	for _, r := range rows {
		values = append(values, value(r))
	}
	return values
}

func spread(rows []row, value func(row) float64) string {
	values := valuesOf(rows, value)
	return fmt.Sprintf("%s [%s to %s]", trim(median(values)), trim(slices.Min(values)), trim(slices.Max(values)))
}

func median(values []float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func rank(sorted []int64, quantile float64) string {
	if len(sorted) == 0 {
		return "none"
	}
	index := max(int(math.Ceil(quantile*float64(len(sorted))))-1, 0)
	return fmt.Sprint(sorted[index])
}

func trim(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func seconds(ms int64) float64 {
	return float64(ms) / 1000
}

func megabytes(bytes uint64) float64 {
	return float64(bytes) / (1 << 20)
}
