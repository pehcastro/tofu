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
)

const (
	learnUsage  = `tofu learn scan [--chain <session>|--last N|--global] [--local] [--all] [--dir project] [--json], tofu learn show|upstream <n>, tofu learn apply <n> [--project] [--dir project], tofu learn reject <n> --reason "<why>", tofu learn upstream --list`
	learnRuleID = "learned_"
	timeOfDay   = "15:04"
	dayAndTime  = "2006-01-02 15:04"
)

type learnOpts struct {
	chain, dir, reason                string
	last                              int
	global, local, all, project, list bool
	rest                              []string
}

func parseLearnArgs(args []string) (learnOpts, error) {
	opts := learnOpts{dir: ".", last: learn.SessionsByDefault}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--chain" || arg == "--last" || arg == "--dir" || arg == "--reason" {
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
		case "--project":
			opts.project = true
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
	proposal, found := run.Find(id)
	if err != nil || !found {
		return o.usage(fmt.Errorf("no proposal %q in the last run, %s", strings.Join(opts.rest, " "), run.ID))
	}
	decide := func(action learn.Action) error {
		return home.Decide(learn.Decision{At: time.Now(), Run: run.ID, ID: proposal.ID, Key: proposal.Key, Action: action, Places: proposal.Places, Reason: opts.reason})
	}
	switch args[0] {
	case "show":
		return o.done(true, proposal, func(page cli.Page) []string { return showLines(page, run, proposal) })
	case "reject":
		if opts.reason == "" {
			return o.usage(errors.New(`a rejection says why: --reason "<why>"`))
		}
		if err := decide(learn.ActionReject); err != nil {
			return o.fail(err)
		}
		return o.done(true, proposal, func(page cli.Page) []string {
			return []string{page.Glyph(cli.Removed) + " rejected " + strconv.Itoa(proposal.ID) + page.Label(" · quiet until more sessions than "+strconv.Itoa(proposal.Places)+" support it")}
		})
	case "upstream":
		if proposal.Target != learn.TargetUpstream {
			return o.usage(fmt.Errorf("proposal %d changes something of yours; tofu learn apply %d", proposal.ID, proposal.ID))
		}
		return learnDraft(o, home, proposal, decide)
	}
	return learnApply(o, opts, home, proposal, decide)
}

func learnSources(opts learnOpts) ([]learn.Source, string, error) {
	if !opts.global {
		store, err := session.OpenIn(opts.dir)
		if err != nil {
			return nil, "", err
		}
		if opts.chain != "" {
			headers, err := learn.Chain(store, opts.chain)
			return []learn.Source{{Store: store, Headers: headers}}, "the chain ending in " + opts.chain, err
		}
		headers, err := learn.Recent(store, opts.last)
		return []learn.Source{{Store: store, Headers: headers}}, countOf(len(headers), "session") + " of the last " + strconv.Itoa(opts.last) + " conversations of this project", err
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, "", err
	}
	states, err := filepath.Glob(filepath.Join(home, sys.ProjectsDirName, "*"))
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
	run, err := learn.Scan(sources, learn.Known{Memory: append(shelves.Global.Entries, shelves.Project.Entries...), Decisions: decisions}, time.Now())
	if err != nil {
		return o.fail(err)
	}
	run.Scope, run.Sent.Local = scope, opts.local || !learnOn(opts.dir)
	if !run.Sent.Local {
		if err := errors.Join(labelWindows(&run, o.errOut), writeStatements(&run, opts.dir)); err != nil {
			return o.fail(err)
		}
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

func writeStatements(run *learn.Run, dir string) error {
	opts := onTheBoundKeyWire(runOpts{dir: dir, wire: wireSubscription, effort: llm.EffortDefault})
	if opts.wire == wireKey {
		return errors.New("the lead model is reached through the OpenRouter key, which is for Jev only; tofu learn --local writes the statements without a model")
	}
	selected, err := chooseModel(opts)
	if err != nil {
		return err
	}
	lead := subscriptionModel{opts}
	for _, proposals := range [][]learn.Proposal{run.Proposals, run.Held} {
		for i, p := range proposals {
			if p.Target != learn.TargetMemory {
				continue
			}
			decision, err := lead.Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: p.Prompt()}}})
			text, ok := learn.Statement(decision.Content)
			switch {
			case err != nil:
				run.Sent.Kept = err.Error()
			case !ok:
				run.Sent.Kept = "the reply was not one statement: " + strconv.Quote(clip(cmp.Or(text, decision.Refusal), learn.ShownTextRunes))
			default:
				proposals[i].Text, proposals[i].WrittenBy = text, selected.Slug()
				run.Sent.Written++
			}
		}
	}
	return nil
}

