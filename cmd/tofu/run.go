package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/browser/jevloop"
	"tofu/internal/cron"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/llm/wire/openrouter"
	"tofu/internal/recall"
	"tofu/internal/recipe"
	"tofu/internal/rule"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
	"tofu/internal/web"
	shipped "tofu/library"
	"tofu/library/questions"
)

const (
	wireSubscription = "anthropic"
	wireCodex        = "codex"
	wireKey          = openrouter.Name
	wireMeta         = string(models.Meta)

	toolSetFull         = "full"
	toolSetThree        = "three"
	toolPickRule        = "tool_pick"
	verifySubAgentsRule = "verify_sub_agents"

	gateFollowsTheRule = ""
	gateOff            = "off"
	gateShadow         = "shadow"
	gateEnforce        = "enforce"

	openRouterDefaultModel = "anthropic/claude-opus-5"
)

func runWires() []string { return []string{wireSubscription, wireCodex, wireKey} }

func wireEfforts(wire string) []llm.Effort {
	switch wire {
	case wireSubscription:
		return anthropic.ReasoningEfforts()
	case wireCodex:
		return llm.Efforts()
	}
	return nil
}

func wireSpend(wire string) turn.Spend {
	if wire == wireKey || wire == wireMeta {
		return turn.SpendAPIKey
	}
	return turn.SpendSubscription
}

type runOpts struct {
	dir              string
	task             string
	turnID           string
	session          string
	wire             string
	dryRun           bool
	showPrompt       bool
	agent            string
	gateArm          string
	noSubAgents      bool
	readBeforeEdit   bool
	doneArm          string
	model            string
	toolSet          string
	effort           llm.Effort
	maxSteps         int
	loopGuardRepeats int
	loopGuardWindow  int
	contextCeiling   int
	siftArm          string
	noInstructions   bool
	noDocs           bool
	subAgentList     string
	shell            turn.RunShell
}

type runtime struct {
	model        turn.Model
	accounts     turn.Accounts
	spend        turn.Spend
	budget       recall.Budget
	gate         *toolGate
	sift         *turn.ShellSift
	scorer       *shellScorer
	sessions     *session.Store
	notify       func(string)
	roster       *subagent.Roster
	inbox        *turn.Inbox
	now          func() time.Time
	open         func(runOpts) (appWire, error)
	wrapSubAgent func(turn.Model) (turn.Model, error)
	orchestrator models.Model
	tabs         *tools.BrowserTabs
	leadAsks     turn.Person
	cron         *cron.Book

	omitThinkingSummary bool
}

func (r runtime) leadTools(lead []turn.Tool) turn.Registry {
	if r.cron != nil {
		lead = append(slices.Clone(lead), tools.Cron{Book: r.cron, Now: r.now, Told: r.notify})
	}
	return turn.NewRegistry(lead...)
}

type summaryOmitted struct{ inner turn.Model }

func (s summaryOmitted) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	request.OmitThinkingSummary = true
	return s.inner.Ask(ctx, request)
}

func (r runtime) applyThinkingSummary(accounts turn.Accounts) turn.Accounts {
	if !r.omitThinkingSummary || accounts.Pick == nil || accounts.Next == nil {
		return accounts
	}
	omitted := func(account turn.Account) turn.Account {
		if account.Model != nil {
			account.Model = summaryOmitted{account.Model}
		}
		return account
	}
	return turn.Accounts{
		Pick: func(ctx context.Context) (turn.Account, error) {
			account, err := accounts.Pick(ctx)
			return omitted(account), err
		},
		Next: func(ctx context.Context, pinned turn.Account) (turn.Account, bool, error) {
			account, moved, err := accounts.Next(ctx, pinned)
			return omitted(account), moved, err
		},
	}
}

func boundRoles(wire, dir string) (models.Bindings, error) {
	library, err := modelLibrary(dir)
	if err != nil {
		return nil, err
	}
	spec, carried := library.ForWire(wire)
	if !carried {
		return nil, outsideTheLibrary(library, wire)
	}
	return library.Bind(spec.ID)
}

func chooseModel(opts runOpts) (models.Model, error) {
	if opts.wire == wireKey {
		return models.Model{ID: cmp.Or(opts.model, openRouterDefaultModel)}, nil
	}
	if opts.wire == wireMeta {
		library, err := modelLibrary(opts.dir)
		if err != nil {
			return models.Model{}, err
		}
		if opts.model == "" {
			return library.KeyDefault(models.Meta)
		}
		return library.Select(opts.model)
	}
	if opts.model != "" {
		return selectModel(opts.wire, opts.model)
	}
	bound, err := boundRoles(opts.wire, opts.dir)
	if err != nil {
		return models.Model{}, err
	}
	orchestrator := bound[models.RoleOrchestrator]
	if orchestrator.Wire != opts.wire {
		return models.Model{}, fmt.Errorf(
			"%s, and --wire %s reaches another subscription: run --wire %s instead, or bind the orchestrator role to a model --wire %s serves",
			orchestrator.Says(), opts.wire, orchestrator.Wire, opts.wire)
	}
	return orchestrator.Model, nil
}

func onTheBoundKeyWire(opts runOpts) runOpts {
	if opts.model != "" || wireSpend(opts.wire) == turn.SpendAPIKey {
		return opts
	}
	bound, err := boundRoles(opts.wire, opts.dir)
	if orchestrator := bound[models.RoleOrchestrator]; err == nil && orchestrator.Wire == wireMeta {
		opts.wire, opts.model = wireMeta, orchestrator.Model.Slug()
	}
	return opts
}

func boundSubAgent(opts runOpts) (string, error) {
	if wireSpend(opts.wire) == turn.SpendAPIKey {
		return "", nil
	}
	bound, err := boundRoles(opts.wire, opts.dir)
	if err != nil || bound[models.RoleSubAgent].By != models.BoundByFile {
		return "", err
	}
	return bound[models.RoleSubAgent].Model.Slug(), nil
}

