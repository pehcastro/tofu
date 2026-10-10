package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/interface/tui/frame"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/learn"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/library/changelog"
)

const (
	learnUsage  = `tofu learn scan [--chain <session or family>|--last N|--global] [--local] [--all] [--dir project] [--json], tofu learn show|upstream <n>, tofu learn apply <n> [--scope user-local|project-local|project-global|user-global] [--dir project], tofu learn reject <n> --reason "<why>", tofu learn upstream --list`
	learnRuleID = "learned_"
	timeOfDay   = "15:04"
	dayAndTime  = "2006-01-02 15:04"
)

type learnOpts struct {
	chain, dir, reason       string
	scope                    memory.Scope
	last                     int
	global, local, all, list bool
	rest                     []string
}

func parseLearnArgs(args []string) (learnOpts, error) {
	opts := learnOpts{dir: ".", last: learn.SessionsByDefault}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--chain" || arg == "--last" || arg == "--dir" || arg == "--reason" || arg == "--scope" {
			if i++; i >= len(args) {
				return learnOpts{}, fmt.Errorf("%s needs a value", arg)
			}
		}
		switch arg {
		case "--chain":
			opts.chain = args[i]
		case "--last":
			last, err := strconv.Atoi(args[i])
			if err != nil || last < 1 {
				return learnOpts{}, fmt.Errorf("--last is a count of sessions, found %q", args[i])
			}
			opts.last = last
		case "--dir":
			opts.dir = args[i]
		case "--reason":
			opts.reason = strings.TrimSpace(args[i])
		case "--global":
			opts.global = true
		case "--local":
			opts.local = true
		case "--all":
			opts.all = true
		case "--scope":
			scope, err := memory.ParseScope(args[i])
			if err != nil {
				return learnOpts{}, err
			}
			opts.scope = scope
		case "--list":
			opts.list = true
		case jsonFlag:
		default:
			if strings.HasPrefix(arg, "-") {
				return learnOpts{}, fmt.Errorf("unknown argument %q", arg)
			}
			opts.rest = append(opts.rest, arg)
		}
	}
	return opts, nil
}

func learnVerb(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		args = []string{"scan"}
	}
	o := verbOutput{verb: "learn " + args[0], usageLine: learnUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	opts, err := parseLearnArgs(args[1:])
	if !slices.Contains([]string{"scan", "show", "apply", "reject", "upstream"}, args[0]) {
		err = fmt.Errorf("no learn verb %q", args[0])
	}
	if err != nil {
		return o.usage(err)
	}
	home, err := learn.OpenHome()
	if err != nil {
		return o.fail(err)
	}
	if args[0] == "scan" {
		return learnScan(o, opts, home)
	}
	if args[0] == "upstream" && opts.list {
		return learnDrafts(o, home)
	}
	run, err := home.Last()
	if err != nil {
		return o.fail(err)
	}
	id := 0
	if len(opts.rest) == 1 {
		id, err = strconv.Atoi(opts.rest[0])
	}
	finding, found := run.Find(id)
	if err != nil || !found {
		return o.usage(fmt.Errorf("no finding %q in the last run, %s", strings.Join(opts.rest, " "), run.ID))
	}
	decide := func(action learn.Action) error {
		return home.Decide(learn.Decision{At: time.Now(), Run: run.ID, ID: finding.ID, Key: finding.Key, Action: action, Places: finding.Sessions, Reason: opts.reason})
	}
	switch args[0] {
	case "show":
		return o.done(true, finding, func(page cli.Page) []string { return showLines(page, run, finding) })
	case "reject":
		if opts.reason == "" {
			return o.usage(errors.New(`a rejection says why: --reason "<why>"`))
		}
		if err := decide(learn.ActionReject); err != nil {
			return o.fail(err)
		}
		return o.done(true, finding, func(page cli.Page) []string {
			return []string{page.Glyph(cli.Removed) + " rejected " + strconv.Itoa(finding.ID) + page.Label(" · quiet until more sessions than "+strconv.Itoa(finding.Sessions)+" support it")}
		})
	case "upstream":
		if finding.Target() != learn.TargetUpstream {
			return o.usage(fmt.Errorf("finding %d changes something of yours; tofu learn apply %d", finding.ID, finding.ID))
		}
		return learnDraft(o, home, finding, decide)
	}
	return learnApply(o, opts, home, finding, decide)
}

