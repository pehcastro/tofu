package learn

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

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	llm "tofu/internal/models/llm/core"
	platformbuild "tofu/internal/platform/build"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
	"tofu/internal/store/learn"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/ui/tui/frame/frame"
	"tofu/library/changelog"
)

const (
	Usage       = `tofu learn scan [--chain <session or family>|--last N|--global] [--local] [--all] [--dir project] [--json], tofu learn show|upstream <n>, tofu learn apply <n> [--scope user-local|project-local|project-global|user-global] [--dir project], tofu learn reject <n> --reason "<why>", tofu learn upstream --list`
	learnRuleID = "learned_"
	timeOfDay   = "15:04"
	dayAndTime  = "2006-01-02 15:04"
)

type Run func(args []string, out, errOut io.Writer) int

type Deps struct {
	SetSetting Run
	AddRule    Run
}

type learnOpts struct {
	chain, dir, reason       string
	scope                    entries.Scope
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
			scope, err := entries.ParseScope(args[i])
			if err != nil {
				return learnOpts{}, err
			}
			opts.scope = scope
		case "--list":
			opts.list = true
		case output.JSONFlag:
		default:
			if strings.HasPrefix(arg, "-") {
				return learnOpts{}, fmt.Errorf("unknown argument %q", arg)
			}
			opts.rest = append(opts.rest, arg)
		}
	}
	return opts, nil
}