func (r runtime) subAgentOpener(opts runOpts) func(subagent.Definition, llm.Effort) (turn.SubAgentModel, error) {
	if r.open == nil {
		return nil
	}
	var told sync.Map
	return func(definition subagent.Definition, wanted llm.Effort) (turn.SubAgentModel, error) {
		subAgent := opts
		named := definition.Model
		if definition.Name == "" {
			bound, err := boundSubAgent(opts)
			if err != nil {
				return turn.SubAgentModel{}, err
			}
			named = bound
		}
		asked := cmp.Or(named, r.orchestrator.Slug())
		offered := r.orchestrator.Efforts
		if named != "" {
			library, err := modelLibrary(opts.dir)
			if err != nil {
				return turn.SubAgentModel{}, err
			}
			model, err := library.Select(named)
			if err != nil {
				return turn.SubAgentModel{}, err
			}
			subAgent.model, subAgent.wire, offered = named, library.WireOf(model), model.Efforts
		}
		if subAgent.wire == wireKey {
			offered = nil
		}
		switch {
		case wanted != "" && len(offered) == 0:
			return turn.SubAgentModel{}, fmt.Errorf("effort %s for %s: it sends no reasoning effort, so the level would be dropped without a word", wanted, asked)
		case wanted != "" && !slices.Contains(offered, wanted):
			return turn.SubAgentModel{}, fmt.Errorf("effort %s for %s: it takes %s, so the level would be changed without a word", wanted, asked, llm.EffortList(offered))
		}
		subAgent.effort = cmp.Or(wanted, definition.Effort, opts.effort)
		if wanted == "" && definition.Effort != "" && !slices.Contains(offered, definition.Effort) {
			subAgent.effort = ""
			if len(offered) > 0 {
				subAgent.effort = defaultEffort(offered)
			}
			if _, said := told.LoadOrStore(definition.Name, true); !said && r.notify != nil {
				r.notify(fmt.Sprintf("%s asks for effort %s, which %s does not take, so it runs at %s", definition.Name, definition.Effort, asked, cmp.Or(string(subAgent.effort), "no effort")))
			}
		}
		if named == "" && subAgent.effort == opts.effort {
			return turn.SubAgentModel{Slug: asked, Windows: r.orchestrator.WindowText(), Effort: subAgent.effort}, nil
		}
		opened, err := r.open(subAgent)
		if err != nil {
			if opened.held != nil {
				opened.held.close()
			}
			return turn.SubAgentModel{}, err
		}
		opened.held.wrap = r.wrapSubAgent
		return turn.SubAgentModel{Slug: asked, Windows: opened.selected.WindowText(), Wire: subAgent.wire, Effort: subAgent.effort, Spend: opened.spend, Accounts: r.applyThinkingSummary(opened.held.forTurn()), Close: opened.held.close}, nil
	}
}

func runUsage() string {
	return fmt.Sprintf(runUsageText,
		llm.EffortList(llm.Efforts()), llm.EffortDefault,
		llm.EffortList(anthropic.ReasoningEfforts()), shellSiftCost(), turn.InstructionsOff)
}

const runUsageText = `tofu run works a task in a directory until it is done.

Usage:
  tofu run --dir <path> [arguments] <task>

Arguments:
  --dir <path>          the directory the task is worked in, required
  --model <id>          the orchestrator's model, otherwise the one the orchestrator role is bound to;
                        a meta/ model is paid by the key tofu login meta stores and picks its own wire
  --wire <name>         anthropic, codex or openrouter
  --tools <set>         full, or three for the read, write and bash arm
  --effort <level>      how hard the model thinks: %s.
                        the default is %s. the anthropic wire takes %s,
                        and refuses the rest rather than picking a neighbour.
                        the openrouter wire sends no level and takes no --effort
  --gate <arm>          off, shadow or enforce, otherwise the rule's own mode decides
  --no-gate             the arm that turns the tool gate off
  --sift <arm>          free or judged, otherwise the method table decides which
                        one cuts a bash result before the model reads it. free
                        keeps an error-shaped line and the ends of the output,
                        drops the rest, costs nothing and makes no call. judged
                        costs, from library/decisions/methods@1.yaml:
                        %s
  --no-subagents        run without the spawn tool for this run, no matter what
                        the turnMaySpawn setting says; there is no flag that
                        turns spawning on when that setting says off
                        the readBeforeEdit setting, on by default, refuses an
                        edit or a write to a file this session has not read;
                        there is no flag for it, set it with
                        tofu settings set readBeforeEdit false
  --no-instructions     the arm that %s
  --no-docs             the arm that drops the tofu_docs tool and the prompt sentence pointing at it
  --done-review <arm>          the arm that reviews a sub-agent's answer
  --max-steps <n>              cap the steps a turn takes, unset means no cap
  --loop-guard-repeats <n>     how many repeats of one call with one result stops a turn
  --loop-guard-window <n>      how many recent calls the loop guard remembers
  --dry-run                    print the request that would be sent and send nothing
  --show-prompt                print the prompt the turn composes, part by part, and send nothing
  --agent <name>               with --show-prompt, print the prompt a spawn of that sub-agent composes for the task

TOFU_DRIVE_CASSETTE names a recorded model, read as tofu drive reads it, and
then the run opens no live wire for the orchestrator or for any sub-agent.
`

func runVerb(args []string, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, runUsage())
		return exitOK
	}
	opts, err := parseRunArgs(args)
	if err != nil {
		return runFail(errOut, err)
	}
	opts = onTheBoundKeyWire(opts)
	dir := cmp.Or(opts.dir, ".")
	say := func(unreadable string) { _, _ = fmt.Fprintln(errOut, "tofu run: "+unreadable) }
	if opts.maxSteps == 0 {
		opts.maxSteps = settingInt(dir, settingspkg.DecisionCap, say)
	}
	if !opts.noSubAgents {
		if maySpawn := settingInt(dir, settingspkg.TurnMaySpawn, say); maySpawn == 0 {
			opts.noSubAgents = true
			if opts.doneArm != doneArmOff {
				return runFail(errOut, fmt.Errorf(
					"--done-review %s with the %s setting off: that arm has no spawn tool, so no sub-agent is ever reviewed and the flag would say a check is running that is not",
					opts.doneArm, settingspkg.TurnMaySpawn))
			}
		}
	}
	opts.readBeforeEdit = settingInt(dir, settingspkg.ReadBeforeEdit, say) != 0
	if opts.showPrompt {
		return showPrompt(opts, out, errOut)
	}

	selected, err := chooseModel(opts)
	if err != nil {
		return runFail(errOut, err)
	}

	shell, err := turn.ResolveRunShell(settingText(dir, settingspkg.Shell, say))
	if err != nil {
		return runFail(errOut, err)
	}
	opts.shell = shell

	home, _ := os.UserHomeDir()
	warm := newWarmProcesses(tools.NewBrowserTabs(home))
	defer warm.Close()
	built, _, err := buildRunToolsForRun(opts.dir, opts.toolSet, readsWhen(opts.readBeforeEdit, turn.NewReadLedger()), warm, shell)
	if err != nil {
		return runFail(errOut, err)
	}
	budget, err := contextBudget(opts, selected)
	if err != nil {
		return runFail(errOut, err)
	}

	if opts.dryRun {
		opts.effort = selected.EffortTaken(opts.effort)
		config, _, err := runConfig(opts, built, runtime{spend: turn.SpendSubscription, budget: budget, notify: writeNotice(errOut), open: openAppWire, orchestrator: selected})
		if err != nil {
			return runFail(errOut, err)
		}
		body, err := dryRunBody(opts, selected.ID, config)
		if err != nil {
			return runFail(errOut, err)
		}
		_, _ = fmt.Fprintln(errOut, "context budget "+budget.Record())
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}
	return runTurn(opts, selected, built, budget, warm.tabs, out, errOut)
}