func learnSources(opts learnOpts) ([]learn.Source, string, error) {
	if !opts.global {
		store, err := session.OpenIn(opts.dir)
		if err != nil {
			return nil, "", err
		}
		if opts.chain != "" {
			headers, err := learn.Chain(store, opts.chain)
			return []learn.Source{{Store: store, Headers: headers}}, opts.chain, err
		}
		headers, err := learn.Recent(store, opts.last)
		return []learn.Source{{Store: store, Headers: headers}}, plural(len(headers), "session") + " of the last " + strconv.Itoa(opts.last) + " conversations of this project", err
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, "", err
	}
	states, err := sys.ProjectGlob(filepath.Join(home, sys.ProjectsDirName), "*")
	var sources []learn.Source
	for _, state := range states {
		store := session.OpenAt(state)
		headers, err := learn.Recent(store, opts.last)
		if err != nil {
			return nil, "", err
		}
		sources = append(sources, learn.Source{Store: store, Headers: headers, Project: filepath.Base(state)})
	}
	return sources, "every project, the last " + strconv.Itoa(opts.last) + " conversations of each", err
}

func learnScan(o verbOutput, opts learnOpts, home learn.Home) int {
	sources, scope, err := learnSources(opts)
	if err != nil {
		return o.fail(err)
	}
	shelves, err := memory.Open(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	decisions, err := home.Decisions()
	if err != nil {
		return o.fail(err)
	}
	known := learn.Known{Memory: shelves.All(), Decisions: decisions, Releases: learn.Releases(changelog.Markdown), Settings: map[string]string{}}
	for _, spec := range settingspkg.Default() {
		known.Settings[spec.Key] = spec.Description
	}
	run, err := learn.Scan(sources, known, time.Now())
	if err != nil {
		return o.fail(err)
	}
	run.Scope, run.Sent.Local = scope, opts.local || !learnOn(opts.dir)
	if !run.Sent.Local && len(run.Said) > 0 {
		if err := labelWindows(&run, o.errOut); err != nil {
			return o.fail(err)
		}
		groupByMeaning(&run, known, opts.dir, o.errOut)
	}
	file, err := home.Save(run)
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, run, func(page cli.Page) []string { return scanLines(page, run, opts, file) })
}

func learnOn(dir string) bool {
	store, err := openSettings(dir)
	return err == nil && store.Bool(settingspkg.Learn)
}

func labelWindows(run *learn.Run, errOut io.Writer) error {
	set, err := learn.Questions()
	if err != nil {
		return err
	}
	asked := battery{SetName: set.Name, QuestionsVersion: set.QuestionsVersion, Kinds: map[string]question.Kind{}}
	for _, q := range set.Questions {
		asked.Questions, asked.Kinds[q.Name] = append(asked.Questions, q.ToJev()), q.Kind
	}
	key, err := gateKey()
	if err != nil {
		return err
	}
	client, err := jevClientOn(key, 1)
	if err != nil {
		return err
	}
	states := run.Windows()
	for _, state := range states {
		body, _ := json.Marshal(state)
		run.Sent.Bytes += len(body)
	}
	run.Sent.Windows = len(states)
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, []string{page.Label(fmt.Sprintf("sending %d windows to Jev, one request each, %d bytes of your sessions", len(states), run.Sent.Bytes))})
	for i, state := range states {
		label := learn.Label{Said: i}
		decision, err := client.Ask(context.Background(), jev.Request{State: state, Questions: asked.Questions})
		if err == nil {
			var row ledger.Row
			row, err = appendRow(state, asked, rowInput{decision: &decision, answers: toLedgerAnswers(set.QuestionsVersion, decision.Answers), stateBuilder: set.Name + "@" + strconv.Itoa(set.QuestionsVersion)})
			label.Row, label.Build, label.Cost = row.ID, decision.Build, decision.Usage.Cost
			label.Answers, label.Chosen = map[string]float64{}, map[string]string{}
			for name, answer := range decision.Answers {
				if answer.Kind == jev.QuestionChoice {
					label.Chosen[name] = answer.Choice
					continue
				}
				label.Answers[name] = answer.Noul
			}
		}
		if err != nil {
			label.Failure, run.Sent.Failed = err.Error(), run.Sent.Failed+1
		}
		run.Sent.Cost += label.Cost
		run.Labels = append(run.Labels, label)
	}
	return nil
}

