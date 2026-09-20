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

	"tofu/internal/crew"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/policy"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/llm/wire/openrouter"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const (
	wireSubscription = "anthropic"
	wireCodex        = "codex"
	wireKey          = "openrouter"

	toolSetFull  = "full"
	toolSetThree = "three"

	gateFollowsThePolicy = ""
	gateOff              = "off"
	gateShadow           = "shadow"
	gateEnforce          = "enforce"

	openRouterDefaultModel = "anthropic/claude-opus-5"
)

func runWires() []string { return []string{wireSubscription, wireCodex, wireKey} }

func wireSpend(wire string) turn.Spend {
	if wire == wireKey {
		return turn.SpendAPIKey
	}
	return turn.SpendSubscription
}

type childRole struct {
	wire    string
	id      string
	windows string
	model   turn.Model
	spend   turn.Spend
}

type runOpts struct {
	dir            string
	task           string
	turnID         string
	wire           string
	dryRun         bool
	gateArm        string
	noCrew         bool
	doneArm        string
	model          string
	toolSet        string
	maxSteps       int
	maxDecisions   int
	contextCeiling int
	child          childRole
}

type runtime struct {
	model    turn.Model
	spend    turn.Spend
	budget   recall.Budget
	gate     *toolGate
	sessions *session.Store
}

func boundRoles(wire string) (models.Bindings, error) {
	catalog, err := modelCatalog()
	if err != nil {
		return nil, err
	}
	spec, carried := catalog.ForWire(wire)
	if !carried {
		return nil, outsideTheCatalog(catalog, wire)
	}
	return catalog.Bind(spec.ID)
}

func chooseModel(opts runOpts) (models.Model, error) {
	if opts.wire == wireKey {
		return models.Model{ID: cmp.Or(opts.model, openRouterDefaultModel)}, nil
	}
	if opts.model != "" {
		return selectModel(opts.wire, opts.model)
	}
	bound, err := boundRoles(opts.wire)
	if err != nil {
		return models.Model{}, err
	}
	forTurn := bound[models.RoleTurn]
	if forTurn.Wire != opts.wire {
		return models.Model{}, fmt.Errorf(
			"%s, and --wire %s reaches another subscription: run --wire %s instead, or bind the turn role to a model --wire %s serves",
			forTurn.Says(), opts.wire, forTurn.Wire, opts.wire)
	}
	return forTurn.Model, nil
}

func chooseChild(opts runOpts) (childRole, error) {
	if opts.wire == wireKey {
		return childRole{}, nil
	}
	bound, err := boundRoles(opts.wire)
	if err != nil {
		return childRole{}, err
	}
	forChild := bound[models.RoleChild]
	return childRole{wire: forChild.Wire, id: forChild.Model.ID, windows: forChild.Model.WindowText()}, nil
}

const runUsage = `tofu run works a task in a directory until it is done.

Usage:
  tofu run --dir <path> [arguments] <task>

Arguments:
  --dir <path>          the directory the task is worked in, required
  --model <id>          the model to run on, otherwise the one the turn role is bound to
  --wire <name>         anthropic, codex or openrouter
  --tools <set>         full, or three for the read, write and bash arm
  --gate <arm>          off, shadow or enforce, otherwise the policy's own mode decides
  --no-gate             the arm that turns the tool gate off
  --no-crew             run without the spawn tool
  --done-review <arm>   the arm that reviews a child's answer
  --max-steps <n>       cap the steps a turn takes
  --max-decisions <n>   cap the gate decisions a turn spends
  --dry-run             print the request that would be sent and send nothing
`