func contextBudget(opts runOpts, model models.Model) (recall.Budget, error) {
	registry, err := modelRegistry()
	if err != nil {
		return recall.Budget{}, err
	}
	windowTokens, windowSource := models.WindowFor(model, registry, models.Served{})
	budget, err := recall.BudgetFor(model.ID, windowTokens)
	if err != nil {
		return recall.Budget{}, err
	}
	budget.WindowSource = windowSource
	if opts.contextCeiling <= 0 {
		return budget, nil
	}
	return budget.At(opts.contextCeiling, fmt.Sprintf("--context-ceiling %d, which is read before %s and wins when both are set",
		opts.contextCeiling, recall.CeilingVariable)), nil
}

type windowGuard struct {
	inner  turn.Model
	budget recall.Budget
	cfg    recall.Config
}

func (g windowGuard) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	tokens := 0
	for _, message := range request.Messages {
		tokens += g.cfg.MessageTokens(message.Content)
	}
	if err := g.budget.RefuseOverWindow(tokens); err != nil {
		return llm.Decision{}, err
	}
	return g.inner.Ask(ctx, request)
}

func guarded(model turn.Model, budget recall.Budget) (turn.Model, error) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		return nil, err
	}
	return windowGuard{inner: model, budget: budget, cfg: cfg}, nil
}

func runTurn(opts runOpts, selected models.Model, built []turn.Tool, budget recall.Budget, tabs *tools.BrowserTabs, out, errOut io.Writer) int {
	open := openAppWire
	deck, err := readCassette(os.Getenv(cassetteVariable))
	if err != nil {
		return runFail(errOut, err)
	}
	if deck != nil {
		open = driveWire(deck)
	}
	opened, err := open(opts)
	if opened.held != nil {
		defer opened.held.close()
	}
	if err != nil {
		return runFail(errOut, err)
	}
	wrap := func(model turn.Model) (turn.Model, error) { return guarded(model, budget) }
	opened.held.wrap = wrap

	var gate *toolGate
	if opts.gateArm != gateOff {
		if gate, err = newToolGate(opts.dir); err != nil {
			return runFail(errOut, err)
		}
	}

	sifter, scorer, err := buildShellSift(opts.siftArm)
	if err != nil {
		var contradiction turn.RuleContradictsTheTableError
		if !errors.As(err, &contradiction) {
			return runFail(errOut, err)
		}
		_, _ = fmt.Fprintln(errOut, "tofu run: no shell result is cut: "+err.Error())
	}

	sessions, err := session.OpenIn(cmp.Or(opts.dir, "."))
	if err != nil {
		return runFail(errOut, err)
	}

	config, spawner, err := runConfig(opts, built, runtime{accounts: opened.held.forTurn(), spend: opened.spend, budget: budget, gate: gate, sift: sifter, scorer: scorer, sessions: sessions, notify: writeNotice(errOut),
		open: open, wrapSubAgent: wrap, orchestrator: selected, tabs: tabs})
	if err != nil {
		return runFail(errOut, err)
	}
	if spawner != nil {
		review, reviewErr := newDoneReview(opts.doneArm)
		if reviewErr != nil {
			return runFail(errOut, reviewErr)
		}
		spawner.Review = review
	}
	_, _ = fmt.Fprintln(out, "context budget "+budget.Record())
	registry, registryErr := launchShellRegistry(opts.dir)
	if registryErr == nil {
		_ = registry.Prune()
	}
	head := ""
	interrupted, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	context.AfterFunc(interrupted, stop)
	runErr := turn.Lead(turn.WithShellRegistry(interrupted, registry), config, nil, nil, func(row turn.Row, _ error) {
		printRunRow(out, row, selected.Slug(), selected.WindowText())
		head = cmp.Or(row.Session, head)
	})
	for _, warning := range turn.EndSession(context.Background(), cmp.Or(opts.dir, "."), head, sessionEndOther) {
		_, _ = fmt.Fprintln(errOut, "tofu run: "+warning)
	}
	leaveShells(registry)
	for _, subAgent := range subAgentRows(spawner) {
		askedAs, windows := askedAsOf(subAgent, spawner.Spawned(), selected.Slug(), selected.WindowText())
		printRunRow(out, subAgent, askedAs, windows)
	}
	if head != "" {
		if headErr := sessions.SetHead(head); headErr != nil {
			_, _ = fmt.Fprintf(errOut, "tofu run: pointing the head at %s: %v\n", head, headErr)
		}
	}
	if gate != nil {
		_, _ = fmt.Fprintf(out, "gate decisions %d cost $%.6f mode %s\n", gate.decisions, gate.costUSD, config.GateMode)
	}
	if scorer != nil {
		calls, cost := scorer.spend()
		_, _ = fmt.Fprintf(out, "sift decisions %d cost $%.6f\n", calls, cost)
	}
	if runErr != nil {
		return runFail(errOut, runErr)
	}
	return exitOK
}

func runEnvironment(opts runOpts) (environment, instructions, notice string) {
	if opts.shell.Resolved() {
		environment = turn.EnvironmentFromShell(opts.dir, time.Now(), opts.shell)
	} else {
		environment = turn.Environment(opts.dir, time.Now())
	}
	if opts.noInstructions {
		return environment, "off by request, and " + turn.InstructionsOff, ""
	}
	home, _ := os.UserHomeDir()
	setting, unreadable := appSetting(opts.dir, settingspkg.ProjectInstructionsCap)
	capBytes := turn.InstructionCap(setting)
	written, cut, skipped := turn.ProjectInstructionsInOrder(opts.dir, home, capBytes, settingText(opts.dir, settingspkg.InstructionSources, nil))
	instructions = "on, and neither an AGENTS.md nor a CLAUDE.md was found to send"
	if written != "" {
		instructions = fmt.Sprintf("on, %d bytes", len(written))
		environment += "\n\n" + written
	}
	var notices []string
	if unreadable != "" {
		notices = append(notices, unreadable)
	}
	if len(cut) > 0 {
		notices = append(notices, fmt.Sprintf("your instructions were cut at %d bytes and %s never reached the model: raise the cap with tofu settings set %s <bytes>",
			capBytes, strings.Join(cut, ", "), settingspkg.ProjectInstructionsCap))
	}
	if len(skipped) > 0 {
		notices = append(notices, fmt.Sprintf("skipped %s: choose with tofu settings set %s %s, %s or %s", strings.Join(skipped, "; "), settingspkg.InstructionSources,
			settingspkg.InstructionsAgentsFirst, settingspkg.InstructionsClaudeFirst, settingspkg.InstructionsBoth))
	}
	return environment, instructions, strings.Join(notices, "; ")
}

func writeNotice(w io.Writer) func(string) {
	return func(notice string) { _, _ = fmt.Fprintln(w, "tofu: "+notice) }
}

type composedRun struct {
	opts         runOpts
	environment  string
	instructions string
	composed     turn.Composed
	subAgents    turn.SubAgents
	skills       []skill.Skill
	checksWork   bool
}