func groupByMeaning(run *learn.Run, known learn.Known, dir string, errOut io.Writer) {
	opts := onTheBoundKeyWire(runOpts{dir: dir, wire: wireSubscription, effort: llm.EffortDefault})
	if opts.wire == wireKey {
		run.Sent.Kept = "the lead model is reached through the OpenRouter key, which is for Jev only"
		return
	}
	selected, err := chooseModel(opts)
	if err == nil {
		prompt := run.Prompt(known)
		run.Sent.GroupBytes = len(prompt)
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, []string{page.Label(fmt.Sprintf("grouping %d messages by meaning with %s on your subscription, one request, %d bytes", len(run.Said), selected.Slug(), len(prompt)))})
		var decision llm.Decision
		decision, err = subscriptionModel{opts}.Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}}})
		run.Sent.GroupTokens = decision.Usage.InputTokens + decision.Usage.OutputTokens
		if err == nil {
			err = run.Group(cmp.Or(decision.Content, decision.Refusal), known, selected.Slug())
		}
	}
	if err != nil {
		run.Sent.Kept = err.Error()
	}
}

func learnApply(o verbOutput, opts learnOpts, home learn.Home, finding learn.Finding, decide func(learn.Action) error) int {
	switch finding.Target() {
	case learn.TargetUpstream:
		return learnDraft(o, home, finding, decide)
	case learn.TargetNone:
		return o.usage(fmt.Errorf("finding %d is about your project, not tofu; there is nothing to apply", finding.ID))
	case learn.TargetSetting:
		if code := settingsVerb([]string{"set", "--scope", "project", finding.Setting, finding.Value}, o.out, o.errOut); code != exitOK {
			return code
		}
		if err := decide(learn.ActionApply); err != nil {
			return o.fail(err)
		}
		return exitOK
	}
	if finding.Rule == "" {
		return o.usage(fmt.Errorf("finding %d has no rule worded yet; with tofu settings set learn true a scan words it with your lead model, or tofu memory add \"<rule>\" keeps your own", finding.ID))
	}
	if finding.Target() == learn.TargetRule {
		flag := "--project"
		if finding.Scope == memory.Global {
			flag = "--global"
		}
		if code := rulesAddVerb([]string{flag, "--dir", opts.dir, learnRuleID + finding.Key, finding.Rule}, o.out, o.errOut); code != exitOK {
			return code
		}
		shelves, err := memory.Open(opts.dir)
		if err == nil {
			_, err = shelves.Remove(finding.Scope, finding.Retire)
		}
		if err = errors.Join(err, decide(learn.ActionApply)); err != nil {
			return o.fail(err)
		}
		return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "memory " + finding.Retire + ", now the rule " + learnRuleID + finding.Key}},
			Undo: "tofu memory add --scope " + string(finding.Scope) + " --said " + strconv.Quote(finding.Said) + " " + strconv.Quote(finding.Rule)})
	}
	shelves, err := memory.Open(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	added, err := shelves.Add(memory.Entry{Scope: cmp.Or(opts.scope, finding.Scope), Kind: memory.KindPerson, Text: finding.Rule, Said: finding.Said, Session: finding.Session, At: time.Now(), By: memory.ByOffer}, "")
	if err = errors.Join(err, decide(learn.ActionApply)); err != nil {
		return o.fail(err)
	}
	for _, notice := range shelves.Notices {
		_, _ = fmt.Fprintln(o.errOut, notice)
	}
	undo := added.Undo()
	if opts.dir != "." {
		undo += " --dir " + strconv.Quote(opts.dir)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeAdded, What: "memory " + added.ID + " · " + string(added.Scope), File: added.File}}, Undo: undo})
}

func learnDraft(o verbOutput, home learn.Home, finding learn.Finding, decide func(learn.Action) error) int {
	build := learn.Build{Version: frame.Release(sys.Version(), sys.BuildRevision()), Commit: sys.BuildRevision(), Platform: sys.OS() + "/" + sys.Arch()}
	file, text, err := home.WriteDraft(learn.DraftOf(finding, build, time.Now()))
	if err = errors.Join(err, decide(learn.ActionDraft)); err != nil {
		return o.fail(err)
	}
	if !o.asJSON {
		_, _ = fmt.Fprintln(o.out, text)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeAdded, What: "upstream draft " + finding.Key + " · written, not sent", File: file}}, Undo: "delete " + file})
}