func runVerb(args []string, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, runUsage)
		return exitOK
	}
	opts, err := parseRunArgs(args)
	if err != nil {
		return runFail(errOut, err)
	}

	selected, err := chooseModel(opts)
	if err != nil {
		return runFail(errOut, err)
	}
	if opts.child, err = chooseChild(opts); err != nil {
		return runFail(errOut, err)
	}

	built, err := buildRunTools(opts.dir, opts.toolSet)
	if err != nil {
		return runFail(errOut, err)
	}
	budget, err := contextBudget(opts, selected)
	if err != nil {
		return runFail(errOut, err)
	}

	if opts.dryRun {
		config, _ := runConfig(opts, built, runtime{spend: turn.SpendSubscription, budget: budget})
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
	model, spend, store, err := runModel(opts, selected.ID)
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	if err != nil {
		return runFail(errOut, err)
	}
	if model, err = guarded(model, budget); err != nil {
		return runFail(errOut, err)
	}

	askedAs, windows := selected.ID, selected.WindowText()
	if opts.child.wire != "" && (opts.child.wire != opts.wire || opts.child.id != selected.ID) {
		asChild := opts
		asChild.wire = opts.child.wire
		childModel, childSpend, childStore, childErr := runModel(asChild, opts.child.id)
		if childStore != nil {
			defer func() { _ = childStore.Close() }()
		}
		if childErr != nil {
			return runFail(errOut, childErr)
		}
		opts.child.model, opts.child.spend = childModel, childSpend
		askedAs, windows = opts.child.id, opts.child.windows
	}

	var gate *toolGate
	if opts.gateArm != gateOff {
		if gate, err = newToolGate(opts.dir); err != nil {
			return runFail(errOut, err)
		}
	}

	sessions, err := session.Open()
	if err != nil {
		return runFail(errOut, err)
	}

	config, spawner := runConfig(opts, built, runtime{model: model, spend: spend, budget: budget, gate: gate, sessions: sessions})
	if spawner != nil {
		review, reviewErr := newDoneReview(opts.doneArm)
		if reviewErr != nil {
			return runFail(errOut, reviewErr)
		}
		spawner.Review = review
	}
	_, _ = fmt.Fprintln(out, "context budget "+budget.Record())
	row, runErr := turn.Run(context.Background(), config)
	printRunRow(out, row, selected.ID, selected.WindowText())
	for _, child := range childRows(spawner) {
		printRunRow(out, child, askedAs, windows)
	}
	if row.ID != "" {
		if headErr := sessions.SetHead(row.ID); headErr != nil {
			_, _ = fmt.Fprintf(errOut, "tofu run: pointing the head at %s: %v\n", row.ID, headErr)
		}
	}
	if gate != nil {
		_, _ = fmt.Fprintf(out, "gate decisions %d cost $%.6f mode %s\n", gate.decisions, gate.costUSD, config.GateMode)
	}
	if runErr != nil {
		return runFail(errOut, runErr)
	}
	return exitOK
}

func runConfig(opts runOpts, built []turn.Tool, run runtime) (turn.Config, *turn.SpawnTool) {
	home, _ := os.UserHomeDir()
	system := runSystem(opts)
	if written := turn.ProjectInstructions(opts.dir, home); written != "" {
		system += "\n\n" + written
	}
	config := turn.Config{
		Model:       run.model,
		Spend:       run.spend,
		Tools:       turn.NewRegistry(built...),
		Task:        opts.task,
		Wire:        opts.wire,
		System:      system,
		Environment: turn.Environment(opts.dir, time.Now()),
		Caps: turn.Caps{
			MaxSteps:     opts.maxSteps,
			MaxDecisions: opts.maxDecisions,
		},
		ResultBytesCap: konst.TurnResultBytesCap,
		Budget:         run.budget,
		Sessions:       run.sessions,
	}
	if run.gate != nil {
		config.Gate = run.gate
		config.GateMode = gateMode(opts.gateArm, run.gate.set.Mode)
	}
	parentID := cmp.Or(opts.turnID, "turn-"+strconv.FormatInt(time.Now().UnixNano(), 16))
	config.NewID = func() string { return parentID }
	if opts.noCrew || opts.toolSet == toolSetThree {
		return config, nil
	}
	spawner := turn.NewSpawnTool(parentID, childBase(config, opts.child), &crew.Roster{})
	config.Tools = turn.NewRegistry(append(slices.Clone(built), spawner)...)
	return config, spawner
}

func childBase(config turn.Config, child childRole) turn.Config {
	if child.model == nil {
		return config
	}
	config.Model, config.Spend, config.Wire = child.model, child.spend, child.wire
	return config
}

func gateArms() []string { return []string{gateOff, gateShadow, gateEnforce} }

func gateMode(arm string, declared policy.Mode) turn.GateMode {
	switch arm {
	case gateEnforce:
		return turn.GateEnforce
	case gateShadow, gateOff:
		return turn.GateShadow
	case gateFollowsThePolicy:
		if declared == policy.ModeEnforced {
			return turn.GateEnforce
		}
		return turn.GateShadow
	}
	panic("tofu run: unknown gate arm " + arm)
}