func Verb(deps Deps) func(ctx context.Context, args []string, out, errOut io.Writer) int {
	return func(ctx context.Context, args []string, out, errOut io.Writer) int {
		if len(args) == 0 {
			args = []string{"scan"}
		}
		o := output.Verb{Name: "learn " + args[0], UsageLine: Usage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
		opts, err := parseLearnArgs(args[1:])
		if !slices.Contains([]string{"scan", "show", "apply", "reject", "upstream"}, args[0]) {
			err = fmt.Errorf("no learn verb %q", args[0])
		}
		if err != nil {
			return o.Usage(err)
		}
		home, err := learn.OpenHome()
		if err != nil {
			return o.Fail(err)
		}
		if args[0] == "scan" {
			return learnScan(ctx, o, opts, home)
		}
		if args[0] == "upstream" && opts.list {
			return learnDrafts(o, home)
		}
		run, err := home.Last()
		if err != nil {
			return o.Fail(err)
		}
		id := 0
		if len(opts.rest) == 1 {
			id, err = strconv.Atoi(opts.rest[0])
		}
		finding, found := run.Find(id)
		if err != nil || !found {
			return o.Usage(fmt.Errorf("no finding %q in the last run, %s", strings.Join(opts.rest, " "), run.ID))
		}
		decide := func(action learn.Action) error {
			return home.Decide(learn.Decision{At: time.Now(), Run: run.ID, ID: finding.ID, Key: finding.Key, Action: action, Places: finding.Sessions, Reason: opts.reason})
		}
		switch args[0] {
		case "show":
			return o.Done(true, finding, func(page cli.Page) []string { return showLines(page, run, finding) })
		case "reject":
			if opts.reason == "" {
				return o.Usage(errors.New(`a rejection says why: --reason "<why>"`))
			}
			if err := decide(learn.ActionReject); err != nil {
				return o.Fail(err)
			}
			return o.Done(true, finding, func(page cli.Page) []string {
				return []string{page.Glyph(cli.Removed) + " rejected " + strconv.Itoa(finding.ID) + page.Label(" · quiet until more sessions than "+strconv.Itoa(finding.Sessions)+" support it")}
			})
		case "upstream":
			if finding.Target() != learn.TargetUpstream {
				return o.Usage(fmt.Errorf("finding %d changes something of yours; tofu learn apply %d", finding.ID, finding.ID))
			}
			return learnDraft(o, home, finding, decide)
		}
		return learnApply(ctx, o, deps, opts, home, finding, decide)
	}
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
		return []learn.Source{{Store: store, Headers: headers}}, output.Plural(len(headers), "session") + " of the last " + strconv.Itoa(opts.last) + " conversations of this project", err
	}
	home, err := fs.HomeConfigDir()
	if err != nil {
		return nil, "", err
	}
	states, err := project.ProjectGlob(filepath.Join(home, fs.ProjectsDirName), "*")
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

func learnScan(ctx context.Context, o output.Verb, opts learnOpts, home learn.Home) int {
	sources, scope, err := learnSources(opts)
	if err != nil {
		return o.Fail(err)
	}
	shelves, err := entries.OpenIn(ctx, opts.dir)
	if err != nil {
		return o.Fail(err)
	}
	decisions, err := home.Decisions()
	if err != nil {
		return o.Fail(err)
	}
	known := learn.Known{Memory: shelves.All(), Decisions: decisions, Releases: learn.Releases(changelog.Markdown), Settings: map[string]string{}}
	for _, spec := range settingspkg.Default() {
		known.Settings[spec.Key] = spec.Description
	}
	run, err := learn.Scan(sources, known, time.Now())
	if err != nil {
		return o.Fail(err)
	}
	run.Scope, run.Sent.Local = scope, opts.local || !learnOn(opts.dir)
	if !run.Sent.Local && len(run.Said) > 0 {
		if err := labelWindows(ctx, &run, o.ErrOut); err != nil {
			return o.Fail(err)
		}
		groupByMeaning(ctx, &run, known, opts.dir, o.ErrOut)
	}
	file, err := home.Save(run)
	if err != nil {
		return o.Fail(err)
	}
	return o.Done(true, run, func(page cli.Page) []string { return scanLines(page, run, opts, file) })
}

func learnOn(dir string) bool {
	store, err := settingspkg.OpenFor(dir)
	return err == nil && store.Bool(settingspkg.Learn)
}

func labelWindows(ctx context.Context, run *learn.Run, errOut io.Writer) error {
	set, err := learn.Questions()
	if err != nil {
		return err
	}
	asked := wiring.Battery{SetName: set.Name, QuestionsVersion: set.QuestionsVersion, Kinds: map[string]questions.Kind{}}
	for _, q := range set.Questions {
		asked.Questions, asked.Kinds[q.Name] = append(asked.Questions, q.ToJev()), q.Kind
	}
	key, err := wiring.GateKey()
	if err != nil {
		return err
	}
	client, err := wiring.JevClientOn(key, 1)
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
		decision, err := client.Ask(ctx, core.Request{State: state, Questions: asked.Questions})
		if err == nil {
			var row ledger.Row
			row, err = wiring.AppendRow(state, asked, wiring.RowInput{Decision: &decision, Answers: wiring.ToLedgerAnswers(set.QuestionsVersion, decision.Answers), StateBuilder: set.Name + "@" + strconv.Itoa(set.QuestionsVersion)})
			label.Row, label.Build, label.Cost = row.ID, decision.Build, decision.Usage.Cost
			label.Answers, label.Chosen = map[string]float64{}, map[string]string{}
			for name, answer := range decision.Answers {
				if answer.Kind == core.QuestionChoice {
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

func groupByMeaning(ctx context.Context, run *learn.Run, known learn.Known, dir string, errOut io.Writer) {
	opts := wiring.OnTheBoundWire(wiring.RunOpts{Dir: dir, Effort: llm.EffortDefault})
	if opts.Wire == wiring.WireKey {
		run.Sent.Kept = "the lead model is reached through the OpenRouter key, which learning never spends"
		return
	}
	selected, err := wiring.ChooseModel(opts)
	if err == nil {
		prompt := run.Prompt(known)
		run.Sent.GroupBytes = len(prompt)
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, []string{page.Label(fmt.Sprintf("grouping %d messages by meaning with %s on your subscription, one request, %d bytes", len(run.Said), selected.Slug(), len(prompt)))})
		var decision llm.Decision
		decision, err = wiring.SubscriptionModel{Opts: opts}.Ask(ctx, llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}}})
		run.Sent.GroupTokens = decision.Usage.InputTokens + decision.Usage.OutputTokens
		if err == nil {
			err = run.Group(cmp.Or(decision.Content, decision.Refusal), known, selected.Slug())
		}
	}
	if err != nil {
		run.Sent.Kept = err.Error()
	}
}

func learnApply(ctx context.Context, o output.Verb, deps Deps, opts learnOpts, home learn.Home, finding learn.Finding, decide func(learn.Action) error) int {
	switch finding.Target() {
	case learn.TargetUpstream:
		return learnDraft(o, home, finding, decide)
	case learn.TargetNone:
		return o.Usage(fmt.Errorf("finding %d is about your project, not tofu; there is nothing to apply", finding.ID))
	case learn.TargetSetting:
		if code := deps.SetSetting([]string{"set", "--scope", "project", finding.Setting, finding.Value}, o.Out, o.ErrOut); code != output.ExitOK {
			return code
		}
		if err := decide(learn.ActionApply); err != nil {
			return o.Fail(err)
		}
		return output.ExitOK
	}
	if finding.Rule == "" {
		return o.Usage(fmt.Errorf("finding %d has no rule worded yet; with tofu settings set learn true a scan words it with your lead model, or tofu memory add \"<rule>\" keeps your own", finding.ID))
	}
	if finding.Target() == learn.TargetRule {
		flag := "--project"
		if finding.Scope == entries.Global {
			flag = "--global"
		}
		if code := deps.AddRule([]string{flag, "--dir", opts.dir, learnRuleID + finding.Key, finding.Rule}, o.Out, o.ErrOut); code != output.ExitOK {
			return code
		}
		if finding.Retire == "" {
			if err := decide(learn.ActionApply); err != nil {
				return o.Fail(err)
			}
			return output.ExitOK
		}
		shelves, err := entries.OpenIn(ctx, opts.dir)
		if err == nil {
			_, err = shelves.Remove(finding.Scope, finding.Retire)
		}
		if err = errors.Join(err, decide(learn.ActionApply)); err != nil {
			return o.Fail(err)
		}
		return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeRemoved, What: "memory " + finding.Retire + ", now the rule " + learnRuleID + finding.Key}},
			Undo: "tofu memory add --scope " + string(finding.Scope) + " --said " + strconv.Quote(finding.Said) + " " + strconv.Quote(finding.Rule)})
	}
	shelves, err := entries.OpenIn(ctx, opts.dir)
	if err != nil {
		return o.Fail(err)
	}
	added, err := shelves.Add(entries.Entry{Scope: cmp.Or(opts.scope, finding.Scope), Kind: entries.KindPerson, Text: finding.Rule, Said: finding.Said, Session: finding.Session, At: time.Now(), By: entries.ByOffer}, "")
	if err = errors.Join(err, decide(learn.ActionApply)); err != nil {
		return o.Fail(err)
	}
	for _, notice := range shelves.Notices {
		_, _ = fmt.Fprintln(o.ErrOut, notice)
	}
	undo := added.Undo()
	if opts.dir != "." {
		undo += " --dir " + strconv.Quote(opts.dir)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeAdded, What: "memory " + added.ID + " · " + string(added.Scope), File: added.File}}, Undo: undo})
}

func learnDraft(o output.Verb, home learn.Home, finding learn.Finding, decide func(learn.Action) error) int {
	build := learn.Build{Version: frame.Release(platformbuild.Version(), platformbuild.BuildRevision()), Commit: platformbuild.BuildRevision(), Platform: fs.OS() + "/" + fs.Arch()}
	file, text, err := home.WriteDraft(learn.DraftOf(finding, build, time.Now()))
	if err = errors.Join(err, decide(learn.ActionDraft)); err != nil {
		return o.Fail(err)
	}
	if !o.AsJSON {
		_, _ = fmt.Fprintln(o.Out, text)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeAdded, What: "upstream draft " + finding.Key + " · written, not sent", File: file}}, Undo: "delete " + file})
}

func learnDrafts(o output.Verb, home learn.Home) int {
	listed, err := home.Drafts()
	if err != nil {
		return o.Fail(err)
	}
	return o.Done(true, listed, func(page cli.Page) []string {
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
