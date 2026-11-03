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

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/cron"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/cli/output"
	"tofu/internal/cli/page"
	"tofu/internal/cli/wiring"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/librarian/skills"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/core"
	"tofu/internal/models/llm/wire/anthropic"
	"tofu/internal/models/llm/wire/codex"
	"tofu/internal/models/llm/wire/openrouter"
	"tofu/internal/models/transport"
	"tofu/internal/platform/recover"
	"tofu/internal/project/boardy"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/memory/tree"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/tools/registry"
	"tofu/internal/tools/web"
	"tofu/internal/tools/web/browser/loop"
	"tofu/internal/tools/web/browser/recipe"
	shipped "tofu/library"
)

const (
	toolSetFull         = "full"
	toolSetThree        = "three"
	toolPickRule        = "tool_pick"
	verifySubAgentsRule = "verify_sub_agents"

	gateFollowsTheRule = ""
	gateOff            = "off"
	gateShadow         = "shadow"
	gateEnforce        = "enforce"

	openRouterEndpointVariable = "TOFU_OPENROUTER_ENDPOINT"
)

func wireEfforts(wire string) []llm.Effort {
	switch wire {
	case wiring.WireSubscription:
		return anthropic.ReasoningEfforts()
	case wiring.WireCodex:
		return llm.Efforts()
	}
	return nil
}

func wireSpend(wire string) turn.Spend {
	if wire == wiring.WireKey || wire == wiring.WireMeta {
		return turn.SpendAPIKey
	}
	return turn.SpendSubscription
}

type runtime struct {
	model        turn.Model
	plan         *tools.Plan
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
	open         func(wiring.RunOpts) (appWire, error)
	wrapSubAgent func(turn.Model, recall.Budget) (turn.Model, error)
	orchestrator models.Model
	tabs         *tools.BrowserTabs
	leadAsks     turn.Person
	cron         *cron.Book
	places       sys.Places
	startup      *startupClock
	keys         *storedKeys
	memory       *sessionMemoryModel
	drops        *turn.DropIndex
	current      func(subagent.Definition) subagent.Definition
	composed     *composedRun
	foreseen     *leadForeseen

	omitThinkingSummary bool
}

func (r runtime) leadTools(lead []turn.Tool) turn.Registry {
	scheduled := turn.Tool(turn.ListedOnly{Tool: tools.Cron{}, Why: "tofu run has no host to fire a job, so one made here would never run: schedule it in the app or tofu serve"})
	if r.cron != nil {
		scheduled = tools.Cron{Book: r.cron, Now: r.now, Told: r.notify}
	}
	return turn.NewRegistry(append(slices.Clone(lead), scheduled)...)
}

type summaryOmitted struct{ inner turn.Model }

func (s summaryOmitted) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	request.OmitThinkingSummary = true
	return s.inner.Ask(ctx, request)
}

func thinkingSummaries() []string {
	return []string{settingspkg.ThinkingSummarized, settingspkg.ThinkingOmitted}
}

func thinkingSummaryOff(opts wiring.RunOpts, say func(string)) bool {
	if opts.ThinkingSummary != "" {
		return opts.ThinkingSummary == settingspkg.ThinkingOmitted
	}
	return settingspkg.TextSaying(cmp.Or(opts.Dir, "."), settingspkg.ThinkingSummary, say) == settingspkg.ThinkingOmitted
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
	omitting := turn.Accounts{
		Pick: func(ctx context.Context) (turn.Account, error) {
			account, err := accounts.Pick(ctx)
			return omitted(account), err
		},
		Next: func(ctx context.Context, pinned turn.Account) (turn.Account, bool, error) {
			account, moved, err := accounts.Next(ctx, pinned)
			return omitted(account), moved, err
		},
		Resets: accounts.Resets,
	}
	if accounts.Keep != nil {
		omitting.Keep = func(ctx context.Context, held turn.Held) (turn.Account, string, error) {
			account, leftBecause, err := accounts.Keep(ctx, held)
			return omitted(account), leftBecause, err
		}
	}
	if accounts.Elsewhere != nil {
		omitting.Elsewhere = func(ctx context.Context, left []string, conversationTokens int) (turn.Moved, error) {
			moved, err := accounts.Elsewhere(ctx, left, conversationTokens)
			moved.Account, moved.Accounts = omitted(moved.Account), r.applyThinkingSummary(moved.Accounts)
			return moved, err
		}
	}
	return omitting
}

func chooseModel(opts wiring.RunOpts) (models.Model, error) {
	switch opts.Wire {
	case "":
		return models.Model{}, models.RoleOrchestrator.Unset()
	case wiring.WireKey:
		if opts.Model == "" {
			return models.Model{}, errors.New("--wire openrouter names no model: pass --model, or bind the orchestrator to an openrouter model")
		}
		library, err := wiring.ModelLibrary(opts.Dir)
		if err != nil {
			return models.Model{}, err
		}
		if listed, known := library.Resolve(opts.Model); known && listed.Provider == models.OpenRouter {
			return library.Select(listed.Slug())
		}
		return models.Model{ID: opts.Model}, nil
	case wiring.WireMeta:
		library, err := wiring.ModelLibrary(opts.Dir)
		if err != nil {
			return models.Model{}, err
		}
		if opts.Model == "" {
			return library.Default(models.Subscription(models.Meta))
		}
		return library.Select(opts.Model)
	}
	if opts.Model != "" {
		return wiring.SelectModel(opts.Wire, opts.Model)
	}
	bound, err := wiring.BoundRoles(opts.Wire, opts.Dir)
	if err != nil {
		return models.Model{}, err
	}
	orchestrator := bound[models.RoleOrchestrator]
	if orchestrator.Wire != opts.Wire {
		return models.Model{}, fmt.Errorf(
			"%s, and --wire %s reaches another subscription: run --wire %s instead, or bind the orchestrator role to a model --wire %s serves",
			orchestrator.Says(), opts.Wire, orchestrator.Wire, opts.Wire)
	}
	return orchestrator.Model, nil
}

func onTheBoundWire(opts wiring.RunOpts) wiring.RunOpts {
	if opts.Wire != "" && opts.Model != "" {
		return opts
	}
	library, err := wiring.ModelLibrary(opts.Dir)
	if err != nil {
		return opts
	}
	if opts.Model != "" {
		if named, err := library.Select(opts.Model); err == nil {
			opts.Wire = library.WireOf(named)
		}
		return opts
	}
	if orchestrator, bound := library.Bound(models.RoleOrchestrator); bound && (opts.Wire == "" || opts.Wire == library.WireOf(orchestrator)) {
		opts.Wire, opts.Model = library.WireOf(orchestrator), orchestrator.Slug()
	}
	return opts
}

func boundSubAgent(opts wiring.RunOpts) (string, error) {
	library, err := wiring.ModelLibrary(opts.Dir)
	if err != nil {
		return "", err
	}
	if subAgent, bound := library.Bound(models.RoleSubAgent); bound {
		return subAgent.Slug(), nil
	}
	return "", nil
}

type subAgentTarget struct {
	named   string
	wire    string
	offered []llm.Effort
}

func (r runtime) subAgentTarget(opts wiring.RunOpts, definition subagent.Definition) (subAgentTarget, error) {
	if r.current != nil {
		definition = r.current(definition)
	}
	target := subAgentTarget{named: definition.Model, wire: opts.Wire, offered: r.orchestrator.Efforts}
	if definition.Model == "" {
		bound, err := boundSubAgent(opts)
		if err != nil {
			return subAgentTarget{}, err
		}
		target.named = bound
	}
	if target.named != "" {
		library, err := wiring.ModelLibrary(opts.Dir)
		if err != nil {
			return subAgentTarget{}, err
		}
		model, err := library.Select(target.named)
		if err != nil {
			return subAgentTarget{}, err
		}
		target.wire, target.offered = library.WireOf(model), model.Efforts
	}
	if target.wire == wiring.WireKey {
		target.offered = nil
	}
	return target, nil
}