func learnDrafts(o verbOutput, home learn.Home) int {
	listed, err := home.Drafts()
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, listed, func(page cli.Page) []string {
		lines := []string{page.Subject("Upstream drafts") + page.Label(" · "+strconv.Itoa(len(listed))+" · none sent   "+page.Path(home.UpstreamDir()))}
		for _, l := range listed {
			mark := cli.Done
			if !l.Marked {
				mark = cli.Fail
			}
			lines = append(lines, fmt.Sprintf("  %s %-8s %-8s %s", page.Glyph(mark), l.Draft.Key, l.Draft.Kind, l.Draft.Title),
				"      "+page.Label(fmt.Sprintf("%s · %d sessions · written %s · %s", l.Draft.Mechanism, l.Draft.Sessions, l.Draft.Written.Format(dayAndTime), page.Path(l.File))))
		}
		return lines
	})
}

func scanLines(page cli.Page, run learn.Run, opts learnOpts, file string) []string {
	read := run.Read
	head := page.Subject("Learn") + page.Label(" · "+plural(len(read.Sessions), "session")+" · "+run.Scope)
	if read.Typed > 0 {
		head += page.Label(" · " + read.From.Local().Format(time.DateOnly) + " to " + read.To.Local().Format(time.DateOnly))
	}
	how := "grouped by shared words only, with no model: the weaker mode; tofu settings set learn true groups by meaning"
	if run.Mode == learn.ModeModel {
		how = fmt.Sprintf("grouped by meaning by %s on your subscription, one request, %d tokens", run.Model, run.Sent.GroupTokens)
	}
	lines := []string{head, "  " + page.Label(how)}
	if run.Sent.Kept != "" {
		lines = append(lines, "  "+page.Label("the model did not group them, so the weaker grouping is shown: "+clip(run.Sent.Kept, learn.ShownTextRunes)))
	}
	lines = append(lines, "", page.Subject("What keeps going wrong"))
	for i, wrong := range run.Summary.Wrong {
		lines = append(lines, fmt.Sprintf("  %d %s", i+1, wrong))
	}
	if len(run.Summary.Wrong) == 0 {
		lines = append(lines, "  nothing said again in two or more sessions")
	}
	lines = append(lines, "  "+page.Label(fmt.Sprintf("it cost you %s, and the lead %s answering them", plural(run.Summary.Repeats, "repeat"), plural(run.Summary.Calls, "call"))))
	if len(run.Summary.Do) > 0 {
		lines = append(lines, "  "+page.Hint(strings.Join(run.Summary.Do, " · ")))
	}
	all := len(run.Findings) + len(run.Held)
	lines = append(lines, "", page.Subject("Findings")+page.Label(fmt.Sprintf(" · %d of %d, the cap is %d", len(run.Findings), all, learn.ProposalCap)))
	for _, f := range run.Findings {
		lines = append(lines, findingLines(page, f)...)
	}
	sections := []struct {
		name     string
		findings []learn.Finding
	}{{"Held back by the cap", run.Held}, {"Fixed in a later tofu", run.Fixed}, {"Already applied, rejected or drafted, until more sessions say it", run.Decided}, {"Watching, one session so far", run.Watching}, {"About your project, not tofu's business", run.Project}}
	for _, section := range sections {
		if len(section.findings) == 0 {
			continue
		}
		lines = append(lines, "", page.Subject(section.name)+page.Label(" · "+strconv.Itoa(len(section.findings))))
		shown := section.findings
		if !opts.all {
			shown = shown[:min(len(shown), learn.ProposalCap)]
		}
		for _, f := range shown {
			lines = append(lines, "  "+clip(f.Title, learn.ShownTextRunes), "    "+page.Label(briefOf(f)))
		}
	}
	sent := "nothing, --local"
	switch {
	case run.Sent.Local && !opts.local:
		sent = "nothing: learn is off; tofu settings set learn true sends the windows"
	case !run.Sent.Local:
		sent = fmt.Sprintf("%d windows, %d bytes, %.6f USD, %d failed; labels uncalibrated, shown and not used", run.Sent.Windows, run.Sent.Bytes, run.Sent.Cost, run.Sent.Failed)
	}
	lines = append(lines, "", page.Subject("Read"),
		fmt.Sprintf("  %d of your messages, %d repeated prompts set aside · %d lead calls, %d sub-agent reports, %d forks", read.Typed, read.Repeated, read.Calls, read.Reports, read.Forks),
		"  to Jev  "+sent)
	if run.Sent.Dropped > 0 {
		lines = append(lines, fmt.Sprintf("  the model's answer: %d fields refused, such as %s", run.Sent.Dropped, strings.Join(run.Sent.Refused[:min(len(run.Sent.Refused), 2)], "; ")))
	}
	return append(lines, "  "+page.Label("run kept at ")+page.Path(file), page.Hint("tofu learn show <n> for the evidence · apply <n> · reject <n> --reason \"...\" · upstream <n>"))
}