func composeRun(opts runOpts, built []turn.Tool, run runtime) (composedRun, error) {
	found := make(chan subagent.Found, 1)
	if !opts.noSubAgents && opts.toolSet != toolSetThree && run.open != nil {
		go func() { found <- offeredToSpawn(cmp.Or(opts.dir, "."), scanSubAgents(opts.dir, built)) }()
	} else {
		found <- subagent.Found{}
	}
	say := func(notice string) {
		if notice != "" && run.notify != nil {
			run.notify(notice)
		}
	}
	environment, instructions, notice := runEnvironment(opts)
	say(notice)
	discovered := <-found
	opts.subAgentList = turn.SubAgentList(discovered.Definitions)
	for _, broken := range discovered.Broken {
		say("the sub-agent in " + broken.Path + " is not offered: " + broken.Reason)
	}
	stack, err := stackRules("", cmp.Or(opts.dir, "."))
	if err != nil {
		return composedRun{}, err
	}
	rules := stack.rules
	var switchedOff []rule.Overriding
	for _, applied := range stack.overrides {
		if applied.Rule.Mode == rule.ModeOff && !applied.Stale && !slices.ContainsFunc(rules, func(r rule.Rule) bool { return r.ID == applied.Base.ID }) {
			switchedOff = append(switchedOff, applied.Overriding)
		}
	}
	if opts.toolSet == toolSetThree {
		rules = slices.DeleteFunc(rules, func(loaded rule.Rule) bool { return loaded.ID == toolPickRule })
	}
	var skills []skill.Skill
	if opts.toolSet != toolSetThree && settingText(cmp.Or(opts.dir, "."), settingspkg.Skills, run.notify) != settingspkg.SkillsOff {
		home, _ := os.UserHomeDir()
		offered := skill.Discover(cmp.Or(opts.dir, "."), home)
		for _, warning := range offered.Warnings {
			say(warning)
		}
		skills = offered.Skills
	}
	frameworks, err := rule.Frameworks(cmp.Or(opts.dir, "."), turn.TaskNamed(opts.task).Paths)
	if err != nil {
		say("no framework rule fires this run: " + err.Error())
	}
	subAgents := turn.SubAgents{Defined: discovered.Definitions, Root: cmp.Or(opts.dir, "."), Prompt: turn.ComposeSpec{Environment: environment,
		ToolGuidance: turn.EveryToolIsRelativeToTheWorkingDirectory + turn.ContractAddendum, Rules: rules, SwitchedOff: switchedOff, Skills: skills, WindowTokens: run.budget.WindowTokens, Frameworks: frameworks}}
	spec := subAgents.Prompt
	spec.Task, spec.ToolGuidance = opts.task, runSystem(opts)
	if opts.agent != "" {
		if spec.Agent, err = subAgents.Named(opts.agent); err != nil {
			return composedRun{}, err
		}
		spec.Role, spec.ToolGuidance = rule.RoleSubAgent, subAgents.Prompt.ToolGuidance
	} else if !opts.noSubAgents && opts.toolSet != toolSetThree {
		spec.Role = rule.RoleOrchestrator
	}
	composed, err := turn.Compose(spec)
	return composedRun{opts: opts, environment: environment, instructions: instructions, composed: composed, subAgents: subAgents, skills: skills,
		checksWork: slices.ContainsFunc(rules, func(loaded rule.Rule) bool { return loaded.ID == verifySubAgentsRule })}, err
}

func runConfig(opts runOpts, built []turn.Tool, run runtime) (turn.Config, *turn.SpawnTool, error) {
	orchestratorID, sessionID := cmp.Or(opts.turnID, turn.NewID(time.Now())), cmp.Or(opts.session, opts.turnID, session.NewEventID())
	if opts.noDocs {
		built = slices.DeleteFunc(slices.Clone(built), func(tool turn.Tool) bool { return tool.Name() == tools.DocsToolName })
	}
	if run.sessions != nil && opts.toolSet != toolSetThree {
		built = append(slices.Clone(built), tools.NewQuote(run.sessions, sessionID))
	}
	prompt, err := composeRun(opts, built, run)
	if err != nil {
		return turn.Config{}, nil, err
	}
	if len(prompt.skills) > 0 {
		built = append(slices.Clone(built), tools.NewSkill(prompt.skills))
	}
	opts, environment, composed := prompt.opts, prompt.environment, prompt.composed
	dir := cmp.Or(opts.dir, ".")
	run.omitThinkingSummary = settingText(dir, settingspkg.ThinkingSummary, run.notify) == settingspkg.ThinkingOmitted
	references := map[string]string{}
	for _, definition := range prompt.subAgents.Defined {
		for _, reference := range definition.References {
			references[reference.Name] = reference.Text
		}
	}
	config := turn.Config{
		References:    references,
		Model:         run.model,
		Now:           run.now,
		Accounts:      run.applyThinkingSummary(run.accounts),
		Spend:         run.spend,
		Tools:         turn.NewRegistry(built...),
		Project:       dir,
		SessionSource: sessionStartup,
		Task:          opts.task,
		Wire:          opts.wire,
		System:        composed.Head(),
		Environment:   composed.WithTaskRules(environment),
		Caps: turn.Caps{
			MaxSteps:         opts.maxSteps,
			LoopGuardRepeats: opts.loopGuardRepeats,
			LoopGuardWindow:  opts.loopGuardWindow,
		},
		ResultBytesCap: konst.TurnResultBytesCap,
		Budget:         run.budget,
		Sessions:       run.sessions,
		Session:        sessionID,
		Sift:           run.sift,
		Proxy:          loadProxySetting(opts.dir).proxy,
		Inbox:          cmp.Or(run.inbox, turn.NewInbox()),
		Notify:         run.notify,
	}
	if run.gate != nil {
		config.Gate = run.gate
		config.GateMode = gateMode(opts.gateArm, settingText(dir, settingspkg.GatePrompt, run.notify))
	}
	config.NewID = func() string { return orchestratorID }
	if run.scorer != nil {
		run.scorer.turnID = orchestratorID
	}
	if opts.toolSet == toolSetThree && run.leadAsks == nil {
		config.Tools = run.leadTools(built)
		return config, nil, nil
	}
	layers, err := userRuleLayers(dir)
	if err != nil {
		return turn.Config{}, nil, err
	}
	running := func() ([]rule.Rule, error) {
		rules, _, err := loadRules("", dir)
		return rules, err
	}
	override := tools.RuleOverride{Ask: run.leadAsks, Running: running, Global: layers[0].dir, Project: layers[1].dir}
	if opts.noSubAgents || opts.toolSet == toolSetThree {
		config.Tools = run.leadTools(append(slices.Clone(built), override))
		return config, nil, nil
	}
	spawner := turn.NewSpawnTool(orchestratorID, config, cmp.Or(run.roster, &subagent.Roster{}))
	spawner.Inbox, spawner.SubAgents, spawner.Project, spawner.ChecksWork = config.Inbox, prompt.subAgents, dir, prompt.checksWork
	spawner.SubAgents.Open = run.subAgentOpener(opts)
	learn := learnBrowserRecipe(run.notify)
	spawner.SubAgents.Brief = browserRecipeBrief(run.notify)
	spawner.SubAgents.Ended = func(definition subagent.Definition, task string, rounds []turn.Row, report turn.SubAgentReport, finished bool) {
		learn(definition, task, rounds, report, finished)
		run.tabs.CloseOpenedBy(report.ID)
	}
	spawner.Limits = func() turn.SubAgentLimits {
		return turn.SubAgentLimits{Running: settingInt(dir, settingspkg.SubAgentsPerTurn, run.notify), Depth: settingInt(dir, settingspkg.SubAgentDepth, run.notify),
			CheckIn: time.Duration(settingInt(dir, settingspkg.SubAgentCheckSeconds, run.notify)) * time.Second}
	}
	own := slices.DeleteFunc(slices.Clone(built), func(tool turn.Tool) bool { return slices.Contains(leadNeverCalls(), tool.Name()) })
	if settingText(dir, settingspkg.BrowserDriver, run.notify) == settingspkg.DriverSubagent {
		own = slices.DeleteFunc(own, func(tool turn.Tool) bool { return strings.HasPrefix(tool.Name(), "browser_") })
	}
	if !nodeProject(dir) {
		own = slices.DeleteFunc(own, func(tool turn.Tool) bool { return tool.Name() == "typecheck" || tool.Name() == "test" })
	}
	orchestrating := append(turn.WithSourceBudget(own, prompt.subAgents.Defined), spawner, override)
	if run.gate != nil {
		spawner.SettingsTool = true
		orchestrating = append(orchestrating, tools.NewSettings(settingsPaths(dir)))
	}
	config.Tools = run.leadTools(orchestrating)
	return config, spawner, nil
}