func (r runtime) subAgentTakes(opts wiring.RunOpts) func(subagent.Definition) ([]llm.Effort, error) {
	if r.open == nil {
		return nil
	}
	return func(definition subagent.Definition) ([]llm.Effort, error) {
		target, err := r.subAgentTarget(opts, definition)
		return target.offered, err
	}
}

func (r runtime) subAgentOpener(opts wiring.RunOpts) func(subagent.Definition, llm.Effort) (turn.SubAgentModel, error) {
	if r.open == nil {
		return nil
	}
	var told sync.Map
	return func(definition subagent.Definition, wanted llm.Effort) (turn.SubAgentModel, error) {
		target, err := r.subAgentTarget(opts, definition)
		if err != nil {
			return turn.SubAgentModel{}, err
		}
		subAgent := opts
		subAgent.Model, subAgent.Wire = cmp.Or(target.named, opts.Model), target.wire
		named, offered, asked := target.named, target.offered, cmp.Or(target.named, r.orchestrator.Slug())
		switch {
		case wanted != "" && len(offered) == 0:
			return turn.SubAgentModel{}, fmt.Errorf("effort %s for %s: it sends no reasoning effort, so the level would be dropped without a word", wanted, asked)
		case wanted != "" && !slices.Contains(offered, wanted):
			return turn.SubAgentModel{}, fmt.Errorf("effort %s for %s: it takes %s, so the level would be changed without a word", wanted, asked, llm.EffortList(offered))
		}
		subAgent.Effort = cmp.Or(wanted, definition.Effort, opts.Effort)
		if wanted == "" && definition.Effort != "" && !slices.Contains(offered, definition.Effort) {
			subAgent.Effort = ""
			if len(offered) > 0 {
				subAgent.Effort = wiring.DefaultEffort(offered)
			}
			if _, said := told.LoadOrStore(definition.Name, true); !said && r.notify != nil {
				r.notify(fmt.Sprintf("%s asks for effort %s, which %s does not take, so it runs at %s", definition.Name, definition.Effort, asked, cmp.Or(string(subAgent.Effort), "no effort")))
			}
		}
		if named == "" && subAgent.Effort == opts.Effort {
			return turn.SubAgentModel{Slug: asked, Windows: r.orchestrator.WindowText(), Effort: subAgent.Effort}, nil
		}
		opened, err := r.open(subAgent)
		if err != nil {
			if opened.held != nil {
				opened.held.close()
			}
			return turn.SubAgentModel{}, err
		}
		opened.held.wrapping(r.wrapSubAgent, r.budget, subAgent)
		return turn.SubAgentModel{Slug: asked, Windows: opened.selected.WindowText(), Wire: subAgent.Wire, Cache: opened.selected.PromptCache, Effort: subAgent.Effort, Spend: opened.spend, Accounts: r.applyThinkingSummary(opened.held.forTurn()), Close: opened.held.close}, nil
	}
}

func runUsage() string {
	return fmt.Sprintf(runUsageText,
		llm.EffortList(llm.Efforts()), llm.EffortDefault,
		llm.EffortList(anthropic.ReasoningEfforts()), strings.Join(thinkingSummaries(), " or "), shellSiftCost(), turn.LeadAloneMostNamedFiles, turn.InstructionsOff, konst.TemporaryProjectIdleHours)
}

const runUsageText = `tofu run works a task in a directory until it is done.

Usage:
  tofu run --dir <path> [arguments] <task>

Arguments:
  --dir <path>          the directory the task is worked in, required
  --session <handle>    a name, an id or head: the task is the next turn of that session in
                        the project, with its conversation and the prefix it first sent
  --model <id>          the orchestrator's model, otherwise the one the orchestrator role is bound to;
                        a meta/ model is paid by the key tofu login meta stores and picks its own wire
  --wire <name>         anthropic, codex or openrouter, otherwise the wire of the --model slug's subscription
  --tools <set>         full, or three for the read, write and bash arm
  --effort <level>      how hard the model thinks: %s.
                        the default is %s. the anthropic wire takes %s,
                        and refuses the rest rather than picking a neighbour.
                        the openrouter wire sends no level and takes no --effort
  --thinking-summary <v> %s, for this run only, otherwise the
                        thinkingSummary setting; the stored setting is not changed
  --gate <arm>          off, shadow or enforce, and enforce when not given. nobody answers
                        a tofu run, so under enforce and gatePrompt auto a call the gate asks
                        about runs and is recorded, and a deny or a gate that could not answer
                        refuses it; under gatePrompt ask an asked call does not run
  --no-gate             the arm that turns the tool gate off
  --sift <arm>          free or judged, otherwise the method table decides which
                        one cuts a bash result before the model reads it. free
                        keeps an error-shaped line and the ends of the output,
                        drops the rest, costs nothing and makes no call. judged
                        costs, from library/decisions/methods@1.yaml:
                        %s
  --no-subagents        run without the spawn tool for this run, no matter what
                        the turnMaySpawn setting says; there is no flag that
                        turns spawning on when that setting says off; a task
                        naming at most %d files in one folder and no
                        sub-agent keeps spawn but gets no spawn text or rules
                        the readBeforeEdit setting, on by default, refuses an
                        edit or a write to a file this session has not read;
                        there is no flag for it, set it with
                        tofu settings set readBeforeEdit false
  --no-instructions     the arm that %s
  --no-docs             the arm that drops the tofu_docs tool and the prompt sentence pointing at it
  --done-review <arm>          the arm that reviews a sub-agent's answer, over the doneReview setting
  --max-steps <n>              cap the steps a turn takes, unset means no cap
  --loop-guard-repeats <n>     how many repeats of one call with one result stops a turn
  --loop-guard-window <n>      how many recent calls the loop guard remembers
  --dry-run                    print the request that would be sent and send nothing
  --show-prompt                print the prompt the turn composes, part by part, and send nothing
  --agent <name>               with --show-prompt, print the prompt a spawn of that sub-agent composes for the task
  --temporary-project          register a project tofu has not seen as temporary: the next tofu
                               started %d hours after its last use removes its sessions, undo
                               history and logs, never the directory itself; a later open without
                               the flag keeps it for good

TOFU_DRIVE_CASSETTE names a recorded model, read as tofu drive reads it, and
then the run opens no live wire for the orchestrator or for any sub-agent.
`

func runVerb(args []string, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, runUsage())
		return output.ExitOK
	}
	clock := clockFromProcessStart()
	opts, err := parseRunArgs(args)
	if err != nil {
		return runFail(errOut, err)
	}
	resumed, err := resumeRun(opts.Dir, opts.Resume, writeNotice(errOut))
	if err != nil {
		hinted := output.ProblemError{What: err.Error()}
		errors.As(err, &hinted)
		return output.Verb{Name: "run", ErrOut: errOut}.Refuse(output.ExitUsage, cli.Problem(hinted))
	}
	defer resumed.letGo()
	opts, modelFrom := resumed.onItsModel(opts)
	opts = onTheBoundWire(opts)
	clock.step("model roles")
	dir := cmp.Or(opts.Dir, ".")
	say := func(unreadable string) { _, _ = fmt.Fprintln(errOut, "tofu run: "+unreadable) }
	if library, err := wiring.ModelLibrary(opts.Dir); err == nil {
		for _, refused := range library.Broken {
			say("refused " + refused.Error())
		}
	}
	opts = settled(opts, say)
	if opts.NoSubAgents && opts.DoneArm != settingspkg.DoneReviewOff {
		return runFail(errOut, fmt.Errorf(
			"--done-review %s with the %s setting off: that arm has no spawn tool, so no sub-agent is ever reviewed and the flag would say a check is running that is not",
			opts.DoneArm, settingspkg.TurnMaySpawn))
	}
	opts.LeadMayWorkAlone = true
	if opts.ShowPrompt {
		return showPrompt(opts, out, errOut)
	}
	clock.step("settings")

	selected, err := chooseModel(opts)
	if err != nil {
		return runFail(errOut, err)
	}
	if modelFrom != "" {
		_, _ = fmt.Fprintf(out, "session %s runs on %s, from %s\n", resumed.carry.Handle, selected.Slug(), modelFrom)
	}
	clock.step("model choice")
	if opts.DryRun {
		return dryRun(opts, selected, dir, say, out, errOut)
	}
	return runTurn(opts, resumed, selected, clock, out, errOut)
}