func learnApply(o verbOutput, opts learnOpts, home learn.Home, proposal learn.Proposal, decide func(learn.Action) error) int {
	switch proposal.Target {
	case learn.TargetUpstream:
		return learnDraft(o, home, proposal, decide)
	case learn.TargetRule:
		flag, again := "--project", "tofu memory add"
		if proposal.Scope == memory.Global {
			flag, again = "--global", again+" --global"
		}
		if code := rulesAddVerb([]string{flag, "--dir", opts.dir, learnRuleID + proposal.Key, proposal.Text}, o.out, o.errOut); code != exitOK {
			return code
		}
		shelves, err := memory.Open(opts.dir)
		if err == nil {
			_, err = shelves.Remove(proposal.Scope, proposal.Retire)
		}
		if err = errors.Join(err, decide(learn.ActionApply)); err != nil {
			return o.fail(err)
		}
		return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "memory " + proposal.Retire + ", now the rule " + learnRuleID + proposal.Key}},
			Undo: again + " --said " + strconv.Quote(proposal.Said) + " " + strconv.Quote(proposal.Text)})
	case learn.TargetMemory:
		shelves, err := memory.Open(opts.dir)
		if err != nil {
			return o.fail(err)
		}
		scope := proposal.Scope
		if opts.project {
			scope = memory.Project
		}
		added, err := shelves.Add(memory.Entry{Scope: scope, Kind: memory.KindPerson, Text: proposal.Text, Said: proposal.Said, Session: proposal.Session, At: time.Now(), By: memory.ByOffer}, "")
		if err = errors.Join(err, decide(learn.ActionApply)); err != nil {
			return o.fail(err)
		}
		undo := "tofu memory remove " + added.ID
		if added.Scope == memory.Global {
			undo = "tofu memory remove --global " + added.ID
		}
		if opts.dir != "." {
			undo += " --dir " + strconv.Quote(opts.dir)
		}
		return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeAdded, What: "memory " + added.ID + " · " + string(added.Scope), File: added.File}}, Undo: undo})
	}
	panic("tofu: unknown learn target " + string(proposal.Target))
}

func learnDraft(o verbOutput, home learn.Home, proposal learn.Proposal, decide func(learn.Action) error) int {
	build := learn.Build{Version: frame.Release(sys.Version(), sys.BuildRevision()), Commit: sys.BuildRevision(), Platform: sys.OS() + "/" + sys.Arch()}
	file, text, err := home.WriteDraft(learn.DraftOf(proposal, build, time.Now()))
	if err = errors.Join(err, decide(learn.ActionDraft)); err != nil {
		return o.fail(err)
	}
	if !o.asJSON {
		_, _ = fmt.Fprintln(o.out, text)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeAdded, What: "upstream draft " + proposal.Key + " · written, not sent", File: file}}, Undo: "delete " + file})
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
	head := page.Subject("Learn") + page.Label(" · "+strconv.Itoa(len(read.Sessions))+" sessions")
	if len(read.Sessions) > 0 {
		head += page.Label(" · " + read.Sessions[0] + " → " + read.Sessions[len(read.Sessions)-1])
	}
	sent := []string{fmt.Sprintf("%d windows, one request each, %d bytes, %.6f USD, %d failed", run.Sent.Windows, run.Sent.Bytes, run.Sent.Cost, run.Sent.Failed), "labels uncalibrated, shown and not used"}
	switch {
	case opts.local:
		sent = []string{"nothing, --local"}
	case run.Sent.Local:
		sent = []string{"nothing: learn is off", "tofu settings set learn true sends the windows"}
	}
	lines := []string{head,
		fmt.Sprintf("  read      %d of your messages, %d repeated prompts set aside", read.Typed, read.Repeated),
		fmt.Sprintf("            %d lead calls, %d sub-agent reports, %d forks", read.Calls, read.Reports, read.Forks),
		"  between   " + read.From.Local().Format(dayAndTime) + " and " + read.To.Local().Format(dayAndTime),
		"  to Jev    " + sent[0]}
	for _, more := range sent[1:] {
		lines = append(lines, "            "+more)
	}
	if !run.Sent.Local {
		lines = append(lines, fmt.Sprintf("  lead      wrote %d statements", run.Sent.Written))
	}
	if run.Sent.Kept != "" {
		lines = append(lines, "            a template was kept: "+run.Sent.Kept)
	}
	lines = append(lines, "", page.Subject("You said it more than once")+page.Label(" · "+strconv.Itoa(len(run.Corrections))))
	for _, theme := range run.Corrections {
		lines = append(lines, "  "+page.Glyph(cli.Fail)+" "+theme.Label(),
			page.Label(fmt.Sprintf("    %d sessions · in the request when it came back: %d of %d", theme.Places, theme.Present(), len(theme.Checks))))
		lines = append(lines, quoteLines(page, theme.Quotes)...)
	}
	proposals := len(run.Proposals) + len(run.Held)
	lines = append(lines, "", page.Subject("Proposals")+page.Label(fmt.Sprintf(" · %d of %d, the cap is %d", len(run.Proposals), proposals, learn.ProposalCap)))
	lines = append(lines, proposalLines(page, run.Proposals)...)
	if len(run.Held) > 0 {
		lines = append(lines, "", page.Subject("Held back by the cap")+page.Label(" · "+strconv.Itoa(len(run.Held))))
		lines = append(lines, proposalLines(page, run.Held)...)
	}
	for _, section := range []struct {
		name   string
		themes []learn.Theme
	}{{"Watching, one session so far", run.Watching}, {"Seen, about your project rather than tofu", run.Seen}} {
		lines = append(lines, "", page.Subject(section.name)+page.Label(" · "+strconv.Itoa(len(section.themes))))
		shown := section.themes
		if !opts.all {
			shown = shown[:min(len(shown), learn.ShownThemesInSection)]
		}
		for _, theme := range shown {
			lines = append(lines, "  "+theme.Label()+page.Label(fmt.Sprintf(" · %d times in %d sessions", len(theme.Quotes), theme.Places)), quoteLines(page, theme.Quotes[:1])[0])
		}
	}
	return append(lines, "", page.Hint("tofu learn show <n> for the evidence"), page.Hint("apply <n> · reject <n> --reason \"...\" · upstream <n>"), page.Label("  run kept at "+page.Path(file)))
}