func nodeProject(dir string) bool {
	for _, pattern := range []string{"package.json", "tsconfig.json", "*/package.json", "*/tsconfig.json"} {
		if found, _ := filepath.Glob(filepath.Join(dir, pattern)); len(found) > 0 {
			return true
		}
	}
	return false
}

func leadNeverCalls() []string {
	return []string{"github_pr_diff", "tofu_rules_check", "tofu_judge", "tofu_why", "tofu_replay"}
}

func scanSubAgents(dir string, built []turn.Tool) subagent.Found {
	names := []string{(&turn.SpawnTool{}).Name()}
	for _, tool := range built {
		names = append(names, tool.Name())
	}
	known := slices.Clone(names)
	for _, driver := range []string{settingspkg.DriverSteps, settingspkg.DriverGoal} {
		every, _ := tools.NewBrowser(tools.BrowserSettings{Mode: settingspkg.BrowserDrive, Driver: driver})
		for _, tool := range every {
			known = append(known, tool.Name())
		}
	}
	dir = cmp.Or(dir, ".")
	catalog, _ := modelLibrary(dir)
	home, _ := os.UserHomeDir()
	library, _, _ := librarySource()
	sources, tiers := settingspkg.DeclaredDefaultText(settingspkg.AgentSources), map[subagent.Tier]string{}
	if store, err := openSettings(dir); err == nil {
		sources = store.Text(settingspkg.AgentSources)
		for _, tier := range subagent.Tiers() {
			tiers[tier] = strings.TrimSpace(store.Text(tier.Setting()))
		}
	}
	return onBrowserModel(dir, subagent.Definitions(subagent.Scan{
		Project:    dir,
		Home:       home,
		Sources:    strings.Split(sources, ","),
		Library:    library,
		Tools:      names,
		KnownTools: known,
		Catalog:    catalog,
		Tiers:      tiers,
	}), catalog)
}

func gateArms() []string { return []string{gateOff, gateShadow, gateEnforce} }

func gateMode(arm string, prompt string) turn.GateMode {
	switch arm {
	case gateEnforce:
		return turn.GateEnforce
	case gateShadow, gateOff:
		return turn.GateShadow
	case gateFollowsTheRule:
		return turn.GateModeFromPrompt(prompt)
	}
	panic("tofu run: unknown gate arm " + arm)
}

func dryRunBody(opts runOpts, model string, config turn.Config) ([]byte, error) {
	tools := config.Tools.Definitions()
	messages := []llm.Message{{Role: llm.RoleUser, Content: config.FirstUserMessage()}}
	switch opts.wire {
	case wireKey:
		system := append([]llm.Message{{Role: llm.RoleSystem, Content: config.System}}, messages...)
		return llm.Request{Messages: system, Tools: tools}.Encode(model)
	case wireCodex, wireMeta:
		return codex.Request{Model: model, Instructions: config.System, Messages: messages, Tools: tools, Effort: opts.effort}.Encode(nil)
	}
	versions, _ := subFingerprint(opts.dir)
	return anthropic.Request{Model: model, System: []string{config.System}, Messages: messages, Tools: tools, Effort: opts.effort, ClaudeCodeVersion: versions.ClaudeCode}.Encode(true)
}

func turnTransportConfig() transport.Config {
	return transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Retries:        konst.TurnRetries,
		Backoff:        time.Duration(konst.TurnBackoffMillis) * time.Millisecond,
		MaxBackoff:     time.Duration(konst.TurnMaxBackoffMillis) * time.Millisecond,
		Growth:         konst.TurnBackoffGrowth,
		JitterFraction: konst.TurnBackoffJitterFraction,
		TotalWait:      time.Duration(konst.TurnTotalBackoffCeilingMillis) * time.Millisecond,
		Concurrency:    1,
	}
}

func keyModel(model string) (turn.Model, error) {
	key, err := jev.Key(".env")
	if err != nil {
		return nil, err
	}
	prompting, err := cred.NewLLMKey(cred.OpenRouter, key)
	if err != nil {
		return nil, err
	}
	wire, err := openrouter.New(openrouter.Config{
		Model:     model,
		Key:       prompting.Prompt(),
		Transport: turnTransportConfig(),
	})
	if err != nil {
		return nil, err
	}
	return llm.NewClient(wire)
}

type codexTurn struct {
	wire   *codex.Wire
	effort llm.Effort
}

func (c codexTurn) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	var instructions []string
	messages := make([]llm.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == llm.RoleSystem {
			instructions = append(instructions, message.Content)
			continue
		}
		messages = append(messages, message)
	}

	result, _, err := c.wire.Ask(ctx, codex.Request{
		Instructions: strings.Join(instructions, "\n\n"),
		Messages:     messages,
		Tools:        request.Tools,
		ToolChoice:   request.ToolChoice.OpenAIValue(),
		Effort:       c.effort,
		OnThinking:   request.OnThinking,
		OnRetry:      request.OnRetry,
	})
	if err != nil {
		return llm.Decision{}, err
	}

	decision := llm.Decision{
		Build:            result.Model,
		RequestID:        result.ID,
		Outcome:          llm.OutcomeAfter(result.Stop, len(result.ToolCalls)),
		Stop:             result.StopReason,
		Content:          result.Content,
		Thinking:         llm.Thinking{Text: result.Thinking, Signature: codex.EncodeReasoning(result.ReasoningID, result.ReasoningEncrypted)},
		ToolCalls:        result.ToolCalls,
		Usage:            llm.Usage{InputTokens: result.Usage.Input, OutputTokens: result.Usage.Output, ReasoningTokens: result.Usage.Reasoning},
		PromptAccounting: llm.PromptAccountingFor(codex.Name),
		CacheReadTokens:  result.Usage.CacheRead,
		FirstTokenMS:     result.FirstTokenMS,
		Warnings:         result.Warnings,
	}
	if decision.Outcome == llm.OutcomeRefusal {
		decision.Refusal = cmp.Or(result.Refusal, result.StopReason)
	}
	return decision, nil
}

func sessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}

const docsSentence = "for a question about tofu itself, or a request to change its settings, rules, sub-agents or models, call " +
	tools.DocsToolName + " first, and change tofu only with the tofu command it names."

func runSystem(opts runOpts) string {
	if opts.toolSet == toolSetThree {
		return turn.EveryToolIsRelativeToTheWorkingDirectory +
			"read reads a whole file, write creates one or replaces it whole, and bash runs anything else, " +
			"including finding a file, searching text and changing part of a file."
	}
	system := strings.TrimSpace(turn.EveryToolIsRelativeToTheWorkingDirectory)
	if !opts.noDocs {
		system += " " + docsSentence
	}
	if opts.noSubAgents {
		return system
	}
	system += " " + turn.SpawnAddendum
	if opts.subAgentList != "" {
		system += "\n\n" + opts.subAgentList
	}
	return system
}

func readsWhen(readBeforeEdit bool, reads *turn.ReadLedger) *turn.ReadLedger {
	if !readBeforeEdit {
		return nil
	}
	return reads
}

type warmProcesses struct {
	checkers *turn.Typecheckers
	tests    *turn.TestRunners
	tabs     *tools.BrowserTabs
}

func newWarmProcesses(tabs *tools.BrowserTabs) *warmProcesses {
	return &warmProcesses{checkers: turn.NewTypecheckers(), tests: turn.NewTestRunners(), tabs: tabs}
}

func (w *warmProcesses) Close() {
	if w != nil {
		w.checkers.Close()
		w.tests.Close()
		w.tabs.Close()
	}
}

func buildRunToolsForRun(dir, set string, ledger *turn.ReadLedger, warm *warmProcesses, shell turn.RunShell) ([]turn.Tool, *tools.Plan, error) {
	bashTool, bashErr := turn.NewBashToolFromShell(dir, shell)
	if bashErr != nil {
		return nil, nil, bashErr
	}
	if warm != nil {
		warm.checkers.Warm(dir)
	}
	return assembleRunTools(dir, set, ledger, warm, bashTool)
}

func assembleRunTools(dir, set string, ledger *turn.ReadLedger, warm *warmProcesses, bashTool *turn.BashTool) ([]turn.Tool, *tools.Plan, error) {
	if warm == nil {
		warm = &warmProcesses{}
	}
	checkers := warm.checkers
	readTool, readErr := turn.NewReadTool(dir)
	writeTool, writeErr := turn.NewWriteTool(dir)
	if err := cmp.Or(readErr, writeErr); err != nil {
		return nil, nil, err
	}
	read := readTool.Reading(ledger)
	write := writeTool.Reading(ledger).Checking(checkers)
	checked, checkErr := tools.Checked(dir, []turn.Tool{bashTool})
	if checkErr != nil {
		return nil, nil, checkErr
	}
	shell := checked[0]
	if set == toolSetThree {
		return tools.NewMemo().Wrap([]turn.Tool{read, write, shell}), nil, nil
	}
	globTool, globErr := tools.NewGlob(dir)
	searchTool, searchErr := tools.NewSearch(dir)
	symbolsTool, symbolsErr := tools.NewSymbols(dir)
	editTool, editErr := tools.NewEdit(dir)
	projectTool, projectErr := tools.NewProject(dir)
	verbTools, verbErr := tools.NewVerbs(dir)
	githubTool, githubErr := tools.NewGitHubPRDiff(dir)
	typecheckTool, typecheckErr := tools.NewTypecheck(dir, checkers)
	testTool, testErr := tools.NewTest(dir, warm.tests)
	if err := cmp.Or(globErr, searchErr, symbolsErr, editErr, projectErr, verbErr, githubErr, typecheckErr, testErr); err != nil {
		return nil, nil, err
	}
	webTools, webErr := buildWebTools(dir)
	home, _ := os.UserHomeDir()
	settingsDir := cmp.Or(dir, ".")
	browserTools, browserErr := tools.NewBrowser(tools.BrowserSettings{
		Home:   home,
		Mode:   settingText(settingsDir, settingspkg.Browser, nil),
		Driver: settingText(settingsDir, settingspkg.BrowserDriver, nil),
		Steps:  settingInt(settingsDir, settingspkg.BrowserSteps, nil),
		Judge:  func() (jevloop.Jev, error) { return browserJudge(dir) },
		Model:  func() (turn.Model, string, error) { return browserModel(settingsDir) },
		Tabs:   warm.tabs,
	})
	if err := cmp.Or(webErr, browserErr); err != nil {
		return nil, nil, err
	}
	plan := tools.NewPlan()
	full := append([]turn.Tool{read, write, shell, plan, tools.Shells{}, projectTool, globTool, searchTool, symbolsTool, editTool.Reading(ledger).Checking(checkers), typecheckTool, testTool, githubTool}, verbTools...)
	return tools.NewMemo().Wrap(append(append(full, webTools...), browserTools...)), plan, nil
}

type subscriptionModel struct{ opts runOpts }

func (s subscriptionModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	opened, err := openAppWire(s.opts)
	if opened.held != nil {
		defer opened.held.close()
	}
	if err != nil {
		return llm.Decision{}, err
	}
	account, err := opened.held.forTurn().Pick(ctx)
	if err != nil {
		return llm.Decision{}, err
	}
	return account.Model.Ask(ctx, request)
}

func browserSlug(dir string) (slug, key string) {
	for _, key := range []string{settingspkg.BrowserModel, subagent.TierDumb.Setting(), subagent.TierWorker.Setting()} {
		if slug := strings.TrimSpace(settingText(dir, key, nil)); slug != "" {
			return slug, key
		}
	}
	return "", ""
}

func browserModel(dir string) (turn.Model, string, error) {
	slug, key := browserSlug(dir)
	if slug == "" {
		return turn.RunningModel{}, "the turn's own model, as none of " + settingspkg.BrowserModel + ", " + subagent.TierDumb.Setting() + " and " + subagent.TierWorker.Setting() + " is set", nil
	}
	library, err := modelLibrary(dir)
	if err != nil {
		return nil, "", err
	}
	model, err := library.Select(slug)
	if err != nil {
		return nil, "", err
	}
	opts := runOpts{dir: dir, wire: library.WireOf(model), model: slug}
	if len(model.Efforts) > 0 {
		opts.effort = defaultEffort(model.Efforts)
	}
	return subscriptionModel{opts}, slug + " from " + key, nil
}