func dryRun(opts wiring.RunOpts, selected models.Model, dir string, say func(string), out, errOut io.Writer) int {
	shell, err := turn.ResolveRunShell(settingspkg.TextSaying(dir, settingspkg.Shell, say))
	if err != nil {
		return runFail(errOut, err)
	}
	opts.Shell = shell
	home, _ := os.UserHomeDir()
	warm := newWarmProcesses(tools.NewBrowserTabs(home))
	defer warm.Close()
	built, plan, err := buildRunToolsForRun(opts.Dir, opts.ToolSet, readsWhen(opts.ReadBeforeEdit, turn.NewReadLedger()), warm, shell)
	if err != nil {
		return runFail(errOut, err)
	}
	built, _ = planAsOffered(selected, built, plan)
	budget, err := contextBudget(opts, selected)
	if err != nil {
		return runFail(errOut, err)
	}
	opts.Effort = selected.EffortTaken(opts.Effort)
	config, _, err := runConfig(opts, built, runtime{spend: turn.SpendSubscription, budget: budget, notify: writeNotice(errOut), open: openAppWire, orchestrator: selected})
	if err != nil {
		return runFail(errOut, err)
	}
	body, err := dryRunBody(opts, selected, config)
	if err != nil {
		return runFail(errOut, err)
	}
	_, _ = fmt.Fprintln(errOut, "context budget "+budget.Record())
	_, _ = fmt.Fprintln(out, string(body))
	return output.ExitOK
}

func contextBudget(opts wiring.RunOpts, model models.Model) (recall.Budget, error) {
	registry, err := modelRegistry()
	if err != nil {
		return recall.Budget{}, err
	}
	windowTokens, windowSource := models.WindowFor(model, registry, models.Served{})
	budget, err := recall.BudgetUnder(model.ID, windowTokens, settingspkg.IntSaying(opts.Dir, settingspkg.ContextCeiling, nil))
	if err != nil {
		return recall.Budget{}, err
	}
	budget.WindowSource = windowSource
	if opts.ContextCeiling <= 0 {
		return budget, nil
	}
	return budget.At(opts.ContextCeiling, fmt.Sprintf("--context-ceiling %d, which is read before %s and wins when both are set",
		opts.ContextCeiling, recall.CeilingVariable)), nil
}

func runTurn(opts wiring.RunOpts, resumed runResumed, selected models.Model, clock *startupClock, out, errOut io.Writer) int {
	open := openAppWire
	deck, err := readCassette(os.Getenv(cassetteVariable))
	if err != nil {
		return runFail(errOut, err)
	}
	if deck != nil {
		open = driveWire(deck)
	}
	places, err := sys.ResolvePlaces(".")
	if err != nil {
		return runFail(errOut, err)
	}
	home, _ := os.UserHomeDir()
	warm := newWarmProcesses(tools.NewBrowserTabs(home))
	defer warm.Close()
	missing := missingJudgeRefuses
	if opts.GateArm == gateOff {
		missing = missingJudgeRunsWithout
	}
	engine := &appEngine{dir: opts.Dir, open: open, tabs: warm.tabs, warm: warm, places: places, missingJudge: missing}
	same := func(model turn.Model) turn.Model { return model }
	started, err := engine.startTurn(opts, host.Turn{}, host.Hooks{Lead: same, SubAgent: same, Say: writeNotice(errOut), Reads: turn.NewReadLedger()}, clock)
	if started.Close != nil {
		defer started.Close()
	}
	if err != nil {
		return runFail(errOut, err)
	}
	config, spawner, sessions, gate, scorer := started.Config, started.Spawner, started.Sessions, started.gate, started.scorer
	resumed.into(&config)
	_, _ = fmt.Fprintln(out, "context budget "+started.budget.Record())
	registry, registryErr := launchShellRegistry(opts.Dir)
	if registryErr == nil {
		_ = registry.Prune()
	}
	head, rows := "", []turn.Row{}
	interrupted, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	context.AfterFunc(interrupted, stop)
	leading, err := leadScratch(interrupted, config, errOut)
	if err != nil {
		return runFail(errOut, err)
	}
	clock.step("shell registry and scratch")
	runErr := turn.Lead(turn.WithShellRegistry(leading, registry), config, nil, nil, func(row turn.Row, _ error) {
		printRunRow(out, row, selected.Slug(), selected.WindowText())
		head, rows = cmp.Or(row.Session, head), append(rows, row)
	})
	for _, warning := range turn.EndSession(context.Background(), cmp.Or(opts.Dir, "."), head, sessionEndOther) {
		_, _ = fmt.Fprintln(errOut, "tofu run: "+warning)
	}
	leaveShells(registry)
	for _, subAgent := range subAgentRows(spawner) {
		askedAs, windows := askedAsOf(subAgent, spawner.Spawned(), selected.Slug(), selected.WindowText())
		printRunRow(out, subAgent, askedAs, windows)
		rows = append(rows, subAgent)
	}
	requests, calls, unanswered := runTotals(rows)
	_, _ = fmt.Fprintf(out, "totals requests %d calls %d\n", requests, calls)
	if head != "" {
		if headErr := sessions.SetHead(head); headErr != nil {
			_, _ = fmt.Fprintf(errOut, "tofu run: pointing the head at %s: %v\n", head, headErr)
		}
	}
	if gate != nil {
		_, _ = fmt.Fprintf(out, "gate decisions %d cost $%.6f mode %s\n", gate.decisions, gate.costUSD, config.GateMode)
		if unanswered > 0 && config.GateMode == turn.GateEnforce {
			_, _ = fmt.Fprintln(errOut, "tofu run: "+output.Plural(unanswered, "call")+" did not run: nobody answers a tofu run, so under enforce a call the gate asks about runs and is recorded only under gatePrompt auto, and a deny or a gate that could not answer refuses it")
		}
	}
	if scorer != nil {
		calls, cost := scorer.spend()
		_, _ = fmt.Fprintf(out, "sift decisions %d cost $%.6f\n", calls, cost)
	}
	if runErr != nil {
		return runFail(errOut, runErr)
	}
	return output.ExitOK
}

