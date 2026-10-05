package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
)

var rowOrder = []string{"act navigate", "act back", "act wait", "act click", "act fill", "act other", "observe interactive", "observe text", "observe from", "read"}

type event struct {
	Agent string          `json:"agent"`
	Call  string          `json:"call"`
	Kind  string          `json:"kind"`
	Body  json.RawMessage `json:"body"`
}

type callBody struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

type resultBody struct {
	ResultBytes int    `json:"result_bytes"`
	Command     string `json:"command"`
	Content     string `json:"content"`
}

type messageBody struct {
	Role     string          `json:"role"`
	Content  json.RawMessage `json:"content"`
	Thinking string          `json:"thinking"`
}

type browserArgs struct {
	Interactive *bool `json:"interactive"`
	From        int   `json:"from"`
	Actions     []struct {
		Action string `json:"action"`
	} `json:"actions"`
}

type row struct {
	sizes                                []int
	urlBytes, cut                        int
	parts                                [partCount]int
	shapes                               [shapeCount]int
	diffed, diffNow, diffAfter           int
	statuses                             [ompCount]int
	textTrees, textBytes, textSaved      int
	textTreeBytes                        int
	usedTrees, usedBlockBytes, refetched int
}

type tally struct {
	rows                                      map[string]*row
	roles                                     map[string]int
	seenCalls                                 map[string]bool
	files, duplicateSessions, browserSessions int
	duplicateCalls, badLines, results, whole  int
}

type session struct {
	calls     map[string]callBody
	baselines map[string]baseline
	watches   map[string]*textWatch
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	projects := flag.String("projects", filepath.Join(home, ".tofu", "projects"), "the recorded sessions, read only")
	flag.Parse()
	paths, err := filepath.Glob(filepath.Join(*projects, "*", "sessions", "*", "events.jsonl"))
	if err != nil {
		return err
	}
	t := tally{rows: map[string]*row{}, roles: map[string]int{}, seenCalls: map[string]bool{}, files: len(paths)}
	for _, path := range bySession(paths, &t) {
		if err := t.read(path); err != nil {
			return err
		}
	}
	host, _ := os.Hostname()
	fmt.Printf("browser output split, %s, machine %s %s/%s, offline over %s\n", time.Now().Format("2006-01-02 15:04"), host, runtime.GOOS, runtime.GOARCH, *projects)
	fmt.Printf("session files %d, distinct sessions read %d, duplicate session ids skipped %d, sessions with a browser result %d\n",
		t.files, t.files-t.duplicateSessions, t.duplicateSessions, t.browserSessions)
	fmt.Printf("browser results %d, content held whole for %d, duplicate call ids skipped %d, malformed lines %d, tokens are bytes/%d\n\n",
		t.results, t.whole, t.duplicateCalls, t.badLines, konst.SearchBytesPerToken)
	rows := t.ordered()
	t.printSplit(rows)
	printDiff(rows)
	printText(rows)
	return nil
}

func bySession(paths []string, t *tally) []string {
	type file struct {
		path string
		size int64
	}
	largest := map[string]file{}
	for _, path := range paths {
		id := filepath.Base(filepath.Dir(path))
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if _, seen := largest[id]; seen {
			t.duplicateSessions++
		}
		if info.Size() > largest[id].size {
			largest[id] = file{path, info.Size()}
		}
	}
	var kept []string
	for _, found := range largest {
		kept = append(kept, found.path)
	}
	slices.Sort(kept)
	return kept
}

func (t *tally) read(path string) error {
	recorded, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := session{calls: map[string]callBody{}, baselines: map[string]baseline{}, watches: map[string]*textWatch{}}
	browsed := false
	for line := range bytes.Lines(recorded) {
		if len(bytes.TrimSpace(line)) > 0 && t.event(&s, line) {
			browsed = true
		}
	}
	for agent := range s.watches {
		t.close(&s, agent, "", "", "")
	}
	if browsed {
		t.browserSessions++
	}
	return nil
}

func (t *tally) event(s *session, line []byte) bool {
	var e event
	if json.Unmarshal(line, &e) != nil {
		t.badLines++
		return false
	}
	switch e.Kind {
	case "message":
		var m messageBody
		if json.Unmarshal(e.Body, &m) != nil || m.Role != "assistant" {
			return false
		}
		var text string
		if json.Unmarshal(m.Content, &text) != nil {
			text = string(m.Content)
		}
		s.watches[e.Agent].read(text + m.Thinking)
	case "tool_call":
		var c callBody
		if json.Unmarshal(e.Body, &c) != nil {
			return false
		}
		s.calls[e.Call] = c
		s.watches[e.Agent].read(string(c.Args))
	case "tool_result":
		call, found := s.calls[e.Call]
		label := rowOf(call)
		if !found || label == "" {
			return false
		}
		delete(s.calls, e.Call)
		if t.seenCalls[e.Call] {
			t.duplicateCalls++
			return false
		}
		t.seenCalls[e.Call] = true
		var r resultBody
		if json.Unmarshal(e.Body, &r) != nil {
			t.badLines++
			return false
		}
		t.result(s, e.Agent, label, r)
		return true
	}
	return false
}