func quoteLines(page cli.Page, quotes []learn.Quote) []string {
	var lines []string
	for _, q := range quotes {
		lines = append(lines, fmt.Sprintf("      %s %-17s %s", q.At.Local().Format(timeOfDay), q.Session, page.Label(strconv.Quote(clip(q.Text, learn.ShownQuoteRunes)))))
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

func proposalLines(page cli.Page, proposals []learn.Proposal) []string {
	var lines []string
	for _, p := range proposals {
		where := string(p.Target)
		if p.Scope != "" {
			where += ", " + string(p.Scope)
		}
		act := "tofu learn apply " + strconv.Itoa(p.ID)
		if p.Target == learn.TargetUpstream {
			where, act = p.Mechanism, "tofu learn upstream "+strconv.Itoa(p.ID)
		}
		lines = append(lines, fmt.Sprintf("  %-2d %s · %s", p.ID, p.Bucket, where), "     "+clip(p.Text, learn.ShownTextRunes), "     "+page.Hint(act))
	}
	return lines
}

func showLines(page cli.Page, run learn.Run, p learn.Proposal) []string {
	lines := []string{page.Subject("Proposal "+strconv.Itoa(p.ID)) + page.Label(" · "+string(p.Bucket)+" · "+string(p.Target)+" · "+strconv.Itoa(p.Places)+" sessions · run "+run.ID), "  " + p.Text}
	if p.Said != "" {
		lines = append(lines, "  "+page.Label("your words: "+strconv.Quote(p.Said)+"  "+p.Session))
	}
	if p.Retire != "" {
		lines = append(lines, "  "+page.Label("retires memory "+p.Retire+" in the "+string(p.Scope)+" scope"))
	}
	for _, theme := range p.Themes {
		lines = append(lines, "", page.Subject(theme.Label()))
		for _, q := range theme.Quotes {
			lines = append(lines, fmt.Sprintf("  %s %-18s %s", q.At.Local().Format(dayAndTime), q.Session, strconv.Quote(oneLine(q.Text))))
			at := slices.IndexFunc(run.Said, func(s learn.Said) bool { return s.At.Equal(q.At) && s.Session == q.Session })
			if at >= 0 && at < len(run.Labels) {
				label := run.Labels[at]
				lines = append(lines, page.Label(fmt.Sprintf("    jev, uncalibrated: %v %v %s", label.Answers, label.Chosen, label.Failure)))
			}
		}
		for _, c := range theme.Checks {
			held := page.Glyph(cli.Fail) + " absent"
			if c.Present {
				held = page.Glyph(cli.Done) + " present"
			}
			lines = append(lines, fmt.Sprintf("  %s when it recurred at %s in %s, request %s", held, c.At.Local().Format(timeOfDay), c.Session, c.Request))
		}
	}
	return lines
}