func runEnvironment(opts wiring.RunOpts, now func() time.Time) (environment, written, instructions, notice string) {
	today := time.Now()
	if now != nil {
		today = now()
	}
	if opts.Shell.Resolved() {
		environment = turn.EnvironmentFromShell(opts.Dir, today, opts.Shell)
	} else {
		environment = turn.Environment(opts.Dir, today)
	}
	if opts.NoInstructions {
		return environment, "", "off by request, and " + turn.InstructionsOff, ""
	}
	home, _ := os.UserHomeDir()
	setting, unreadable := settingspkg.IntFor(opts.Dir, settingspkg.ProjectInstructionsCap)
	capBytes := turn.InstructionCap(setting)
	written, cut, skipped := turn.ProjectInstructionsInOrder(opts.Dir, home, capBytes, settingspkg.TextSaying(opts.Dir, settingspkg.InstructionSources, nil))
	instructions = "on, and neither an AGENTS.md nor a CLAUDE.md was found to send"
	if written != "" {
		instructions = fmt.Sprintf("on, %d bytes", len(written))
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
	return environment, written, instructions, strings.Join(notices, "; ")
}

func writeNotice(w io.Writer) func(string) {
	return func(notice string) { _, _ = fmt.Fprintln(w, "tofu: "+notice) }
}

type composedRun struct {
	opts         wiring.RunOpts
	environment  string
	files        string
	memory       string
	instructions string
	composed     turn.Composed
	composeTask  func(task string) (turn.Composed, error)
	subAgents    turn.SubAgents
	skills       []skill.Skill
	shippedOff   []rule.Rule
	checksWork   bool
}

func composeRun(opts wiring.RunOpts, built []turn.Tool, run runtime) (composedRun, error) {
	if run.composed != nil {
		return *run.composed, nil
	}
	say := func(notice string) {
		if notice != "" && run.notify != nil {
			run.notify(notice)
		}
	}
	found := make(chan subagent.Found, 1)
	if !opts.NoSubAgents && opts.ToolSet != toolSetThree && run.open != nil {
		scanned := crashTold(cmp.Or(opts.Dir, "."), func(said string) {
			say(said)
			found <- subagent.Found{}
		})
		safe.Go(scanned, "sub-agent scan", func() { found <- offeredToSpawn(cmp.Or(opts.Dir, "."), scanSubAgents(opts.Dir, built)) })
	} else {
		found <- subagent.Found{}
	}
	environment, files, instructions, notice := runEnvironment(opts, run.now)
	say(notice)
	run.startup.step("instructions and environment")
	remembered := ""
	if opts.Agent == "" && memoryOn(cmp.Or(opts.Dir, ".")) {
		block, unread := memory.Block(cmp.Or(opts.Dir, "."))
		if unread != nil {
			say("your memory is not sent this turn: " + unread.Error())
		}
		remembered = block
	}
	run.startup.step("memory")
	discovered := <-found
	opts.LeadAlone = opts.LeadMayWorkAlone && opts.Agent == "" && !opts.NoSubAgents && opts.ToolSet != toolSetThree &&
		turn.LeadWorksAlone(cmp.Or(opts.Dir, "."), opts.Task, discovered.Definitions)
	if opts.LeadAlone {
		say(fmt.Sprintf("the lead works alone: the task names %d or fewer files in one folder and asks for no sub-agent, so the spawn text and the spawn rules are not sent", turn.LeadAloneMostNamedFiles))
	} else {
		opts.SubAgentList = turn.SubAgentList(discovered.Definitions)
	}
	var brokenAgents []turn.PromptPart
	for _, broken := range discovered.Broken {
		say("the sub-agent in " + broken.Path + " is not offered: " + broken.Reason)
		brokenAgents = append(brokenAgents, turn.PromptPart{Concern: rule.ConcernIdentity, File: filepath.ToSlash(broken.Path), Why: broken.Reason, State: session.PartDropped})
	}
	run.startup.step("sub-agent definitions")
	stack, err := stackRules("", cmp.Or(opts.Dir, "."))
	if err != nil {
		return composedRun{}, err
	}
	run.startup.step("rules")
	rules := stack.rules
	var switchedOff []rule.Overriding
	for _, applied := range stack.overrides {
		if applied.Rule.Mode == rule.ModeOff && !applied.Stale && !slices.ContainsFunc(rules, func(r rule.Rule) bool { return r.ID == applied.Base.ID }) {
			switchedOff = append(switchedOff, applied.Overriding)
		}
	}
	if opts.ToolSet == toolSetThree {
		rules = slices.DeleteFunc(rules, func(loaded rule.Rule) bool { return loaded.ID == toolPickRule })
	}
	var skills []skill.Skill
	var unloaded []turn.PromptPart
	skillsOff := settingspkg.TextSaying(cmp.Or(opts.Dir, "."), settingspkg.Skills, run.notify) == settingspkg.SkillsOff
	if opts.ToolSet != toolSetThree && !skillsOff {
		home, _ := os.UserHomeDir()
		offered := skill.Discover(cmp.Or(opts.Dir, "."), home)
		for _, warning := range offered.Warnings {
			say(warning)
		}
		for _, one := range offered.Unoffered {
			unloaded = append(unloaded, turn.PromptPart{Concern: turn.ConcernSkills, Name: one.Name, File: filepath.ToSlash(one.File), Why: one.Why, State: session.PartDropped})
		}
		library, err := skill.Shipped(shipped.Files(), libraryRoot)
		if err != nil {
			return composedRun{}, err
		}
		skills = slices.Concat(offered.Skills, slices.DeleteFunc(library, func(one skill.Skill) bool {
			return slices.ContainsFunc(offered.Skills, func(own skill.Skill) bool { return own.Name == one.Name })
		}))
		slices.SortFunc(skills, func(a, b skill.Skill) int { return strings.Compare(a.Name, b.Name) })
	}
	run.startup.step("skills")
	subAgents := turn.SubAgents{Defined: discovered.Definitions, Root: cmp.Or(opts.Dir, "."), Prompt: turn.ComposeSpec{Environment: strings.TrimSpace(environment + "\n\n" + files),
		ToolGuidance: turn.EveryToolIsRelativeToTheWorkingDirectory + turn.ContractAddendum, Rules: rules, SwitchedOff: switchedOff, Skills: skills, SkillsOff: skillsOff, WindowTokens: run.budget.WindowTokens,
		Kinds: turn.NewKindJudge(cmp.Or(opts.Dir, ".")), Applies: turn.NewRuleJudge(cmp.Or(opts.Dir, "."))}}
	spec := subAgents.Prompt
	spec.ToolGuidance = runSystem(opts)
	if opts.Agent != "" {
		if spec.Agent, err = subAgents.Named(opts.Agent); err != nil {
			return composedRun{}, err
		}
		spec.Role, spec.ToolGuidance = rule.RoleSubAgent, subAgents.Prompt.ToolGuidance
	} else if !opts.NoSubAgents && opts.ToolSet != toolSetThree {
		spec.Role = rule.RoleOrchestrator
	}
	if opts.LeadAlone {
		spec.Rules = slices.DeleteFunc(slices.Clone(rules), func(loaded rule.Rule) bool {
			return slices.Contains([]string{"orchestrate", verifySubAgentsRule, "plan_before_spawn", "spawn_effort", "research_fan_out", "brief_from_references", "announce_spawn"}, loaded.ID)
		})
		spec.Alone = true
	}
	subAgents.Prompt.Dropped = slices.Concat(brokenAgents, unloaded, droppedRules(stack, rules, switchedOff, opts))
	spec.Dropped = slices.Concat(brokenAgents, unloaded, droppedRules(stack, spec.Rules, switchedOff, opts))
	composeTask := func(task string) (turn.Composed, error) {
		return turn.Compose(taskSpec(spec, cmp.Or(opts.Dir, "."), task, say))
	}
	first := taskSpec(spec, cmp.Or(opts.Dir, "."), opts.Task, say)
	if subAgents.Prompt.Frameworks, err = rule.Frameworks(cmp.Or(opts.Dir, "."), nil); err != nil {
		say("no framework rule fires for a sub-agent from the project's own manifests: " + err.Error())
	}
	composed, err := turn.Compose(first)
	run.startup.step("frameworks and prompt")
	return composedRun{opts: opts, environment: environment, files: files, memory: remembered, instructions: instructions, composed: composed, composeTask: composeTask, subAgents: subAgents, skills: skills,
		shippedOff: stack.shippedOff, checksWork: slices.ContainsFunc(rules, func(loaded rule.Rule) bool { return loaded.ID == verifySubAgentsRule })}, err
}

func taskSpec(spec turn.ComposeSpec, dir, task string, say func(string)) turn.ComposeSpec {
	named := turn.TaskNamed(task).Paths
	var err error
	spec.Task = task
	if spec.Frameworks, err = rule.Frameworks(dir, named); err != nil {
		say("no framework rule fires this run: " + err.Error())
	}
	if project, err := rule.Frameworks(dir, nil); err == nil && len(named) > 0 {
		spec.PathFrameworks = slices.DeleteFunc(slices.Clone(spec.Frameworks), func(framework string) bool { return slices.Contains(project, framework) })
	}
	if !spec.Alone {
		return spec
	}
	if spec.Languages, err = rule.Languages(dir, named); err != nil {
		say("no language rule fires from the folders the task names: " + err.Error())
	}
	return spec
}

func droppedRules(stack ruleStack, kept []rule.Rule, switchedOff []rule.Overriding, opts wiring.RunOpts) []turn.PromptPart {
	var dropped []turn.PromptPart
	for _, one := range slices.Concat(stack.rules, stack.shippedOff) {
		if slices.ContainsFunc(kept, func(r rule.Rule) bool { return r.ID == one.ID }) || slices.ContainsFunc(switchedOff, func(off rule.Overriding) bool { return off.Base.ID == one.ID }) {
			continue
		}
		reason, why := rule.ReasonSetting, "the run's setting --tools three sends no "+one.ID
		switch {
		case one.Mode == rule.ModeOff && ruleSetting(one.ID) == "":
			reason, why = rule.ReasonMode, stack.switchOn(one.ID, cmp.Or(opts.Dir, "."))
		case one.Mode == rule.ModeOff:
			why = stack.switchOn(one.ID, cmp.Or(opts.Dir, "."))
		case opts.LeadAlone:
			why = "the run's lead alone setting: the task names few files in one folder, so the spawn rules are not sent"
		}
		dropped = append(dropped, turn.PromptPart{Concern: one.Concern, RuleID: one.ID, File: filepath.ToSlash(one.File), Why: why, Reason: reason, Mode: one.Mode, State: session.PartDropped})
	}
	return dropped
}

func runConfig(opts wiring.RunOpts, built []turn.Tool, run runtime) (turn.Config, *turn.SpawnTool, error) {
	orchestratorID, sessionID := cmp.Or(opts.TurnID, turn.NewID(time.Now())), cmp.Or(opts.Session, opts.TurnID, session.NewEventID())
	if opts.NoDocs {
		built = slices.DeleteFunc(slices.Clone(built), func(tool turn.Tool) bool { return tool.Name() == tools.DocsToolName })
	}
	if run.sessions != nil && opts.ToolSet != toolSetThree {
		built = append(slices.Clone(built), tools.NewQuote(run.sessions, sessionID))
	}
	if opts.ToolSet != toolSetThree {
		built = append(slices.Clone(built), tools.ScratchTools(run.leadAsks)...)
	}
	prompt, err := composeRun(opts, built, run)
	if err != nil {
		return turn.Config{}, nil, err
	}
	if len(prompt.skills) > 0 {
		built = append(slices.Clone(built), tools.NewSkill(prompt.skills))
	}
	opts, environment, composed := prompt.opts, prompt.environment, prompt.composed
	dir := cmp.Or(opts.Dir, ".")
	run.omitThinkingSummary = thinkingSummaryOff(opts, run.notify)
	references := map[string]string{}
	for _, reference := range composed.References {
		references[reference.Name] = reference.Text
	}
	if run.keys == nil {
		run.keys = &storedKeys{}
	}
	redactor := run.keys.redactor()
	config := turn.Config{
		Places:        run.places,
		Redactor:      &redactor,
		References:    references,
		Model:         run.model,
		Now:           run.now,
		Accounts:      run.applyThinkingSummary(run.accounts),
		Spend:         run.spend,
		Tools:         turn.NewRegistry(built...),
		Project:       dir,
		SessionSource: hook.SessionStartup,
		Task:          opts.Task,
		Wire:          opts.Wire,
		Slug:          run.orchestrator.Slug(),
		PromptCache:   run.orchestrator.PromptCache,
		System:        composed.Head(),
		Parts:         composed.Recorded(),
		Environment:   composed.WithTaskRules(environment),
		Instructions:  prompt.files,
		Memory:        prompt.memory,
		Caps: turn.Caps{
			MaxSteps:         opts.MaxSteps,
			LoopGuardRepeats: opts.LoopGuardRepeats,
			LoopGuardWindow:  opts.LoopGuardWindow,
		},
		ResultBytesCap: konst.TurnResultBytesCap,
		NoFork:         settingspkg.TextSaying(dir, settingspkg.Fork, run.notify) == settingspkg.Off,
		NoCompaction:   settingspkg.TextSaying(dir, settingspkg.AutoCompaction, run.notify) == settingspkg.Off,
		Budget:         run.budget,
		Sessions:       run.sessions,
		Session:        sessionID,
		Drops:          run.drops,
		Sift:           run.sift,
		Proxy:          loadProxySetting(opts.Dir).proxy,
		Inbox:          cmp.Or(run.inbox, turn.NewInbox()),
		Notify:         run.notify,
		GatePromptAsks: func() bool {
			return settingspkg.TextSaying(dir, settingspkg.GatePrompt, run.notify) == settingspkg.GatePromptAsk
		},
	}
	config.Inbox.ComposesEachTask(environment, prompt.composeTask)
	if run.plan != nil && opts.PlanEveryStep {
		config.Plan = run.plan.Shown
	}
	if run.gate != nil {
		config.Gate = run.gate
		config.GateMode = gateMode(opts.GateArm)
	}
	config.NewID = func() string { return orchestratorID }
	if run.scorer != nil {
		run.scorer.turnID = orchestratorID
	}
	if opts.ToolSet == toolSetThree && run.leadAsks == nil {
		config.Tools = turn.NewRegistry(built...)
		if run.cron != nil {
			config.Tools = run.leadTools(built)
		}
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
	asking := []turn.Tool{tools.RuleOverride{Ask: run.leadAsks, Running: running, Global: layers[0].dir, Project: layers[1].dir}, tools.AskPerson{}}
	if run.leadAsks != nil {
		asking[1] = askPersonTool(dir, run.places.Log, run.leadAsks, config.Inbox, run.keys.keyring(), run.notify)
	}
	if run.sessions != nil && memoryOn(dir) {
		asking = append(asking, tools.Remember{Store: run.sessions, Session: sessionID, Project: dir, Inbox: config.Inbox, Judge: memoryScopeJudge(dir, run.places.Log, run.keys.keyring(), run.notify),
			Auto: func() bool { on, _ := settingspkg.IntFor(dir, settingspkg.AutoMemory); return on != 0 }})
	}
	if boardManaged(dir) {
		store, err := boardy.OpenStore(dir)
		if err != nil {
			return turn.Config{}, nil, err
		}
		store.Speakers = map[string]boardy.Author{sessionID: *boardy.LeadAuthor(sessionID, "", "", config.Slug)}
		board := boardy.Managed{Local: boardy.Local{Store: store}, Person: string(boardy.RolePerson), Lead: sessionID}
		config.Board, config.TicketGrant, config.TicketTools = &board, tools.TicketGrant, tools.TicketTools
		leadTickets, err := tools.TicketTools(board, sessionID)
		if err != nil {
			return turn.Config{}, nil, err
		}
		asking = append(asking, leadTickets...)
		if run.foreseen != nil {
			*run.foreseen = foreseeLead(board, sessionID)
		} else {
			config.Inbox.FramesFirstRequest(leadAttached(board, sessionID, run))
		}
	}
	if opts.Agent == "" && episodesOn(dir) {
		zoom := &tools.Zoom{Project: dir, Compactor: func(ask turn.RecordedAsk) (memtree.Compact, memtree.CompactAll, func()) {
			if run.memory == nil {
				opened := openMemoryModel(dir, run.orchestrator)
				one, all := opened.compactors(ask)
				return one, all, opened.release
			}
			one, all := run.memory.model(dir, run.orchestrator).compactors(ask)
			return one, all, func() {}
		}}
		asking = append(asking, zoom, tools.Recall{Zoom: zoom, Sessions: run.sessions, Session: sessionID})
	}
	if opts.NoSubAgents || opts.ToolSet == toolSetThree {
		config.Tools = run.leadTools(append(slices.Clone(built), asking...))
		return config, nil, nil
	}
	subAgentBase := config
	subAgentBase.Instructions, subAgentBase.Memory = "", ""
	spawner := turn.NewSpawnTool(orchestratorID, subAgentBase, cmp.Or(run.roster, &subagent.Roster{}))
	spawner.Inbox, spawner.SubAgents, spawner.Project, spawner.ChecksWork = config.Inbox, prompt.subAgents, dir, prompt.checksWork
	run.current = func(held subagent.Definition) subagent.Definition {
		now := scanSubAgents(dir, built).Definitions
		runs := func(one subagent.Definition) bool {
			return one.Name == held.Name && (one.Runs == subagent.RunsModel || one.Runs == subagent.RunsInherit)
		}
		if at := slices.IndexFunc(now, runs); at >= 0 {
			held.Model, held.Effort = now[at].Model, now[at].Effort
		}
		return held
	}
	spawner.SubAgents.Open, spawner.SubAgents.Takes = run.subAgentOpener(opts), run.subAgentTakes(opts)
	if run.plan != nil && settingspkg.TextSaying(dir, settingspkg.PlanAdvisor, run.notify) == settingspkg.AdvisorShadow {
		run.plan.Advise = spawner.Advise
	}
	if opts.Agent == "" && memoryOn(dir) {
		spawner.Remembered = func() (map[string]string, error) {
			shelves, err := memory.Open(dir)
			lines := map[string]string{}
			for _, entry := range shelves.All() {
				lines[entry.ID] = entry.Line()
			}
			return lines, err
		}
	}
	learn := learnBrowserRecipe(run.notify)
	spawner.SubAgents.Brief = browserRecipeBrief(run.notify)
	spawner.SubAgents.Ended = func(definition subagent.Definition, task string, rounds []turn.Row, report turn.SubAgentReport, finished bool) {
		learn(definition, task, rounds, report, finished)
		run.tabs.CloseOpenedBy(report.ID)
	}
	spawner.Limits = func() turn.SubAgentLimits {
		store, err := settingspkg.OpenFor(dir)
		if err != nil {
			if run.notify != nil {
				run.notify("the sub-agent limits fell back to their defaults because the settings file could not be read: " + err.Error())
			}
			store, _ = settingspkg.Open("", "")
		}
		return turn.SubAgentLimits{Running: store.Int(settingspkg.SubAgentsPerTurn), Depth: store.Int(settingspkg.SubAgentDepth),
			CheckIn: time.Duration(store.Int(settingspkg.SubAgentCheckSeconds)) * time.Second, CacheTTL: store.Text(settingspkg.SubAgentCache)}
	}
	own := slices.DeleteFunc(slices.Clone(built), func(tool turn.Tool) bool { return slices.Contains(leadNeverCalls(), tool.Name()) })
	if settingspkg.TextSaying(dir, settingspkg.BrowserDriver, run.notify) == settingspkg.DriverSubagent {
		own = slices.DeleteFunc(own, func(tool turn.Tool) bool { return strings.HasPrefix(tool.Name(), "browser_") })
	}
	if !nodeProject(dir) {
		own = slices.DeleteFunc(own, func(tool turn.Tool) bool { return tool.Name() == "typecheck" || tool.Name() == "test" })
	}
	orchestrating := append(turn.WithSourceBudget(append(own, spawner), prompt.subAgents.Defined), asking...)
	if run.gate != nil {
		spawner.SettingsTool = true
		orchestrating = append(orchestrating, tools.NewSettings(settingspkg.Paths(dir)))
	}
	config.Tools = run.leadTools(orchestrating)
	return config, spawner, nil
}

func leadAttached(board boardy.Managed, session string, run runtime) string {
	if run.sessions != nil {
		if begun, _ := sys.Exists(run.sessions.EventsPath(session)); begun {
			return ""
		}
	}
	sizes := board.Store.EventSizes()
	err := eachLedBoard(board, session, func(key string) error {
		return board.Fire(rule.WhenAfter, rule.HookLeadAttach, key, "", session)
	})
	return turn.BoardNudges(board.Store, sizes) + attachFailed(err)
}

type leadForeseen struct {
	attached, notified string
	effects            []string
}

func foreseeLead(board boardy.Managed, session string) leadForeseen {
	var all boardy.Foreseen
	err := eachLedBoard(board, session, func(key string) error {
		one, err := board.Foresee(rule.HookLeadAttach, key, "", session)
		all.Nudges, all.Notifies, all.Effects = append(all.Nudges, one.Nudges...), append(all.Notifies, one.Notifies...), append(all.Effects, one.Effects...)
		return err
	})
	return leadForeseen{attached: turn.NudgesText(all.Nudges) + attachFailed(err), notified: turn.UnreadBoardNotifies(&board, all.Notifies...), effects: all.Effects}
}

func eachLedBoard(board boardy.Managed, session string, do func(key string) error) error {
	boards, err := board.Store.Boards()
	for _, managed := range boards {
		if board.Manages(managed.Key, session, "") == nil {
			err = errors.Join(err, do(managed.Key))
		}
	}
	return err
}

func attachFailed(err error) string {
	if err == nil {
		return ""
	}
	return "\n\na board hook on lead.attach failed: " + err.Error()
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
	names := []string{(&turn.SpawnTool{}).Name(), turn.SkillToolName}
	for _, tool := range built {
		names = append(names, tool.Name())
	}
	known := append(slices.Clone(names), "fetch", "web_search")
	for _, driver := range []string{settingspkg.DriverSteps, settingspkg.DriverGoal} {
		every, _ := tools.NewBrowser(tools.BrowserSettings{Mode: settingspkg.BrowserDrive, Driver: driver})
		for _, tool := range every {
			known = append(known, tool.Name())
		}
	}
	dir = cmp.Or(dir, ".")
	catalog, _ := wiring.ModelLibrary(dir)
	home, _ := os.UserHomeDir()
	library, _, _ := librarySource()
	sources, tiers := settingspkg.DeclaredDefaultText(settingspkg.AgentSources), map[subagent.Tier]string{}
	if store, err := settingspkg.OpenFor(dir); err == nil {
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

func gateMode(arm string) turn.GateMode {
	switch arm {
	case gateEnforce, gateFollowsTheRule:
		return turn.GateEnforce
	case gateShadow, gateOff:
		return turn.GateShadow
	}
	panic("tofu run: unknown gate arm " + arm)
}

func dryRunBody(opts wiring.RunOpts, selected models.Model, config turn.Config) ([]byte, error) {
	tools := config.Tools.Definitions()
	messages := config.MemoryMessage()
	if dir := cmp.Or(opts.Dir, "."); opts.Agent == "" && episodesOn(dir) {
		view, _, err := memory.EpisodeView(dir, konst.MemtreeViewBytes, nil, nil)
		if err != nil {
			return nil, err
		}
		if view != "" {
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: view})
		}
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: config.FirstUserMessage()})
	summaryOff := thinkingSummaryOff(opts, nil)
	switch opts.Wire {
	case wiring.WireKey:
		system := append([]llm.Message{{Role: llm.RoleSystem, Content: config.SystemMessage()}}, messages...)
		return llm.Request{Messages: system, Tools: tools}.Encode(selected.ID)
	case wiring.WireCodex, wiring.WireMeta:
		return codex.Request{Model: selected.ID, Instructions: config.SystemMessage(), Messages: messages, Tools: tools, Effort: opts.Effort, SummaryOff: summaryOff,
			Verbosity: string(selected.Verbosity), ReasoningContext: string(selected.ReasoningContext)}.Encode(nil)
	}
	versions, _ := subFingerprint(opts.Dir)
	return anthropic.Request{Model: selected.ID, System: []string{config.SystemMessage()}, Messages: messages, Tools: tools, Effort: opts.Effort, ClaudeCodeVersion: versions.ClaudeCode, OmitThinkingSummary: summaryOff}.Encode(true)
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
	facts := keyFactsOf(roleLLM, openRouterName)
	key, err := jev.KeyFor(sys.CredentialFileName, facts.variable)
	if err != nil {
		return nil, fmt.Errorf("no %s %s key, run %s: %w", facts.name, facts.role, facts.hint(), err)
	}
	wire, err := openrouter.New(openrouter.Config{
		Endpoint:  os.Getenv(openRouterEndpointVariable),
		Model:     model,
		Key:       key,
		KeyName:   facts.variable,
		Transport: turnTransportConfig(),
	})
	if err != nil {
		return nil, err
	}
	return llm.NewClient(wire)
}

type codexTurn struct {
	wire             *codex.Wire
	effort           llm.Effort
	verbosity        models.Verbosity
	reasoningContext models.ReasoningContext
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
		Turn:             request.Turn,
		Instructions:     strings.Join(instructions, "\n\n"),
		Messages:         messages,
		Tools:            request.Tools,
		ToolChoice:       request.ToolChoice.OpenAIValue(),
		Effort:           c.effort,
		SummaryOff:       request.OmitThinkingSummary,
		Verbosity:        string(c.verbosity),
		ReasoningContext: string(c.reasoningContext),
		OnThinking:       request.OnThinking,
		OnRetry:          request.OnRetry,
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

func runSystem(opts wiring.RunOpts) string {
	if opts.ToolSet == toolSetThree {
		return turn.EveryToolIsRelativeToTheWorkingDirectory +
			"read reads a whole file, write creates one or replaces it whole, and bash runs anything else, " +
			"including finding a file, searching text and changing part of a file."
	}
	system := strings.TrimSpace(turn.EveryToolIsRelativeToTheWorkingDirectory)
	if !opts.NoDocs {
		system += " " + docsSentence
	}
	if opts.NoSubAgents || opts.LeadAlone {
		return system
	}
	system += " " + turn.SpawnAddendum
	if opts.SubAgentList != "" {
		system += "\n\n" + opts.SubAgentList
	}
	return system
}

func readsWhen(readBeforeEdit bool, reads *turn.ReadLedger) *turn.ReadLedger {
	if !readBeforeEdit {
		return nil
	}
	return reads
}

type storedKeys struct {
	mu       sync.Mutex
	stamp    string
	read     bool
	keys     jev.Keyring
	redacted sys.KeyRedactor
}

func credentialStoreStamp() string {
	path, err := sys.CredentialStorePath()
	if err != nil {
		return ""
	}
	stamp := ""
	for _, file := range []string{path, path + "-wal"} {
		if info, err := os.Stat(file); err == nil {
			stamp += info.ModTime().String() + " " + strconv.FormatInt(info.Size(), 10) + "; "
		}
	}
	return stamp
}

func (s *storedKeys) current() (jev.Keyring, sys.KeyRedactor) {
	stamp := credentialStoreStamp()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.read || stamp != s.stamp {
		s.keys = jev.ReadKeyring()
		s.redacted = sys.KeyRedactorOf(s.keys.Stored)
		s.stamp, s.read = stamp, true
	}
	return s.keys, s.redacted
}

func (s *storedKeys) keyring() jev.Keyring {
	keys, _ := s.current()
	return keys
}

func (s *storedKeys) redactor() sys.KeyRedactor {
	_, redactor := s.current()
	return redactor
}

type warmProcesses struct {
	checkers *turn.Typecheckers
	tests    *turn.TestRunners
	tabs     *tools.BrowserTabs
	keys     storedKeys
	memory   sessionMemoryModel
	drops    *turn.DropIndex
}

func newWarmProcesses(tabs *tools.BrowserTabs) *warmProcesses {
	return &warmProcesses{checkers: turn.NewTypecheckers(), tests: turn.NewTestRunners(), tabs: tabs, drops: turn.NewDropIndex()}
}

func (w *warmProcesses) Close() {
	if w != nil {
		w.checkers.Close()
		w.tests.Close()
		w.tabs.Close()
		w.memory.close()
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
	rules, _, rulesErr := loadRules("", dir)
	if rulesErr != nil {
		return nil, nil, rulesErr
	}
	read := readTool.Reading(ledger)
	write := writeTool.Reading(ledger).Checking(checkers).Ruling(rules)
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
	webTools, webErr := buildWebTools(dir, warm.keys.keyring())
	home, _ := os.UserHomeDir()
	settingsDir := cmp.Or(dir, ".")
	browserTools, browserErr := tools.NewBrowser(tools.BrowserSettings{
		Home:     home,
		Profile:  settingspkg.TextSaying(settingsDir, settingspkg.BrowserProfile, nil),
		Headless: settingspkg.IntSaying(settingsDir, settingspkg.BrowserHeadless, nil) != 0,
		Mode:     settingspkg.TextSaying(settingsDir, settingspkg.Browser, nil),
		Driver:   settingspkg.TextSaying(settingsDir, settingspkg.BrowserDriver, nil),
		Steps:    settingspkg.IntSaying(settingsDir, settingspkg.BrowserSteps, nil),
		Judge:    func() (jevloop.Jev, error) { return wiring.BrowserJudge(dir) },
		Model:    func() (turn.Model, string, error) { return browserModel(settingsDir) },
		Tabs:     warm.tabs,
	})
	if err := cmp.Or(webErr, browserErr); err != nil {
		return nil, nil, err
	}
	plan := tools.NewPlan()
	full := append([]turn.Tool{read, write, shell, plan, tools.Shells{}, projectTool, globTool, searchTool, symbolsTool, editTool.Reading(ledger).Checking(checkers).Ruling(rules), typecheckTool, testTool, githubTool}, verbTools...)
	return tools.NewMemo().Wrap(append(append(full, webTools...), browserTools...)), plan, nil
}

func planAsOffered(model models.Model, built []turn.Tool, plan *tools.Plan) ([]turn.Tool, *tools.Plan) {
	if model.Plan != models.PlanWithheld {
		return built, plan
	}
	return slices.DeleteFunc(built, func(tool turn.Tool) bool { return tool.Name() == tools.PlanToolName }), nil
}

type subscriptionModel struct{ opts wiring.RunOpts }

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
		if slug := strings.TrimSpace(settingspkg.TextSaying(dir, key, nil)); slug != "" {
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
	library, err := wiring.ModelLibrary(dir)
	if err != nil {
		return nil, "", err
	}
	model, err := library.Select(slug)
	if err != nil {
		return nil, "", err
	}
	opts := wiring.RunOpts{Dir: dir, Wire: library.WireOf(model), Model: slug}
	if len(model.Efforts) > 0 {
		opts.Effort = wiring.DefaultEffort(model.Efforts)
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
	if settingspkg.TextSaying(dir, settingspkg.BrowserDriver, nil) != settingspkg.DriverSubagent {
		found.Definitions = slices.DeleteFunc(found.Definitions, isBrowserAgent)
	}
	return found
}

func onBrowserModel(dir string, found subagent.Found, catalog models.Library) subagent.Found {
	at := slices.IndexFunc(found.Definitions, isBrowserAgent)
	slug, key := browserSlug(dir)
	wanted := llm.Effort(settingspkg.TextSaying(dir, settingspkg.BrowserEffort, nil))
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
		agent.Effort = wiring.DefaultEffort(model.Efforts)
	}
	for _, effort := range []llm.Effort{written, wanted} {
		if slices.Contains(model.Efforts, effort) {
			agent.Effort = effort
		}
	}
	return found
}

const browserAgent = "browser"

func buildWebTools(dir string, keys jev.Keyring) ([]turn.Tool, error) {
	layers, err := web.Layers(shipped.Files(), dir)
	if err != nil {
		return nil, err
	}
	config, err := web.LoadKeyed(layers, keys)
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

func keySpend(row turn.Row, askedAs string) string {
	reported := fmt.Sprintf("spend api key $%.6f", row.TotalCostUSD)
	if row.TotalCostUSD > 0 {
		return reported
	}
	registry, _ := modelRegistry()
	price, priced := registry.Prices[askedAs]
	if !priced {
		return reported
	}
	var estimate float64
	for _, step := range row.Steps {
		estimate += listUSD(price, step.PromptTokens, step.CacheReadTokens, step.CompletionTokens)
	}
	return fmt.Sprintf("spend api key estimated $%.6f at list price", estimate)
}

func printRunRow(out io.Writer, row turn.Row, askedAs, windows string) {
	spend := "spend subscription windows " + windows + ", no money"
	if row.Spend != turn.SpendSubscription {
		spend = keySpend(row, askedAs)
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
	if said, ended := host.WordsAfterLastCalls(row); ended {
		_, _ = fmt.Fprintf(out, "step %d: assistant_text %q\n", row.Steps[len(row.Steps)-1].Index, said)
	}
}

func subAgentRows(spawner *turn.SpawnTool) []turn.Row {
	if spawner == nil {
		return nil
	}
	return spawner.SubAgentRows()
}

func runTotals(rows []turn.Row) (requests, calls, unanswered int) {
	for _, row := range rows {
		requests += len(row.Steps)
		for _, step := range row.Steps {
			calls += len(step.ToolCalls)
			for _, call := range step.ToolCalls {
				if call.Error != "" && (call.GateVerdict == string(ledger.VerdictAsk) || call.GateError != "") {
					unanswered++
				}
			}
		}
	}
	return requests, calls, unanswered
}

func runFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu run: %v\n", err)
	return output.ExitUsage
}

func parseRunArgs(args []string) (wiring.RunOpts, error) {
	opts := wiring.RunOpts{
		ToolSet:          toolSetFull,
		LoopGuardRepeats: konst.TurnLoopGuardRepeats,
		LoopGuardWindow:  konst.TurnLoopGuardWindow,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch arg {
		case "--dir":
			opts.Dir, err = output.NextArg(args, &i, arg)
		case "--session":
			opts.Resume, err = output.NextArg(args, &i, arg)
		case "--dry-run":
			opts.DryRun = true
		case "--show-prompt":
			opts.ShowPrompt = true
		case "--agent":
			opts.Agent, err = output.NextArg(args, &i, arg)
		case "--no-gate":
			opts.GateArm = gateOff
		case "--gate":
			opts.GateArm, err = output.NextArg(args, &i, arg)
		case "--sift":
			opts.SiftArm, err = output.NextArg(args, &i, arg)
		case "--no-subagents":
			opts.NoSubAgents = true
		case "--no-instructions":
			opts.NoInstructions = true
		case "--no-docs":
			opts.NoDocs = true
		case "--wire":
			opts.Wire, err = output.NextArg(args, &i, arg)
		case "--tools":
			opts.ToolSet, err = output.NextArg(args, &i, arg)
		case "--done-review":
			opts.DoneArm, err = output.NextArg(args, &i, arg)
		case "--model":
			if opts.Model, err = output.NextArg(args, &i, arg); err == nil && strings.TrimSpace(opts.Model) == "" {
				err = errors.New("--model needs a model id")
			}
		case "--effort":
			var raw string
			if raw, err = output.NextArg(args, &i, arg); err == nil {
				opts.Effort, err = llm.ParseEffort(raw)
			}
		case "--thinking-summary":
			if opts.ThinkingSummary, err = output.NextArg(args, &i, arg); err == nil && !slices.Contains(thinkingSummaries(), opts.ThinkingSummary) {
				err = fmt.Errorf("--thinking-summary %q is none of %s", opts.ThinkingSummary, strings.Join(thinkingSummaries(), ", "))
			}
		case "--max-steps":
			opts.MaxSteps, err = nextInt(args, &i, arg)
		case "--loop-guard-repeats":
			opts.LoopGuardRepeats, err = nextInt(args, &i, arg)
		case "--loop-guard-window":
			opts.LoopGuardWindow, err = nextInt(args, &i, arg)
		case "--context-ceiling":
			if opts.ContextCeiling, err = nextInt(args, &i, arg); err == nil && opts.ContextCeiling <= 0 {
				err = fmt.Errorf("--context-ceiling %d takes a count of tokens above zero, as in --context-ceiling 20000", opts.ContextCeiling)
			}
		default:
			switch {
			case strings.HasPrefix(arg, "--"):
				err = fmt.Errorf("unknown argument %q", arg)
			case opts.Task != "":
				err = fmt.Errorf("tofu run takes one task, got %q and %q", opts.Task, arg)
			default:
				opts.Task = arg
			}
		}
		if err != nil {
			return wiring.RunOpts{}, err
		}
	}
	if opts.Dir == "" {
		return wiring.RunOpts{}, errors.New("--dir is required: tofu run never defaults to the current directory, because its bash tool is not sandboxed")
	}
	if strings.TrimSpace(opts.Task) == "" {
		return wiring.RunOpts{}, errors.New("tofu run needs a task")
	}
	if opts.Resume != "" && (opts.DryRun || opts.ShowPrompt) {
		return wiring.RunOpts{}, errors.New("--session sends the next turn of a session, and --dry-run and --show-prompt send nothing")
	}
	if opts.Agent != "" && !opts.ShowPrompt {
		return wiring.RunOpts{}, errors.New("--agent prints the prompt a spawn of that sub-agent composes and runs nothing, so it needs --show-prompt")
	}
	if opts.Wire != "" && !slices.Contains(wiring.RunWires(), opts.Wire) {
		return wiring.RunOpts{}, fmt.Errorf("--wire %q is none of %s", opts.Wire, strings.Join(wiring.RunWires(), ", "))
	}
	source, _, _ := strings.Cut(opts.Model, "/")
	if source == wiring.WireMeta {
		if slices.Contains(args, "--wire") {
			return wiring.RunOpts{}, fmt.Errorf("--model %s is paid by the %s key and reaches its own wire, so it takes no --wire", opts.Model, wiring.WireMeta)
		}
		opts.Wire = wiring.WireMeta
	}
	if opts.Model != "" && !slices.Contains(args, "--wire") {
		if library, err := wiring.ModelLibrary(""); err == nil {
			opts.Wire = cmp.Or(library.WireFor(models.Subscription(source)), opts.Wire)
		}
	}
	if opts.Wire == wiring.WireKey && opts.Effort != "" {
		return wiring.RunOpts{}, fmt.Errorf("--effort %s with --wire %s: the openrouter wire sends no reasoning effort, so the level would be dropped without a word",
			opts.Effort, wiring.WireKey)
	}
	if opts.Wire != wiring.WireKey {
		opts.Effort = cmp.Or(opts.Effort, llm.EffortDefault)
	}
	if opts.GateArm != gateFollowsTheRule && !slices.Contains(gateArms(), opts.GateArm) {
		return wiring.RunOpts{}, fmt.Errorf("--gate %q is none of %s: with no --gate jev's verdict is enforced",
			opts.GateArm, strings.Join(gateArms(), ", "))
	}
	if opts.SiftArm != siftFollowsTheTable && !slices.Contains(siftArms(), opts.SiftArm) {
		return wiring.RunOpts{}, fmt.Errorf("--sift %q is none of %s: with no --sift the method table decides",
			opts.SiftArm, strings.Join(siftArms(), ", "))
	}
	if opts.DoneArm != "" && !slices.Contains(settingspkg.DoneReviews(), opts.DoneArm) {
		return wiring.RunOpts{}, fmt.Errorf("--done-review %q is none of %s: with no --done-review the %s setting decides", opts.DoneArm, strings.Join(settingspkg.DoneReviews(), ", "), settingspkg.DoneReview)
	}
	switch opts.ToolSet {
	case toolSetFull, toolSetThree:
	default:
		return wiring.RunOpts{}, fmt.Errorf("--tools %q is neither %s nor %s, the arm that offers read, write and bash alone",
			opts.ToolSet, toolSetFull, toolSetThree)
	}
	without := spawnlessFlag(opts)
	if opts.DoneArm != "" && opts.DoneArm != settingspkg.DoneReviewOff && without != "" {
		return wiring.RunOpts{}, fmt.Errorf("--done-review %s with %s: that arm has no spawn tool, so no sub-agent is ever reviewed and the flag would say a check is running that is not",
			opts.DoneArm, without)
	}
	if opts.Agent != "" && without != "" {
		return wiring.RunOpts{}, fmt.Errorf("--agent %s with %s: that arm spawns no sub-agent, so there is no spawn prompt to show", opts.Agent, without)
	}
	return opts, nil
}

func spawnlessFlag(opts wiring.RunOpts) string {
	if opts.NoSubAgents {
		return "--no-subagents"
	}
	if opts.ToolSet == toolSetThree {
		return "--tools " + toolSetThree
	}
	return ""
}

func nextInt(args []string, i *int, flag string) (int, error) {
	raw, err := output.NextArg(args, i, flag)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number", flag, raw)
	}
	return n, nil
}

func memoryOn(dir string) bool {
	on, _ := settingspkg.IntFor(dir, settingspkg.Memory)
	return on != 0
}

func episodesOn(dir string) bool {
	return settingspkg.TextSaying(dir, settingspkg.Episodes, nil) == settingspkg.EpisodesOn && memoryOn(dir)
}