func browserRecipeBrief(notify func(string)) func(subagent.Definition, string) string {
	return func(definition subagent.Definition, task string) string {
		if !isBrowserAgent(definition) {
			return ""
		}
		dir, err := recipe.Dir()
		var usable []recipe.Recipe
		if err == nil {
			usable, err = recipe.Find(dir, task)
		}
		if err != nil && notify != nil {
			notify("the browser recipes could not be read, so this run explores from the start: " + err.Error())
		}
		var brief strings.Builder
		for _, known := range usable {
			brief.WriteString("\n\n" + known.Brief())
			if notify != nil {
				notify("the browser sub-agent is given the recipe for " + known.Host)
			}
		}
		return brief.String()
	}
}

func learnBrowserRecipe(notify func(string)) func(subagent.Definition, string, []turn.Row, turn.SubAgentReport, bool) {
	return func(definition subagent.Definition, task string, rounds []turn.Row, _ turn.SubAgentReport, finished bool) {
		if !isBrowserAgent(definition) {
			return
		}
		var visited, typed []string
		for _, round := range rounds {
			for _, step := range round.Steps {
				for _, call := range step.ToolCalls {
					typed = append(typed, recipe.Typed(call.Args)...)
				}
			}
			for _, message := range round.Conversation {
				visited = append(visited, recipe.Visited(message.Content)...)
			}
		}
		dir, err := recipe.Dir()
		if err == nil {
			err = recipe.Settle(dir, task, visited, typed, finished && len(visited) > 0)
		}
		if err != nil && notify != nil {
			notify("the browser recipe for this run was not kept: " + err.Error())
		}
	}
}

func isBrowserAgent(definition subagent.Definition) bool {
	return definition.Name == browserAgent && definition.Origin == "library"
}

func offeredToSpawn(dir string, found subagent.Found) subagent.Found {
	if settingText(dir, settingspkg.BrowserDriver, nil) != settingspkg.DriverSubagent {
		found.Definitions = slices.DeleteFunc(found.Definitions, isBrowserAgent)
	}
	return found
}

func onBrowserModel(dir string, found subagent.Found, catalog models.Library) subagent.Found {
	at := slices.IndexFunc(found.Definitions, isBrowserAgent)
	slug, key := browserSlug(dir)
	wanted := llm.Effort(settingText(dir, settingspkg.BrowserEffort, nil))
	if at < 0 || found.Definitions[at].Runs == subagent.RunsRefused {
		return found
	}
	agent := &found.Definitions[at]
	if slug == "" {
		agent.Effort = wanted
		return found
	}
	model, err := catalog.Select(slug)
	if err != nil {
		agent.Runs, agent.Refused = subagent.RunsRefused, append(agent.Refused, fmt.Sprintf("%s is %s: %v", key, slug, err))
		return found
	}
	written := agent.Effort
	agent.Runs, agent.Model, agent.From, agent.Effort = subagent.RunsModel, model.Slug(), key, ""
	if len(model.Efforts) > 0 {
		agent.Effort = defaultEffort(model.Efforts)
	}
	for _, effort := range []llm.Effort{written, wanted} {
		if slices.Contains(model.Efforts, effort) {
			agent.Effort = effort
		}
	}
	return found
}

func defaultEffort(offered []llm.Effort) llm.Effort {
	floor := slices.Index(llm.Efforts(), llm.EffortLow)
	at := slices.IndexFunc(offered, func(effort llm.Effort) bool { return slices.Index(llm.Efforts(), effort) >= floor })
	return offered[max(at, 0)]
}

const (
	browserStepPoint = "browser_step@1"
	browserAgent     = "browser"
)

func browserJudge(dir string) (jevloop.Jev, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return jevloop.Jev{}, err
	}
	set, _, err := question.Resolve(browserStepPoint, layers)
	if err != nil {
		return jevloop.Jev{}, err
	}
	client, err := newJevClient(oneCallAtATime)
	if err != nil {
		return jevloop.Jev{}, err
	}
	logDir, err := sys.LogDir()
	if err != nil {
		return jevloop.Jev{}, err
	}
	return jevloop.Jev{Client: client, Set: set, Ledger: ledger.NewWriter(logDir)}, nil
}

func buildWebTools(dir string) ([]turn.Tool, error) {
	layers, err := web.Layers(shipped.Files(), dir)
	if err != nil {
		return nil, err
	}
	config, err := web.Load(layers)
	if err != nil {
		return nil, err
	}
	return tools.NewWeb(config), nil
}

func askedAsOf(row turn.Row, spawned []turn.Spawned, slug, windows string) (string, string) {
	for _, one := range spawned {
		if row.ID == one.ID || strings.HasPrefix(row.ID, one.ID+"-") || (one.Call != "" && row.SpawnedBy == one.Call) {
			slug, windows = one.Slug, one.Windows
		}
	}
	return slug, windows
}

func wordsAfterLastCalls(row turn.Row) (string, bool) {
	if len(row.Steps) == 0 || len(row.Steps[len(row.Steps)-1].ToolCalls) == 0 || len(row.Conversation) == 0 {
		return "", false
	}
	if last := row.Conversation[len(row.Conversation)-1]; last.Role == llm.RoleAssistant {
		return last.Content, strings.TrimSpace(last.Content) != ""
	}
	step := row.Steps[len(row.Steps)-1]
	onlySpawned := !slices.ContainsFunc(step.ToolCalls, func(call turn.ToolCallRow) bool { return call.SubAgentID == "" })
	return step.AssistantText, onlySpawned && strings.TrimSpace(step.AssistantText) != ""
}

func printRunRow(out io.Writer, row turn.Row, askedAs, windows string) {
	spend := "spend subscription windows " + windows + ", no money"
	if row.Spend != turn.SpendSubscription {
		spend = fmt.Sprintf("spend api key $%.6f", row.TotalCostUSD)
	}
	_, _ = fmt.Fprintf(out, "turn %s outcome %s model %s asked_as %s %s wall_clock_ms %d\n",
		row.ID, row.Outcome, row.Model, askedAs, spend, row.WallClockMS)
	if len(row.SubAgentIDs) > 0 {
		_, _ = fmt.Fprintf(out, "turn %s spawned %s\n", row.ID, strings.Join(row.SubAgentIDs, " "))
	}
	for _, warning := range row.Warnings {
		_, _ = fmt.Fprintf(out, "turn %s warning %s\n", row.ID, warning)
	}
	for _, step := range row.Steps {
		_, _ = fmt.Fprintf(out, "step %d: stop_reason %s in %d out %d cache_read %d cache_write %d\n",
			step.Index, step.StopReason, step.PromptTokens, step.CompletionTokens,
			step.CacheReadTokens, step.CacheWriteTokens)
		for _, warning := range step.Warnings {
			_, _ = fmt.Fprintf(out, "step %d: warning %s\n", step.Index, warning)
		}
		if len(step.ToolCalls) == 0 {
			_, _ = fmt.Fprintf(out, "step %d: assistant_text %q\n", step.Index, step.AssistantText)
			continue
		}
		for _, call := range step.ToolCalls {
			exitCode := "<nil>"
			if call.ExitCode != nil {
				exitCode = strconv.Itoa(*call.ExitCode)
			}
			_, _ = fmt.Fprintf(out, "step %d: tool_call tool=%s command=%q sub_agent=%q exit_code=%s gate=%s error=%q\n",
				step.Index, call.Tool, call.Command, call.SubAgentID, exitCode, call.GateVerdict, call.Error)
		}
	}
	if said, ended := wordsAfterLastCalls(row); ended {
		_, _ = fmt.Fprintf(out, "step %d: assistant_text %q\n", row.Steps[len(row.Steps)-1].Index, said)
	}
}