func runModel(opts runOpts, model string) (turn.Model, turn.Spend, *cred.Store, error) {
	spend := wireSpend(opts.wire)
	switch opts.wire {
	case wireKey:
		client, err := keyModel(model)
		return client, spend, nil, err
	case wireCodex:
		client, store, err := codexModel(model)
		return client, spend, store, err
	}
	client, store, err := subscriptionModel(model)
	return client, spend, store, err
}

func dryRunBody(opts runOpts, model string, config turn.Config) ([]byte, error) {
	tools := config.Tools.Definitions()
	messages := []llm.Message{{Role: llm.RoleUser, Content: config.FirstUserMessage()}}
	switch opts.wire {
	case wireKey:
		system := append([]llm.Message{{Role: llm.RoleSystem, Content: config.System}}, messages...)
		return llm.Request{Messages: system, Tools: tools}.Encode(model)
	case wireCodex:
		return codex.Request{Model: model, Instructions: config.System, Messages: messages, Tools: tools}.Encode(nil)
	}
	return anthropic.Request{Model: model, System: []string{config.System}, Messages: messages, Tools: tools}.Encode(true)
}

func keyModel(model string) (turn.Model, error) {
	key, err := jev.Key(".env")
	if err != nil {
		return nil, err
	}
	wire, err := openrouter.New(openrouter.Config{
		Model: model,
		Key:   key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
			Retries:        konst.TurnRetries,
			Backoff:        time.Duration(konst.TurnBackoffMillis) * time.Millisecond,
			MaxBackoff:     time.Duration(konst.TurnMaxBackoffMillis) * time.Millisecond,
			Concurrency:    1,
		},
	})
	if err != nil {
		return nil, err
	}
	return llm.NewClient(wire)
}

func subscriptionCredential(provider cred.Provider) (func(context.Context) (string, error), string, *cred.Store, error) {
	spec, err := cred.Lookup(string(provider))
	if err != nil {
		return nil, "", nil, err
	}
	path, err := cred.Path()
	if err != nil {
		return nil, "", nil, err
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil, "", nil, err
	}
	row, present, err := store.Row(provider)
	if err != nil {
		return nil, "", store, err
	}
	if !present {
		return nil, "", store, fmt.Errorf("no %s subscription credential, run tofu login %s", provider, provider)
	}
	return cred.NewManager(store, spec).Access, row.Credential.Identity.AccountID, store, nil
}

func subscriptionModel(model string) (turn.Model, *cred.Store, error) {
	token, accountID, store, err := subscriptionCredential(cred.Anthropic)
	if err != nil {
		return nil, store, err
	}
	session, err := sessionID()
	if err != nil {
		return nil, store, err
	}
	wire, err := anthropic.New(anthropic.Config{
		Model:     model,
		Token:     token,
		SessionID: session,
		AccountID: accountID,
	})
	if err != nil {
		return nil, store, err
	}
	return turn.Subscription{Wire: wire}, store, nil
}

func codexModel(model string) (turn.Model, *cred.Store, error) {
	token, _, store, err := subscriptionCredential(cred.Codex)
	if err != nil {
		return nil, store, err
	}
	session, err := sessionID()
	if err != nil {
		return nil, store, err
	}
	wire, err := codex.New(codex.Config{
		Model:          model,
		Token:          token,
		InstallationID: session,
		SessionID:      session,
	})
	if err != nil {
		return nil, store, err
	}
	return codexTurn{wire: wire}, store, nil
}