func rowOf(call callBody) string {
	var args browserArgs
	_ = json.Unmarshal(call.Args, &args)
	switch call.Tool {
	case "browser_read":
		return "read"
	case "browser_observe":
		interactive := args.From == 0
		if args.Interactive != nil {
			interactive = *args.Interactive
		}
		switch {
		case args.From > 1:
			return "observe from"
		case interactive:
			return "observe interactive"
		}
		return "observe text"
	case "browser_act":
		ran := map[string]bool{}
		for _, step := range args.Actions {
			ran[step.Action] = true
		}
		for _, loads := range []string{"navigate", "back", "wait"} {
			if ran[loads] {
				return "act " + loads
			}
		}
		if len(args.Actions) > 0 && (args.Actions[0].Action == "click" || args.Actions[0].Action == "fill") {
			return "act " + args.Actions[0].Action
		}
		return "act other"
	}
	return ""
}

func (t *tally) result(s *session, agent, label string, r resultBody) {
	t.results++
	if len(r.Content) == r.ResultBytes {
		t.whole++
	}
	this := t.row(label)
	this.sizes = append(this.sizes, len(r.Content))
	got := split(r.Content)
	for kind, size := range got.bytes {
		this.parts[kind] += size
	}
	for role, size := range got.roles {
		t.roles[role] += size
	}
	this.shapes[got.last.shape]++
	this.urlBytes += got.urlBytes
	if got.last.cutBytes > 0 {
		this.cut++
	}
	fields := strings.Fields(r.Command)
	tab, url := "", got.last.url
	if len(fields) > 1 {
		tab = fields[1]
	}
	if url == "" && label == "read" && len(fields) > 2 {
		url = fields[2]
	}
	t.close(s, agent, label, tab, url)
	if got.last.shape != shapeFullText && got.last.shape != shapeFullInteractive {
		return
	}
	status, after := ompDiff(s.baselines, fmt.Sprintf("%s %s %d", agent, tab, got.last.shape), got.last)
	this.diffed++
	this.diffNow += blockBytes(got.last)
	this.diffAfter += after
	this.statuses[status]++
	if got.last.shape == shapeFullText {
		this.textTrees++
		this.textBytes += got.last.textBytes
		this.textSaved += textSaved(got.last)
		this.textTreeBytes += blockBytes(got.last)
		s.watches[agent] = &textWatch{row: label, tab: tab, url: url, texts: got.last.texts, blockBytes: blockBytes(got.last)}
	}
}

func (t *tally) close(s *session, agent, label, tab, url string) {
	w := s.watches[agent]
	if w == nil {
		return
	}
	delete(s.watches, agent)
	owner := t.row(w.row)
	asksForText := label == "observe text" || label == "observe from" || label == "read"
	if asksForText && w.tab == tab && (w.url == url || url == "") {
		owner.refetched++
	}
	if w.used {
		owner.usedTrees++
		owner.usedBlockBytes += w.blockBytes
	}
}

func (t *tally) row(label string) *row {
	if t.rows[label] == nil {
		t.rows[label] = &row{}
	}
	return t.rows[label]
}

func tokens(size int) int {
	return size / konst.SearchBytesPerToken
}

func percent(part, whole int) float64 {
	return 100 * float64(part) / float64(max(whole, 1))
}

func spread(sizes []int) (int, int, int, int) {
	sorted := slices.Sorted(slices.Values(sizes))
	if len(sorted) == 0 {
		return 0, 0, 0, 0
	}
	total := 0
	for _, size := range sorted {
		total += size
	}
	return total, sorted[len(sorted)/2], sorted[len(sorted)*95/100], sorted[len(sorted)-1]
}

type labelledRow struct {
	label string
	*row
}

func (t *tally) ordered() []labelledRow {
	all := &row{}
	var kept []labelledRow
	for _, label := range rowOrder {
		r := t.rows[label]
		if r == nil {
			continue
		}
		kept = append(kept, labelledRow{label, r})
		all.sizes = append(all.sizes, r.sizes...)
		all.urlBytes, all.cut = all.urlBytes+r.urlBytes, all.cut+r.cut
		all.diffed, all.diffNow, all.diffAfter = all.diffed+r.diffed, all.diffNow+r.diffNow, all.diffAfter+r.diffAfter
		all.textTrees, all.textBytes, all.textSaved, all.textTreeBytes = all.textTrees+r.textTrees, all.textBytes+r.textBytes, all.textSaved+r.textSaved, all.textTreeBytes+r.textTreeBytes
		all.usedTrees, all.usedBlockBytes, all.refetched = all.usedTrees+r.usedTrees, all.usedBlockBytes+r.usedBlockBytes, all.refetched+r.refetched
		for kind := range r.parts {
			all.parts[kind] += r.parts[kind]
		}
		for kind := range r.shapes {
			all.shapes[kind] += r.shapes[kind]
		}
		for status := range r.statuses {
			all.statuses[status] += r.statuses[status]
		}
	}
	return append(kept, labelledRow{"all", all})
}