func subAgentRows(spawner *turn.SpawnTool) []turn.Row {
	if spawner == nil {
		return nil
	}
	return spawner.SubAgentRows()
}

func runFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu run: %v\n", err)
	return exitUsage
}

func parseRunArgs(args []string) (runOpts, error) {
	opts := runOpts{
		wire:             wireSubscription,
		toolSet:          toolSetFull,
		doneArm:          doneArmOff,
		loopGuardRepeats: konst.TurnLoopGuardRepeats,
		loopGuardWindow:  konst.TurnLoopGuardWindow,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch arg {
		case "--dir":
			opts.dir, err = nextArg(args, &i, arg)
		case "--dry-run":
			opts.dryRun = true
		case "--show-prompt":
			opts.showPrompt = true
		case "--agent":
			opts.agent, err = nextArg(args, &i, arg)
		case "--no-gate":
			opts.gateArm = gateOff
		case "--gate":
			opts.gateArm, err = nextArg(args, &i, arg)
		case "--sift":
			opts.siftArm, err = nextArg(args, &i, arg)
		case "--no-subagents":
			opts.noSubAgents = true
		case "--no-instructions":
			opts.noInstructions = true
		case "--no-docs":
			opts.noDocs = true
		case "--wire":
			opts.wire, err = nextArg(args, &i, arg)
		case "--tools":
			opts.toolSet, err = nextArg(args, &i, arg)
		case "--done-review":
			opts.doneArm, err = nextArg(args, &i, arg)
		case "--model":
			if opts.model, err = nextArg(args, &i, arg); err == nil && strings.TrimSpace(opts.model) == "" {
				err = errors.New("--model needs a model id")
			}
		case "--effort":
			var raw string
			if raw, err = nextArg(args, &i, arg); err == nil {
				opts.effort, err = llm.ParseEffort(raw)
			}
		case "--max-steps":
			opts.maxSteps, err = nextInt(args, &i, arg)
		case "--loop-guard-repeats":
			opts.loopGuardRepeats, err = nextInt(args, &i, arg)
		case "--loop-guard-window":
			opts.loopGuardWindow, err = nextInt(args, &i, arg)
		case "--context-ceiling":
			if opts.contextCeiling, err = nextInt(args, &i, arg); err == nil && opts.contextCeiling <= 0 {
				err = fmt.Errorf("--context-ceiling %d takes a count of tokens above zero, as in --context-ceiling 20000", opts.contextCeiling)
			}
		default:
			switch {
			case strings.HasPrefix(arg, "--"):
				err = fmt.Errorf("unknown argument %q", arg)
			case opts.task != "":
				err = fmt.Errorf("tofu run takes one task, got %q and %q", opts.task, arg)
			default:
				opts.task = arg
			}
		}
		if err != nil {
			return runOpts{}, err
		}
	}
	if opts.dir == "" {
		return runOpts{}, errors.New("--dir is required: tofu run never defaults to the current directory, because its bash tool is not sandboxed")
	}
	if strings.TrimSpace(opts.task) == "" {
		return runOpts{}, errors.New("tofu run needs a task")
	}
	if opts.agent != "" && !opts.showPrompt {
		return runOpts{}, errors.New("--agent prints the prompt a spawn of that sub-agent composes and runs nothing, so it needs --show-prompt")
	}
	if !slices.Contains(runWires(), opts.wire) {
		return runOpts{}, fmt.Errorf("--wire %q is none of %s", opts.wire, strings.Join(runWires(), ", "))
	}
	if source, _, _ := strings.Cut(opts.model, "/"); source == wireMeta {
		if slices.Contains(args, "--wire") {
			return runOpts{}, fmt.Errorf("--model %s is paid by the %s key and reaches its own wire, so it takes no --wire", opts.model, wireMeta)
		}
		opts.wire = wireMeta
	}
	if opts.wire == wireKey && opts.effort != "" {
		return runOpts{}, fmt.Errorf("--effort %s with --wire %s: the openrouter wire sends no reasoning effort, so the level would be dropped without a word",
			opts.effort, wireKey)
	}
	if opts.wire != wireKey {
		opts.effort = cmp.Or(opts.effort, llm.EffortDefault)
	}
	if opts.gateArm != gateFollowsTheRule && !slices.Contains(gateArms(), opts.gateArm) {
		return runOpts{}, fmt.Errorf("--gate %q is none of %s: with no --gate the rule's own mode decides",
			opts.gateArm, strings.Join(gateArms(), ", "))
	}
	if opts.siftArm != siftFollowsTheTable && !slices.Contains(siftArms(), opts.siftArm) {
		return runOpts{}, fmt.Errorf("--sift %q is none of %s: with no --sift the method table decides",
			opts.siftArm, strings.Join(siftArms(), ", "))
	}
	if !slices.Contains(doneArms(), opts.doneArm) {
		return runOpts{}, fmt.Errorf("--done-review %q is none of %s", opts.doneArm, strings.Join(doneArms(), ", "))
	}
	switch opts.toolSet {
	case toolSetFull, toolSetThree:
	default:
		return runOpts{}, fmt.Errorf("--tools %q is neither %s nor %s, the arm that offers read, write and bash alone",
			opts.toolSet, toolSetFull, toolSetThree)
	}
	if without := spawnlessFlag(opts); opts.doneArm != doneArmOff && without != "" {
		return runOpts{}, fmt.Errorf("--done-review %s with %s: that arm has no spawn tool, so no sub-agent is ever reviewed and the flag would say a check is running that is not",
			opts.doneArm, without)
	}
	return opts, nil
}

func spawnlessFlag(opts runOpts) string {
	if opts.noSubAgents {
		return "--no-subagents"
	}
	if opts.toolSet == toolSetThree {
		return "--tools " + toolSetThree
	}
	return ""
}

func nextArg(args []string, i *int, flag string) (string, error) {
	*i++
	if *i >= len(args) {
		return "", fmt.Errorf("%s needs a value", flag)
	}
	return args[*i], nil
}

func nextInt(args []string, i *int, flag string) (int, error) {
	raw, err := nextArg(args, i, flag)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number", flag, raw)
	}
	return n, nil
}