type codexTurn struct {
	wire *codex.Wire
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
	})
	if err != nil {
		return llm.Decision{}, err
	}

	decision := llm.Decision{
		Build:           result.Model,
		RequestID:       result.ID,
		Outcome:         llm.OutcomeAfter(result.Stop, len(result.ToolCalls)),
		Stop:            result.StopReason,
		Content:         result.Content,
		ToolCalls:       result.ToolCalls,
		Usage:           llm.Usage{InputTokens: result.Usage.Input, OutputTokens: result.Usage.Output},
		CacheReadTokens: result.Usage.CacheRead,
		Warnings:        result.Warnings,
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

const everyToolIsRelativeToTheWorkingDirectory = "you are working inside one directory. every path you name is relative to it and nothing above it exists. "

func runSystem(opts runOpts) string {
	if opts.toolSet == toolSetThree {
		return everyToolIsRelativeToTheWorkingDirectory +
			"read reads a whole file, write creates one or replaces it whole, and bash runs anything else, " +
			"including finding a file, searching text and changing part of a file."
	}
	system := everyToolIsRelativeToTheWorkingDirectory + turn.PreferTheToolOverTheShell
	if opts.noCrew {
		return system
	}
	return system + " " +
		"spawn hands one piece of work to a child with its own context and its own list of paths it may write, " +
		"and returns what the child did rather than its transcript: use it when a piece of the task is separable and its paths do not overlap another child's."
}

func buildRunTools(dir, set string) ([]turn.Tool, error) {
	readTool, readErr := turn.NewReadTool(dir)
	writeTool, writeErr := turn.NewWriteTool(dir)
	bashTool, bashErr := turn.NewBashTool(dir)
	if err := cmp.Or(readErr, writeErr, bashErr); err != nil {
		return nil, err
	}
	checked, checkErr := tools.Checked(dir, []turn.Tool{bashTool})
	if checkErr != nil {
		return nil, checkErr
	}
	shell := checked[0]
	if set == toolSetThree {
		return tools.NewMemo().Wrap([]turn.Tool{readTool, writeTool, shell}), nil
	}
	globTool, globErr := tools.NewGlob(dir)
	grepTool, grepErr := tools.NewGrep(dir)
	searchTool, searchErr := tools.NewSearch(dir)
	symbolsTool, symbolsErr := tools.NewSymbols(dir)
	editTool, editErr := tools.NewEdit(dir)
	projectTool, projectErr := tools.NewProject(dir)
	verbTools, verbErr := tools.NewVerbs(dir)
	if err := cmp.Or(globErr, grepErr, searchErr, symbolsErr, editErr, projectErr, verbErr); err != nil {
		return nil, err
	}
	full := append([]turn.Tool{readTool, writeTool, shell, projectTool, globTool, grepTool, searchTool, symbolsTool, editTool}, verbTools...)
	return tools.NewMemo().Wrap(full), nil
}

func printRunRow(out io.Writer, row turn.Row, askedAs, windows string) {
	spend := "spend subscription windows " + windows + ", no money"
	if row.Spend != turn.SpendSubscription {
		spend = fmt.Sprintf("spend api key $%.6f", row.TotalCostUSD)
	}
	_, _ = fmt.Fprintf(out, "turn %s outcome %s model %s asked_as %s %s wall_clock_ms %d\n",
		row.ID, row.Outcome, row.Model, askedAs, spend, row.WallClockMS)
	if len(row.ChildIDs) > 0 {
		_, _ = fmt.Fprintf(out, "turn %s spawned %s\n", row.ID, strings.Join(row.ChildIDs, " "))
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
			_, _ = fmt.Fprintf(out, "step %d: tool_call tool=%s command=%q child=%q exit_code=%s gate=%s error=%q\n",
				step.Index, call.Tool, call.Command, call.ChildID, exitCode, call.GateVerdict, call.Error)
		}
	}
}

func childRows(spawner *turn.SpawnTool) []turn.Row {
	if spawner == nil {
		return nil
	}
	return spawner.Children()
}

func runFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu run: %v\n", err)
	return exitUsage
}

func parseRunArgs(args []string) (runOpts, error) {
	opts := runOpts{
		wire:         wireSubscription,
		toolSet:      toolSetFull,
		doneArm:      doneArmOff,
		maxDecisions: konst.TurnMaxDecisions,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch arg {
		case "--dir":
			opts.dir, err = nextArg(args, &i, arg)
		case "--dry-run":
			opts.dryRun = true
		case "--no-gate":
			opts.gateArm = gateOff
		case "--gate":
			opts.gateArm, err = nextArg(args, &i, arg)
		case "--no-crew":
			opts.noCrew = true
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
		case "--max-steps":
			opts.maxSteps, err = nextInt(args, &i, arg)
		case "--max-decisions":
			opts.maxDecisions, err = nextInt(args, &i, arg)
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
	if !slices.Contains(runWires(), opts.wire) {
		return runOpts{}, fmt.Errorf("--wire %q is none of %s", opts.wire, strings.Join(runWires(), ", "))
	}
	if opts.gateArm != gateFollowsThePolicy && !slices.Contains(gateArms(), opts.gateArm) {
		return runOpts{}, fmt.Errorf("--gate %q is none of %s: with no --gate the policy's own mode decides",
			opts.gateArm, strings.Join(gateArms(), ", "))
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
		return runOpts{}, fmt.Errorf("--done-review %s with %s: that arm has no spawn tool, so no child is ever reviewed and the flag would say a check is running that is not",
			opts.doneArm, without)
	}
	return opts, nil
}

func spawnlessFlag(opts runOpts) string {
	if opts.noCrew {
		return "--no-crew"
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