func (t *tally) printSplit(rows []labelledRow) {
	grand, _, _, _ := spread(rows[len(rows)-1].sizes)
	fmt.Println("split by what ran and by part of the result, bytes of content; url% is the share of the refs part spent on url= attributes")
	fmt.Printf("  %-20s %6s %11s %6s %7s %7s %7s  %7s %7s %7s %7s %6s  %9s %10s %6s %9s %7s %5s\n", "row", "calls", "bytes", "share", "p50", "p95", "max",
		"header%", "refs%", "text%", "struct%", "url%", shapeNames[shapeFullText], shapeNames[shapeFullInteractive], shapeNames[shapeDelta], shapeNames[shapeUnchanged], shapeNames[shapeNoTree], "cut")
	for _, r := range rows {
		total, p50, p95, most := spread(r.sizes)
		parts := r.parts[partHeader] + r.parts[partRefs] + r.parts[partText] + r.parts[partStructure]
		fmt.Printf("  %-20s %6d %11d %5.1f%% %7d %7d %7d  %6.1f%% %6.1f%% %6.1f%% %6.1f%% %5.1f%%  %9d %10d %6d %9d %7d %5d\n", r.label, len(r.sizes), total, percent(total, grand), p50, p95, most,
			percent(r.parts[partHeader], parts), percent(r.parts[partRefs], parts), percent(r.parts[partText], parts), percent(r.parts[partStructure], parts), percent(r.urlBytes, r.parts[partRefs]),
			r.shapes[shapeFullText], r.shapes[shapeFullInteractive], r.shapes[shapeDelta], r.shapes[shapeUnchanged], r.shapes[shapeNoTree], r.cut)
	}
	fmt.Println("\n  the structure part by role, top 10 by bytes")
	roles := slices.SortedFunc(maps.Keys(t.roles), func(a, b string) int { return t.roles[b] - t.roles[a] })
	for _, role := range roles[:min(10, len(roles))] {
		fmt.Printf("    %-20s %10d bytes  %5.1f%% of all content\n", role, t.roles[role], percent(t.roles[role], grand))
	}
	fmt.Println()
}

func printDiff(rows []labelledRow) {
	fmt.Println("estimate 1: omp's diff (snapshot-plus.ts:216-237) on every full tree, against the last full tree of the same tab and kind in the same session")
	fmt.Println("  arm: today's output for the same results")
	fmt.Printf("  %-20s %6s %11s %11s %11s %9s  %6s %6s %9s %6s\n", "row", "trees", "bytes_now", "bytes_diff", "saved", "tokens", ompNames[ompFull], ompNames[ompDelta], ompNames[ompUnchanged], "saved%")
	for _, r := range rows {
		if r.diffed > 0 {
			fmt.Printf("  %-20s %6d %11d %11d %11d %9d  %6d %6d %9d %5.1f%%\n", r.label, r.diffed, r.diffNow, r.diffAfter, r.diffNow-r.diffAfter, tokens(r.diffNow-r.diffAfter),
				r.statuses[ompFull], r.statuses[ompDelta], r.statuses[ompUnchanged], percent(r.diffNow-r.diffAfter, r.diffNow))
		}
	}
	fmt.Println()
}

func printText(rows []labelledRow) {
	fmt.Println("estimate 2: a full tree without StaticText lines, on every tree that carried text")
	fmt.Printf("  arm: today's output; quoted = a StaticText of %d bytes or more, in no other line of that tree, appears in the model's next words or call arguments before its next browser result\n", quoteMinBytes)
	fmt.Println("  refetched = the next browser result on the same agent was observe text, observe from or read on the same tab and url, with the text already in hand")
	fmt.Printf("  saved = the text bytes, except on a cut tree, where the window refills with other lines up to %d bytes at the shown ratio\n", konst.BrowserSnapshotMaxBytes)
	fmt.Println("  net = saved minus the whole tree paid again for every quoted tree; breakeven = the quoted share at which net reaches zero")
	fmt.Printf("  %-20s %6s %11s %11s %9s %7s %6s %9s %11s %9s %9s\n", "row", "trees", "text_bytes", "saved", "tokens", "quoted", "share", "refetched", "net_bytes", "net_tok", "breakeven")
	for _, r := range rows {
		if r.textTrees > 0 {
			net := r.textSaved - r.usedBlockBytes
			fmt.Printf("  %-20s %6d %11d %11d %9d %7d %5.1f%% %9d %11d %9d %8.1f%%\n", r.label, r.textTrees, r.textBytes, r.textSaved, tokens(r.textSaved),
				r.usedTrees, percent(r.usedTrees, r.textTrees), r.refetched, net, tokens(net), percent(r.textSaved, r.textTreeBytes))
		}
	}
}
