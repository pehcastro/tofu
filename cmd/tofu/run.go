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
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/llm/wire/openrouter"
	"tofu/internal/prompt"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
	"tofu/internal/web"
	shipped "tofu/library"
)

const (
	wireSubscription = "anthropic"
	wireCodex        = "codex"
	wireKey          = openrouter.Name

	toolSetFull  = "full"
	toolSetThree = "three"

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
	if wire == wireKey {
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
	now          func() time.Time
	open         func(runOpts) (appWire, error)
	wrapSubAgent func(turn.Model) (turn.Model, error)
	orchestrator models.Model
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

func boundSubAgent(opts runOpts) (string, error) {
	if opts.wire == wireKey {
		return "", nil
	}
	bound, err := boundRoles(opts.wire, opts.dir)
	if err != nil || bound[models.RoleSubAgent].By != models.BoundByFile {
		return "", err
	}
	return bound[models.RoleSubAgent].Model.Slug(), nil
}

func (r runtime) subAgentOpener(opts runOpts) func(subagent.Definition) (turn.SubAgentModel, error) {
	if r.open == nil {
		return nil
	}
	return func(definition subagent.Definition) (turn.SubAgentModel, error) {
		subAgent := opts
		subAgent.effort = cmp.Or(definition.Effort, opts.effort)
		named := definition.Model
		if definition.Name == "" {
			bound, err := boundSubAgent(opts)
			if err != nil {
				return turn.SubAgentModel{}, err
			}
			named = bound
		}
		asked := cmp.Or(named, r.orchestrator.Slug())
		if named == "" && subAgent.effort == opts.effort {
			return turn.SubAgentModel{Slug: asked, Windows: r.orchestrator.WindowText()}, nil
		}
		if named != "" {
			library, err := modelLibrary(opts.dir)
			if err != nil {
				return turn.SubAgentModel{}, err
			}
			model, err := library.Select(named)
			if err != nil {
				return turn.SubAgentModel{}, err
			}
			subAgent.model, subAgent.wire = named, library.WireFor(model.Subscription)
		}
		if subAgent.wire == wireKey {
			return turn.SubAgentModel{}, fmt.Errorf("%s asks for effort %s, and the openrouter wire sends no reasoning effort", definition.Name, subAgent.effort)
		}
		opened, err := r.open(subAgent)
		if err != nil {
			if opened.held != nil {
				opened.held.close()
			}
			return turn.SubAgentModel{}, err
		}
		opened.held.wrap = r.wrapSubAgent
		return turn.SubAgentModel{Slug: asked, Windows: opened.selected.WindowText(), Wire: subAgent.wire, Spend: opened.spend, Accounts: opened.held.forTurn(), Close: opened.held.close}, nil
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
  --model <id>          the orchestrator's model, otherwise the one the orchestrator role is bound to
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
                        edit or a write to a file this turn has not read;
                        there is no flag for it, set it with
                        tofu settings set readBeforeEdit false
  --no-instructions     the arm that %s
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

	built, _, err := buildRunToolsForRun(opts.dir, opts.toolSet, opts.readBeforeEdit, shell)
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
	return runTurn(opts, selected, built, budget, out, errOut)
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

func runTurn(opts runOpts, selected models.Model, built []turn.Tool, budget recall.Budget, out, errOut io.Writer) int {
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

	sessions, err := session.Open()
	if err != nil {
		return runFail(errOut, err)
	}

	config, spawner, err := runConfig(opts, built, runtime{accounts: opened.held.forTurn(), spend: opened.spend, budget: budget, gate: gate, sift: sifter, scorer: scorer, sessions: sessions, notify: writeNotice(errOut),
		open: open, wrapSubAgent: wrap, orchestrator: selected})
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
	row, runErr := turn.Run(turn.WithShellRegistry(context.Background(), registry), config)
	leaveShells(registry)
	printRunRow(out, row, selected.Slug(), selected.WindowText())
	for _, subAgent := range subAgentRows(spawner) {
		askedAs, windows := selected.Slug(), selected.WindowText()
		for _, spawned := range spawner.Spawned() {
			if subAgent.ID == spawned.ID || (spawned.Call != "" && subAgent.SpawnedBy == spawned.Call) {
				askedAs, windows = spawned.Slug, spawned.Windows
			}
		}
		printRunRow(out, subAgent, askedAs, windows)
	}
	if row.Session != "" {
		if headErr := sessions.SetHead(row.Session); headErr != nil {
			_, _ = fmt.Fprintf(errOut, "tofu run: pointing the head at %s: %v\n", row.Session, headErr)
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
	sources := strings.Split(settingText(opts.dir, settingspkg.InstructionSources, nil), ",")
	written, cut, skipped := turn.ProjectInstructionsInOrder(opts.dir, home, capBytes, sources)
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
		notices = append(notices, fmt.Sprintf("skipped %s: change the order with tofu settings set %s", strings.Join(skipped, "; "), settingspkg.InstructionSources))
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
}

func composeRun(opts runOpts, built []turn.Tool, run runtime) (composedRun, error) {
	found := make(chan subagent.Found, 1)
	if !opts.noSubAgents && opts.toolSet != toolSetThree && run.open != nil {
		go func() { found <- scanSubAgents(opts.dir, built) }()
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
	for _, definition := range discovered.Definitions {
		for _, notice := range definition.Notices {
			say(notice)
		}
	}
	rules, _, err := loadRules("")
	if err != nil {
		return composedRun{}, err
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
	subAgents := turn.SubAgents{Defined: discovered.Definitions, Prompt: turn.ComposeSpec{Environment: environment, ToolGuidance: runSystem(opts), Rules: rules, Skills: skills, WindowTokens: run.budget.WindowTokens}}
	spec := subAgents.Prompt
	spec.Task = opts.task
	if opts.agent != "" {
		if spec.Agent, err = subAgents.Named(opts.agent); err != nil {
			return composedRun{}, err
		}
	} else if !opts.noSubAgents && opts.toolSet != toolSetThree {
		spec.Role = rule.RoleOrchestrator
	}
	composed, err := turn.Compose(spec)
	return composedRun{opts: opts, environment: environment, instructions: instructions, composed: composed, subAgents: subAgents, skills: skills}, err
}

func runConfig(opts runOpts, built []turn.Tool, run runtime) (turn.Config, *turn.SpawnTool, error) {
	orchestratorID, sessionID := cmp.Or(opts.turnID, turn.NewID(time.Now())), cmp.Or(opts.session, opts.turnID, session.NewEventID())
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
	config := turn.Config{
		Model:       run.model,
		Now:         run.now,
		Accounts:    run.accounts,
		Spend:       run.spend,
		Tools:       turn.NewRegistry(built...),
		Task:        opts.task,
		Wire:        opts.wire,
		System:      composed.Head(),
		Environment: composed.WithTaskRules(environment),
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
	}
	if run.gate != nil {
		config.Gate = run.gate
		config.GateMode = gateMode(opts.gateArm, settingText(cmp.Or(opts.dir, "."), settingspkg.GatePrompt, run.notify))
	}
	config.NewID = func() string { return orchestratorID }
	if run.scorer != nil {
		run.scorer.turnID = orchestratorID
	}
	if opts.noSubAgents || opts.toolSet == toolSetThree {
		return config, nil, nil
	}
	spawner := turn.NewSpawnTool(orchestratorID, config, cmp.Or(run.roster, &subagent.Roster{}))
	spawner.SubAgents = prompt.subAgents
	spawner.SubAgents.Open = run.subAgentOpener(opts)
	config.Tools = turn.NewRegistry(append(turn.WithSourceBudget(built, prompt.subAgents.Defined), spawner)...)
	return config, spawner, nil
}

func scanSubAgents(dir string, built []turn.Tool) subagent.Found {
	names := []string{(&turn.SpawnTool{}).Name()}
	for _, tool := range built {
		names = append(names, tool.Name())
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
	return subagent.Definitions(subagent.Scan{
		Project: dir,
		Home:    home,
		Sources: strings.Split(sources, ","),
		Library: library,
		Tools:   names,
		Catalog: catalog,
		Tiers:   tiers,
	})
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
	case wireCodex:
		return codex.Request{Model: model, Instructions: config.System, Messages: messages, Tools: tools, Effort: opts.effort}.Encode(nil)
	}
	return anthropic.Request{Model: model, System: []string{config.System}, Messages: messages, Tools: tools, Effort: opts.effort}.Encode(true)
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
		Effort:       c.effort,
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
		Usage:            llm.Usage{InputTokens: result.Usage.Input, OutputTokens: result.Usage.Output},
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

func runSystem(opts runOpts) string {
	if opts.toolSet == toolSetThree {
		return turn.EveryToolIsRelativeToTheWorkingDirectory +
			"read reads a whole file, write creates one or replaces it whole, and bash runs anything else, " +
			"including finding a file, searching text and changing part of a file."
	}
	system := turn.EveryToolIsRelativeToTheWorkingDirectory + prompt.PreferTheToolOverTheShell
	if opts.noSubAgents {
		return system
	}
	system += " " + turn.SpawnAddendum
	if opts.subAgentList != "" {
		system += "\n\n" + opts.subAgentList
	}
	return system
}

func buildRunToolsForRun(dir, set string, readBeforeEdit bool, shell turn.RunShell) ([]turn.Tool, *tools.Plan, error) {
	bashTool, bashErr := turn.NewBashToolFromShell(dir, shell)
	if bashErr != nil {
		return nil, nil, bashErr
	}
	return assembleRunTools(dir, set, readBeforeEdit, bashTool)
}

func assembleRunTools(dir, set string, readBeforeEdit bool, bashTool *turn.BashTool) ([]turn.Tool, *tools.Plan, error) {
	readTool, readErr := turn.NewReadTool(dir)
	writeTool, writeErr := turn.NewWriteTool(dir)
	if err := cmp.Or(readErr, writeErr); err != nil {
		return nil, nil, err
	}
	var ledger *turn.ReadLedger
	if readBeforeEdit {
		ledger = turn.NewReadLedger()
	}
	read := readTool.Reading(ledger)
	write := writeTool.Reading(ledger)
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
	if err := cmp.Or(globErr, searchErr, symbolsErr, editErr, projectErr, verbErr, githubErr); err != nil {
		return nil, nil, err
	}
	webTools, webErr := buildWebTools(dir)
	if webErr != nil {
		return nil, nil, webErr
	}
	plan := tools.NewPlan()
	full := append([]turn.Tool{read, write, shell, plan, tools.Shells{}, projectTool, globTool, searchTool, symbolsTool, editTool.Reading(ledger), githubTool}, verbTools...)
	return tools.NewMemo().Wrap(append(full, webTools...)), plan, nil
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