func briefOf(f learn.Finding) string {
	brief := fmt.Sprintf("%s in %s · %s", plural(f.Times, "time"), plural(f.Sessions, "session"), f.Class.Label())
	if f.FixedIn != "" {
		brief += " · fixed in " + f.FixedIn + ", last seen on " + f.Built
		if f.FixedSameDay {
			brief += ", released the day it was last seen"
		}
	}
	if command := f.Command(); command != "" && f.ID > 0 {
		brief += " · " + command
	}
	return brief
}

func findingLines(page cli.Page, f learn.Finding) []string {
	lines := []string{fmt.Sprintf("  %-2d %s", f.ID, f.Title),
		"     " + page.Label(fmt.Sprintf("%s in %s · %s · %s confidence", plural(f.Times, "time"), plural(f.Sessions, "session"), f.Class.Label(), f.Confidence)),
		"     why   " + clip(f.Reason, learn.ShownTextRunes)}
	switch {
	case f.Target() == learn.TargetUpstream:
		lines = append(lines, "     fix   a report for tofu, written to a file and never sent")
	case f.Setting != "":
		lines = append(lines, "     fix   tofu settings set --scope project "+f.Setting+" "+f.Value)
	case f.Rule != "":
		lines = append(lines, "     fix   "+string(f.Target())+": "+clip(f.Rule, learn.ShownTextRunes))
	default:
		lines = append(lines, "     fix   no rule worded without a model")
	}
	if command := f.Command(); command != "" {
		lines = append(lines, "           "+page.Hint(command))
	}
	for _, q := range f.Quotes[:min(len(f.Quotes), learn.ShownQuotes)] {
		lines = append(lines, "     "+page.Label(fmt.Sprintf("%s %s %s", q.At.Local().Format(timeOfDay), q.Session, strconv.Quote(clip(q.Text, learn.ShownQuoteRunes)))))
	}
	return lines
}

func clip(text string, runes int) string {
	text = oneLine(text)
	if len([]rune(text)) <= runes {
		return text
	}
	return string([]rune(text)[:runes]) + "..."
}

func showLines(page cli.Page, run learn.Run, f learn.Finding) []string {
	lines := append([]string{page.Subject("Finding "+strconv.Itoa(f.ID)) + page.Label(" · run "+run.ID), "  " + f.Title}, findingLines(page, f)[1:3]...)
	if f.Rule != "" {
		lines = append(lines, "  rule: "+f.Rule)
	}
	if f.Retire != "" {
		lines = append(lines, "  "+page.Label("retires memory "+f.Retire+" in the "+string(f.Scope)+" scope"))
	}
	if f.Built != "" {
		lines = append(lines, "  "+page.Label("last seen on tofu "+f.Built+", from the day it was said and the release dates"))
	}
	lines = append(lines, "")
	for _, q := range f.Quotes {
		lines = append(lines, fmt.Sprintf("  %s %-18s %s", q.At.Local().Format(dayAndTime), q.Session, strconv.Quote(oneLine(q.Text))))
		at := slices.IndexFunc(run.Said, func(s learn.Said) bool { return s.At.Equal(q.At) && s.Session == q.Session })
		if at >= 0 && at < len(run.Labels) {
			label := run.Labels[at]
			lines = append(lines, page.Label(fmt.Sprintf("    jev, uncalibrated: %v %v %s", label.Answers, label.Chosen, label.Failure)))
		}
	}
	for _, c := range f.Checks {
		held := page.Glyph(cli.Fail) + " absent"
		if c.Present {
			held = page.Glyph(cli.Done) + " present"
		}
		lines = append(lines, fmt.Sprintf("  %s when it recurred at %s in %s, request %s", held, c.At.Local().Format(timeOfDay), c.Session, c.Request))
	}
	return lines
}
