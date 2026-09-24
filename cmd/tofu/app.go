package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"tofu/interface/tui"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	sessionstore "tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/shell"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
	"tofu/internal/widget"
)

const (
	noTerminal       = "tofu: the app needs a terminal. with input redirected, use tofu run --dir <dir> <task>"
	gateOffNote      = "gate off: no tool call is judged until tofu login openrouter stores the key"
	noCredential     = "no subscription is signed in, so no model can answer"
	loginFix         = "tofu login claude-sub, which opens the browser; tofu login codex-sub signs in the other subscription"
	noGateKey        = "there is no openrouter key, so jev judges no tool call"
	gateKeyFix       = "tofu login openrouter, which asks for the key and checks it reaches jev"
	unreadableSource = "unreadable: "
	jevName          = "jev"
	freshSessionNote = "the next task starts a new session and carries nothing from the last one"
	sessionEnded     = "tofu: session ended"
	stoppingPrefix   = ", stopping "
)

const (
	kilobyte            = 1024
	quotedColumns       = 32
	workingDirectory    = "the working directory"
	changeDirectory     = "cd "
	moreOfAStoredResult = "more of a stored result"
	earlierCallsHidden  = " earlier calls hidden"
	storedNote          = ", first and last part kept"
	noOutput            = "no output"
	artifactPrefix      = "artifact "
	artifactHolds       = " holds this result whole: "
	artifactUnit        = " bytes,"
	unifiedDiffHeader   = "--- "
	createdFilePrefix   = "created "
	placeWords          = 2
	queuedMessages      = 64
)

type appWiring struct {
	open     func(runOpts) (appWire, error)
	wires    func() []tui.Wire
	blockers func() []tui.Requirement
}

func appOptions(dir string, wiring appWiring, resumed sessionResume) tui.Options {
	note := ""
	if _, err := gateKey(); err != nil {
		note = gateOffNote
	}
	answers := make(chan tui.Answer, 1)
	steering := make(chan string, queuedMessages)
	live := newAppSession(dir, wiring.open, answers, time.Now, resumed)
	live.steer = steering
	settingsStore, _ := openSettings(dir)
	registry, registryErr := openShellRegistry()
	if registryErr == nil {
		live.shells = registry
	}
	return tui.Options{
		Repo:         filepath.Base(dir),
		Root:         dir,
		Branch:       branchOf(dir),
		Note:         note,
		Requirements: wiring.blockers(),
		Recheck:      wiring.blockers,
		Login:        loginCommand(string(cred.ClaudeSub)),
		Wires:        wiring.wires,
		Providers:    appProviders(),
		Quota:        appQuota,
		Settings:     settingsStore,
		Reload:       appReload,
		Turn:         live.run,
		Paste:        paste.Board{Dir: live.pendingSessionDir, Recorded: live.recordAttachment},
		Answers:      answers,
		Steering:     steering,
		Paths:        appPaths(dir),
		ResumeHead:   live.resumeHead,
		NewSession:   live.startFresh,
		Shells:       appShells(registry, registryErr),
		KillShell:    appKillShell(registry, registryErr),
	}
}

func appVerb(in io.Reader, out, errOut io.Writer, resumed sessionResume) int {
	file, isFile := in.(*os.File)
	if !isFile || !term.IsTerminal(file.Fd()) {
		_, _ = fmt.Fprintln(errOut, noTerminal)
		return exitUsage
	}
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu: the working directory is unreadable: %v\n", err)
		return exitVerdict
	}
	live := appWiring{open: openAppWire, wires: appWires, blockers: appRequirements}
	if err := tui.Run(appOptions(dir, live, resumed)); err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintln(out, sessionEndLine(openShellRegistry()))
	return exitOK
}

func sessionEndLine(registry *shell.Registry, openErr error) string {
	var stopped []string
	if openErr == nil {
		found, _ := registry.List()
		for _, one := range found {
			if one.State == shell.Running {
				stopped = append(stopped, one.Name+" ("+one.Command+")")
			}
		}
	}
	if len(stopped) == 0 {
		return sessionEnded
	}
	return sessionEnded + stoppingPrefix + strings.Join(stopped, ", ")
}

func appPaths(dir string) func() []string {
	return func() []string {
		listed, err := tools.ListPaths(dir)
		if err != nil {
			return nil
		}
		return listed
	}
}

func loginCommand(provider string) func() *exec.Cmd {
	return func() *exec.Cmd { return exec.Command(os.Args[0], "login", provider) }
}

type subscriptionSource struct {
	source cred.Provider
	wire   string
}

func appWires() []tui.Wire {
	path, err := cred.Path()
	if err != nil {
		return nil
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = store.Close() }()
	now := time.Now()
	var wires []tui.Wire
	for _, known := range []subscriptionSource{{cred.ClaudeSub, wireSubscription}, {cred.CodexSub, wireCodex}} {
		row, present, err := store.RowAt(known.source, now)
		if err != nil || !present || row.Unusable(now) != "" {
			continue
		}
		selected, err := selectModel(known.wire, "")
		if err != nil {
			continue
		}
		wires = append(wires, tui.Wire{
			Name:     known.wire,
			Model:    selected.ID,
			Provider: string(known.source),
			Efforts:  wireEfforts(known.wire),
		})
	}
	return wires
}

type startBlocker struct {
	label   string
	what    string
	fix     string
	command string
	run     func() *exec.Cmd
}

func startBlockers() []startBlocker {
	var blocking []startBlocker
	if len(appWires()) == 0 {
		blocking = append(blocking, startBlocker{
			label:   string(cred.ClaudeSub),
			what:    noCredential,
			fix:     loginFix,
			command: "tofu login " + string(cred.ClaudeSub),
			run:     loginCommand(string(cred.ClaudeSub)),
		})
	}
	if _, err := gateKey(); err != nil {
		blocking = append(blocking, startBlocker{
			label:   jevName,
			what:    noGateKey,
			fix:     gateKeyFix,
			command: "tofu login " + openRouterName,
			run:     loginCommand(openRouterName),
		})
	}
	return blocking
}

func appRequirements() []tui.Requirement {
	blocking := startBlockers()
	needed := make([]tui.Requirement, 0, len(blocking))
	for _, blocker := range blocking {
		needed = append(needed, tui.Requirement{What: blocker.what, Fix: blocker.fix, Run: blocker.run})
	}
	return needed
}

func appProviders() []settings.Provider {
	path, err := cred.Path()
	var store *cred.Store
	if err == nil {
		store, err = cred.Open(path)
	}
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	return []settings.Provider{
		subscriptionProvider(cred.ClaudeSub, store, path, err),
		openRouterProvider(),
		jevProvider(),
		subscriptionProvider(cred.CodexSub, store, path, err),
	}
}

func subscriptionProvider(provider cred.Provider, store *cred.Store, path string, openErr error) settings.Provider {
	row := settings.Provider{Name: string(provider), Source: path}
	if openErr != nil {
		row.Fix = unreadableSource + openErr.Error()
		return row
	}
	now := time.Now()
	stored, present, err := store.RowAt(provider, now)
	switch {
	case err != nil:
		row.Fix = unreadableSource + err.Error()
	case !present:
		row.Fix = "run tofu login " + string(provider)
	case stored.Unusable(now) != "":
		row.State = stored.Unusable(now)
	default:
		row.State = strings.TrimSpace("signed in " + maskedAccount(stored.Credential.Identity))
	}
	return row
}

func openRouterProvider() settings.Provider {
	located, err := locateGateKey()
	row := settings.Provider{Name: openRouterName, Source: located.Path}
	if located.Source == jev.SourceEnvironment {
		row.Source = located.Name + " in the environment"
	}
	key, keyErr := gateKey()
	if err != nil || keyErr != nil {
		row.Fix = openRouterFix
		return row
	}
	row.Key = widget.Mask(key)
	return row
}

func jevProvider() settings.Provider {
	dir, err := sys.LogDir()
	if err != nil {
		return settings.Provider{Name: jevName, Fix: unreadableSource + err.Error()}
	}
	row := settings.Provider{Name: jevName, Source: dir}
	current, err := currentBuild(dir, "")
	switch {
	case err != nil:
		row.Fix = unreadableSource + err.Error()
	case current.Known:
		row.State = current.Build
	default:
		row.Fix = "no decision recorded yet"
	}
	return row
}

func branchOf(dir string) string {
	head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
}

func appShells(registry *shell.Registry, openErr error) func() []shells.Entry {
	return func() []shells.Entry {
		if openErr != nil {
			return nil
		}
		found, err := registry.List()
		if err != nil {
			return nil
		}
		entries := make([]shells.Entry, 0, len(found))
		for _, one := range found {
			entry := shells.Entry{Name: one.Name, Command: one.Command, Started: one.Started, ExitCode: one.ExitCode}
			switch one.State {
			case shell.Running:
				entry.State = shells.Running
			case shell.Exited:
				entry.State = shells.Exited
			case shell.Killed:
				entry.State = shells.Killed
			}
			entry.Log, _ = registry.Tail(one.Name, shell.DefaultTail)
			entries = append(entries, entry)
		}
		return entries
	}
}

func appKillShell(registry *shell.Registry, openErr error) func(string) error {
	return func(name string) error {
		if openErr != nil {
			return openErr
		}
		return registry.Kill(name)
	}
}

func appQuota() []frame.Quota {
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return nil
	}
	return quotasFrom(results)
}

func quotasFrom(results []pollResult) []frame.Quota {
	quotas := make([]frame.Quota, 0, len(results))
	for _, result := range results {
		if result.err != nil {
			continue
		}
		if fullest, ok := fullestWindow(result.report); ok {
			quotas = append(quotas, fullest)
		}
	}
	return quotas
}

func fullestWindow(report quota.Report) (frame.Quota, bool) {
	var fullest frame.Quota
	found := false
	for _, window := range report.Windows {
		if !window.Used.Reported || strings.Contains(window.ID, ":") || (found && window.Used.Fraction <= fullest.Fraction) {
			continue
		}
		fullest = frame.Quota{
			Label:    string(report.Provider) + " " + window.ID,
			Fraction: window.Used.Fraction,
			Reported: true,
			ResetsAt: window.ResetsAt,
		}
		found = true
	}
	return fullest, found
}

type appWire struct {
	held     *accounts
	spend    turn.Spend
	selected models.Model
}

func openAppWire(opts runOpts) (appWire, error) {
	selected, err := chooseModel(opts)
	if err != nil {
		return appWire{}, err
	}
	held, spend, err := openAccounts(opts, selected.ID)
	return appWire{held: held, spend: spend, selected: selected}, err
}

func awaitPerson(emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns, answers <-chan tui.Answer, granted map[string]bool) turn.Person {
	return func(ctx context.Context, request turn.GateRequest, decision turn.GateDecision) (turn.PersonAnswer, error) {
		place := askedPlace(request)
		if granted[place] {
			return turn.PersonAlwaysHere, nil
		}
		emit(tui.Event{Kind: tui.EventAwaitPerson})
		defer emit(tui.Event{Kind: tui.EventResumed})
		select {
		case answered, open := <-answers:
			if !open {
				return turn.PersonDenied, errors.New("the app stopped taking answers")
			}
			var out turn.PersonAnswer
			switch answered {
			case tui.AlwaysHere:
				granted[place] = true
				out = turn.PersonAlwaysHere
			case tui.AllowedOnce:
				out = turn.PersonAllowedOnce
			case tui.Denied:
				out = turn.PersonDenied
			default:
				panic("tofu: unknown answer from the app")
			}
			recordPersonAnswer(decision.ID, out)
			return out, nil
		case <-ctx.Done():
			return turn.PersonDenied, ctx.Err()
		}
	}
}

func recordPersonAnswer(id string, answer turn.PersonAnswer) {
	if id == "" {
		return
	}
	dir, err := sys.LogDir()
	if err != nil {
		return
	}
	_ = ledger.NewWriter(dir).Backfill(id, answer.Outcome())
}

func steered(queue <-chan string, emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) []string {
	var taken []string
	for {
		select {
		case task := <-queue:
			emit(tui.Event{Kind: tui.EventSteered, Text: task})
			taken = append(taken, task)
		default:
			return taken
		}
	}
}

func askedPlace(request turn.GateRequest) string {
	var fields struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(request.Args, &fields); err != nil {
		return request.Tool + " " + string(request.Args)
	}
	switch {
	case fields.Path != "":
		return request.Tool + " " + fields.Path
	case fields.Command != "":
		return request.Tool + " " + commandPlace(fields.Command)
	}
	return request.Tool + " " + string(request.Args)
}

func commandPlace(command string) string {
	words, named := make([]string, 0, placeWords), 0
	for _, word := range strings.Fields(command) {
		if strings.HasPrefix(word, "-") {
			words = append(words, word)
			continue
		}
		if named++; named <= placeWords {
			words = append(words, word)
		}
	}
	return strings.Join(words, " ")
}

type appSession struct {
	dir     string
	open    func(runOpts) (appWire, error)
	answers <-chan tui.Answer
	steer   <-chan string
	now     func() time.Time
	id      string
	carried []llm.Message
	shown   map[string]bool
	granted map[string]bool
	pending []pendingImage
	shells  *shell.Registry
}

type pendingImage struct {
	index int
	name  string
}

func newAppSession(dir string, open func(runOpts) (appWire, error), answers <-chan tui.Answer, now func() time.Time, resumed sessionResume) *appSession {
	return &appSession{
		dir:     dir,
		open:    open,
		answers: answers,
		now:     now,
		id:      resumed.Session,
		carried: resumed.messages,
		shown:   map[string]bool{},
		granted: map[string]bool{},
	}
}

func appTurnOn(dir string, open func(runOpts) (appWire, error), answers <-chan tui.Answer, now func() time.Time, resumed sessionResume) tui.Turn {
	return newAppSession(dir, open, answers, now, resumed).run
}

func (s *appSession) startFresh() string {
	s.id, s.carried = "", nil
	return freshSessionNote
}

func (s *appSession) pendingID() string {
	if s.id == "" {
		s.id = turn.NewID(s.now())
	}
	return s.id
}

func (s *appSession) pendingSessionDir() (string, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return "", err
	}
	return store.Dir(s.pendingID()), nil
}

func (s *appSession) recordAttachment(index int, name string, bytes int, format string) {
	store, err := sessionstore.Open()
	if err != nil {
		return
	}
	_ = store.AppendEvent(s.pendingID(), sessionstore.EventAttachment, sessionstore.Attachment{File: name, Bytes: bytes, Format: format})
	s.pending = append(s.pending, pendingImage{index: index, name: name})
}

func (s *appSession) takePendingImages(task string) ([]llm.Image, error) {
	pending := s.pending
	s.pending = nil
	var wanted []pendingImage
	for _, image := range pending {
		if strings.Contains(task, session.ImageToken(image.index)) {
			wanted = append(wanted, image)
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	store, err := sessionstore.Open()
	if err != nil {
		return nil, err
	}
	dir := store.Dir(s.pendingID())
	images := make([]llm.Image, 0, len(wanted))
	for _, image := range wanted {
		data, err := os.ReadFile(filepath.Join(dir, image.name))
		if err != nil {
			return nil, err
		}
		images = append(images, llm.Image{MediaType: imageMediaType(image.name), Data: data})
	}
	return images, nil
}

func imageMediaType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "image/" + strings.TrimPrefix(ext, ".")
}

func (s *appSession) label(store *sessionstore.Store) (string, string) {
	id := s.id
	if id == "" {
		return "", ""
	}
	header, err := store.Header(id)
	if err != nil {
		if writeErr := store.Write(sessionstore.Header{ID: id, Root: id, At: s.now()}, nil); writeErr != nil {
			return "", id
		}
		header, err = store.Header(id)
		if err != nil {
			return "", id
		}
	}
	if header.Name == nil {
		return "", id
	}
	return *header.Name, id
}

func (s *appSession) resumeHead() string {
	store, err := sessionstore.Open()
	if err != nil {
		return "the session store does not open, so nothing is carried: " + err.Error()
	}
	carry := continueCarry(store)
	if carry.Fresh != "" {
		return carry.Fresh
	}
	s.id, s.carried = carry.Session, carry.messages
	return "continuing " + carry.Session + ", " + strconv.Itoa(carry.Carried) + " messages from " + sessionSteps(carry.Steps)
}

func gateOffEvent(gateErr error) tui.Event {
	var missing jev.MissingKey
	errors.As(gateErr, &missing)
	return tui.Event{Kind: tui.EventGateOff, Text: gateErr.Error(), GateWhy: missing.Why}
}

func pickedOpts(dir, turnID, task string, pick tui.Pick, maxSteps int) runOpts {
	return runOpts{
		dir:              dir,
		task:             task,
		turnID:           turnID,
		wire:             cmp.Or(pick.Wire, wireSubscription),
		model:            pick.Model,
		toolSet:          toolSetFull,
		effort:           cmp.Or(pick.Effort, llm.EffortDefault),
		loopGuardRepeats: konst.TurnLoopGuardRepeats,
		loopGuardWindow:  konst.TurnLoopGuardWindow,
		maxSteps:         maxSteps,
	}
}

func (s *appSession) run(ctx context.Context, pick tui.Pick, task string, emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {
	fail := func(err error) { emit(tui.Event{Kind: tui.EventFailure, Text: err.Error()}) }
	images, imagesErr := s.takePendingImages(task)
	if imagesErr != nil {
		fail(imagesErr)
		return
	}
	say := func(unreadable string) { emit(tui.Event{Kind: tui.EventNote, Text: unreadable}) }
	opts := pickedOpts(s.dir, s.pendingID(), task, pick, settingInt(s.dir, settingspkg.DecisionCap, say))
	opts.noSubAgents = settingInt(s.dir, settingspkg.TurnMaySpawn, say) == 0
	opts.readBeforeEdit = settingInt(s.dir, settingspkg.ReadBeforeEdit, say) != 0
	shell, shellErr := turn.ResolveRunShell(settingText(s.dir, settingspkg.Shell, say))
	if shellErr != nil {
		fail(shellErr)
		return
	}
	opts.shell = shell
	opened, err := s.open(opts)
	if opened.held != nil {
		defer opened.held.close()
	}
	if err != nil {
		fail(err)
		return
	}
	built, plan, builtErr := buildRunToolsForRun(s.dir, opts.toolSet, opts.readBeforeEdit, shell)
	sessions, sessionsErr := sessionstore.Open()
	if err := cmp.Or(builtErr, sessionsErr); err != nil {
		fail(err)
		return
	}
	if name, id := s.label(sessions); id != "" {
		emit(tui.Event{Kind: tui.EventSession, Text: name, ID: id})
	}
	gate, gateErr := newToolGate(s.dir)
	var unusable unusableRule
	if errors.As(gateErr, &unusable) {
		fail(fmt.Errorf("no turn starts while the tool gate rule is unusable: %w", unusable))
		return
	}
	if gateErr != nil {
		emit(gateOffEvent(gateErr))
	}
	if gate != nil {
		gate.watch = func(tool string, gated turn.GateDecision, err error) {
			switch {
			case err != nil:
				failed := session.Decision{Tool: tool, Verdict: session.Ask, Failure: err.Error()}
				emit(tui.Event{Kind: tui.EventDecision, Decision: &failed})
			case gated.Verdict != ledger.VerdictUnset:
				decided := gateDecision(tool, gated)
				emit(tui.Event{Kind: tui.EventDecision, Decision: &decided})
			}
		}
	}

	budget, budgetErr := contextBudget(opts, opened.selected)
	if budgetErr != nil {
		fail(budgetErr)
		return
	}
	held := &roster.Roster{}
	watch := &appWatcher{gate: gate, held: held, emit: emit, now: s.now, turnID: opts.turnID, seen: s.shown, maxSteps: opts.maxSteps, spent: map[string]int{}}
	opened.held.wrap = func(model turn.Model) (turn.Model, error) {
		asked, guardErr := guarded(model, budget)
		if guardErr != nil {
			return nil, guardErr
		}
		watch.inner = asked
		return watch, nil
	}
	notify := func(notice string) { emit(tui.Event{Kind: tui.EventNote, Text: notice}) }
	config, spawner, configErr := runConfig(opts, built, runtime{accounts: opened.held.forTurn(), spend: opened.spend, budget: budget, gate: gate, sessions: sessions, notify: notify, roster: held, now: s.now})
	if configErr != nil {
		fail(configErr)
		return
	}
	config.History = s.carried
	config.Images = images
	if s.answers != nil {
		config.Person = awaitPerson(emit, s.answers, s.granted)
	}
	if s.steer != nil {
		config.Steering = func() []string { return steered(s.steer, emit) }
	}
	watch.spawner = spawner
	config.ToolResult = watch.result
	config.Step = func(step turn.StepRow) {
		emit(tui.Event{Kind: tui.EventPlan, Plan: statedPlan(plan.Items())})
		if step.Occupancy == nil {
			return
		}
		emit(tui.Event{Kind: tui.EventContext, Context: frame.Context{
			Used:   step.Occupancy.Total(),
			Budget: budget.CeilingTokens,
		}})
	}
	config.EndedSession = func(turn.Row) error {
		emit(tui.Event{Kind: tui.EventForkStart})
		emit(tui.Event{Kind: tui.EventForkEnd})
		return nil
	}
	row, runErr := turn.Run(turn.WithShellRegistry(ctx, s.shells), config)
	stopped := errors.Is(runErr, context.Canceled)
	if runErr != nil && !stopped {
		fail(runErr)
	}
	if row.Conversation != nil {
		s.carried = turn.Sendable(row.Conversation)
	}
	if row.ID != "" {
		s.id = row.ID
		if headErr := sessions.SetHead(row.ID); headErr != nil {
			fail(headErr)
		}
	}
	outcome := row.Outcome
	if stopped {
		outcome = turn.OutcomeStopped
	}
	emit(tui.Event{Kind: tui.EventDone, Text: doneWords(outcome, row.Guard)})
}

func doneWords(outcome turn.Outcome, guard *turn.LoopGuardStop) string {
	switch outcome {
	case turn.OutcomeUnset, turn.OutcomeForked:
		return "finished in"
	case turn.OutcomeStopped:
		return "cooked for"
	case turn.OutcomeStepCap:
		return "stopped at the step cap after"
	case turn.OutcomeDecisionCap:
		return "stopped at the decision cap after"
	case turn.OutcomeTruncated:
		return "stopped on a reply it could not finish, after"
	case turn.OutcomeError:
		return "failed after"
	case turn.OutcomeRetiredCostCap, turn.OutcomeRetiredWallClockCap:
		return "stopped at a cap this build no longer sets, after"
	case turn.OutcomeLoopGuard:
		return loopGuardWords(guard) + ", after"
	}
	panic("tofu: unknown outcome " + outcome.String())
}

func loopGuardWords(guard *turn.LoopGuardStop) string {
	if guard == nil {
		return "stopped itself after repeating a tool call"
	}
	return "stopped itself after calling " + strconv.Quote(guard.Tool) + " with " + string(guard.Args) +
		" and getting the same result " + strconv.Itoa(guard.Repeats) + " times in a row"
}

func statedPlan(items []tools.PlanItem) []session.PlanItem {
	drawn := make([]session.PlanItem, 0, len(items))
	for _, item := range items {
		drawn = append(drawn, session.PlanItem{Phase: item.Phase, Text: item.Text, State: drawnPlanState(item.State)})
	}
	return drawn
}

func drawnPlanState(state tools.PlanState) session.PlanState {
	switch state {
	case tools.PlanPending:
		return session.PlanPending
	case tools.PlanRunning:
		return session.PlanRunning
	case tools.PlanDone:
		return session.PlanDone
	case tools.PlanDropped:
		return session.PlanDropped
	}
	panic("tofu: unknown plan item state " + string(state))
}

func gateDecision(tool string, gated turn.GateDecision) session.Decision {
	decision := session.Decision{Tool: tool, Verdict: sessionVerdict(gated.Verdict)}
	for _, answer := range gated.Answers {
		decision.Answers = append(decision.Answers, sessionAnswer(answer))
	}
	if gated.Reason == nil {
		return decision
	}
	decision.Reason = session.Reason{
		Question:  gated.Reason.Question,
		Limit:     gated.Reason.Comparison,
		Threshold: gated.Reason.Threshold,
		Value:     gated.Reason.Value,
		DeadBand:  gated.Reason.DeadBand,
		RelaxedBy: gated.Reason.RelaxedBy,
		Blocked:   gated.Reason.Blocked,
	}
	for _, answer := range gated.Answers {
		if answer.Question == gated.Reason.Question {
			decision.Reason.Levels = answer.Legend
		}
	}
	if len(gated.Answers) == 0 && gated.Reason.ModeReason != nil {
		decision.Failure = *gated.Reason.ModeReason
	}
	return decision
}

func sessionVerdict(verdict ledger.Verdict) session.Verdict {
	switch verdict {
	case ledger.VerdictAllow:
		return session.Allow
	case ledger.VerdictDeny:
		return session.Deny
	case ledger.VerdictAsk, ledger.VerdictUnset:
		return session.Ask
	}
	panic("tofu: unknown verdict " + string(verdict))
}

func sessionAnswer(answer ledger.Answer) session.Answer {
	out := session.Answer{Question: answer.Question, Max: 1}
	switch answer.Kind {
	case ledger.AnswerNoul:
		out.Value = answer.Noul
	case ledger.AnswerScore:
		out.Value, out.Max = answer.Score, float64(max(len(answer.Dist)-1, 1))
	case ledger.AnswerChoice:
		out.Choice = answer.Choice
		for _, slice := range answer.Dist {
			if slice.Option == answer.Choice {
				out.Value = slice.P
			}
		}
	default:
		panic("tofu: unknown answer kind " + string(answer.Kind))
	}
	return out
}

type appWatcher struct {
	inner     turn.Model
	gate      *toolGate
	spawner   *turn.SpawnTool
	held      *roster.Roster
	emit      tui.CalledFromInsideTheTurnAndNeverAfterItReturns
	now       func() time.Time
	turnID    string
	maxSteps  int
	seen      map[string]bool
	wrote     map[string]string
	in        int
	out       int
	cacheRead int
	spent     map[string]int
}

func (a *appWatcher) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	for _, message := range request.Messages {
		if message.Role == llm.RoleTool {
			a.result(message)
		}
	}
	a.sendSubAgents()

	a.emit(tui.Event{Kind: tui.EventRequesting})
	streamed := false
	request.OnDelta = func(text string) {
		streamed = true
		a.emit(tui.Event{Kind: tui.EventTextDelta, Text: text})
	}
	decision, err := a.inner.Ask(ctx, request)
	if err != nil {
		return decision, err
	}
	fresh := decision.PromptAccounting.FreshTokens(decision.Usage.InputTokens, decision.CacheReadTokens)
	a.in += fresh
	a.out += decision.Usage.OutputTokens
	a.cacheRead += decision.CacheReadTokens
	a.creditRunningChild(fresh + decision.Usage.OutputTokens)
	stats := tui.Event{Kind: tui.EventStats, Model: decision.Build, TokensIn: a.in, TokensOut: a.out, CacheRead: a.cacheRead}
	if a.gate != nil {
		stats.Decisions = a.gate.decisions
	}
	a.emit(stats)

	if text := strings.TrimSpace(decision.Content); text != "" && !streamed {
		a.emit(tui.Event{Kind: tui.EventText, Text: text})
	}
	for _, call := range decision.ToolCalls {
		intent, detail := callIntent(call)
		promotes := a.spawner != nil && call.Name == a.spawner.Name()
		a.emit(tui.Event{Kind: tui.EventToolCall, ID: sessionstore.EventIDFor(a.turnID, call.ID), Tool: call.Name, Text: intent, Detail: detail, Promote: promotes})
		a.noteWholeFile(call)
	}
	return decision, nil
}

func (a *appWatcher) result(message llm.Message) {
	killedWithNothingToShow := message.ToolOutcome == llm.ToolOutcomeAborted && message.ToolResultBytes == 0
	if a.seen[message.ToolCallID] || killedWithNothingToShow {
		return
	}
	a.seen[message.ToolCallID] = true
	result := tui.Event{
		Kind:   tui.EventToolResult,
		ID:     sessionstore.EventIDFor(a.turnID, message.ToolCallID),
		Text:   resultSummary(message.Content),
		Bytes:  message.ToolResultBytes,
		Failed: message.ToolOutcome.Failed(),
	}
	if !result.Failed {
		if strings.HasPrefix(message.Content, unifiedDiffHeader) {
			result.Diff = message.Content
		}
		if strings.HasPrefix(message.Content, createdFilePrefix) {
			result.Created = a.wrote[message.ToolCallID]
		}
	}
	delete(a.wrote, message.ToolCallID)
	a.emit(result)
}

func (a *appWatcher) noteWholeFile(call llm.ToolCall) {
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil || args.Content == "" {
		return
	}
	if a.wrote == nil {
		a.wrote = map[string]string{}
	}
	a.wrote[call.ID] = args.Content
}

func (a *appWatcher) creditRunningChild(tokens int) {
	if a.held == nil {
		return
	}
	agents := a.held.SubAgents()
	for index := len(agents) - 1; index >= 0; index-- {
		if agents[index].State != roster.Working {
			continue
		}
		a.spent[agents[index].ID] += tokens
		a.draw(agents)
		return
	}
}

func (a *appWatcher) sendSubAgents() {
	if a.held == nil {
		return
	}
	a.draw(a.held.SubAgents())
}

func (a *appWatcher) draw(agents []roster.SubAgent) {
	if len(agents) == 0 {
		return
	}
	children := make([]subagent.Child, len(agents))
	for index, agent := range agents {
		since := agent.Active.Sub(agent.Started)
		if agent.State == roster.Working {
			since = a.now().Sub(agent.Started)
		}
		children[index] = subagent.Child{
			Name:   "c" + strconv.Itoa(index+1),
			Owns:   agent.Owns,
			Doing:  agent.Mission,
			Since:  since,
			Steps:  agent.Steps,
			Total:  cmp.Or(a.maxSteps, konst.TurnMaxSteps),
			Tokens: a.spent[agent.ID],
			State:  drawnState(agent.State),
			Calls:  recordedOrCalling(recordedCalls(childRows(a.spawner), agent.ID), agent.Calling, agent.CallsDropped),
			Report: agent.Report,
		}
	}
	a.emit(tui.Event{Kind: tui.EventSubAgent, Children: children})
}

func drawnState(state roster.State) subagent.State {
	switch state {
	case roster.Working:
		return subagent.Running
	case roster.WaitingAnswer:
		return subagent.WaitingForAnswer
	case roster.InReview:
		return subagent.HandedBack
	case roster.Parked:
		return subagent.Parked
	case roster.Errored:
		return subagent.Errored
	case roster.Finished:
		return subagent.Done
	}
	panic("tofu: unknown sub-agent state " + state.String())
}

func recordedOrCalling(recorded []subagent.Call, calling []string, dropped int) []subagent.Call {
	if len(recorded) > 0 {
		return recorded
	}
	watched := make([]subagent.Call, len(calling))
	for index, tool := range calling {
		watched[index] = subagent.Call{Tool: tool}
	}
	return hidingEarlier(watched, dropped)
}

func hidingEarlier(kept []subagent.Call, hidden int) []subagent.Call {
	if hidden <= 0 {
		return kept
	}
	return append([]subagent.Call{{Tool: strconv.Itoa(hidden) + earlierCallsHidden}}, kept...)
}

func recordedCalls(rows []turn.Row, id string) []subagent.Call {
	var calls []subagent.Call
	for _, row := range rows {
		if row.ID != id && !strings.HasPrefix(row.ID, id+"-r") {
			continue
		}
		for _, step := range row.Steps {
			for _, ran := range step.ToolCalls {
				calls = append(calls, subagent.Call{Tool: ran.Tool, Text: strings.TrimPrefix(ran.Command, ran.Tool+" "), Result: ran.Error})
			}
		}
	}
	if len(calls) <= konst.SubAgentCallsWatched {
		return calls
	}
	hidden := len(calls) - konst.SubAgentCallsWatched + 1
	return hidingEarlier(calls[hidden:], hidden)
}

func callIntent(call llm.ToolCall) (string, string) {
	var fields map[string]any
	if err := json.Unmarshal(call.Arguments, &fields); err != nil {
		return "", ""
	}
	text := func(key string) string {
		value, _ := fields[key].(string)
		return oneLine(value)
	}
	command, pattern, path, task := text("command"), text("pattern"), text("path"), text("task")
	switch {
	case command != "":
		return shellIntent(command), command
	case pattern != "":
		return pattern + " in " + cmp.Or(text("glob"), path, workingDirectory), ""
	case path != "":
		return path, ""
	case task != "":
		return task, ""
	case text("handle") != "":
		return moreOfAStoredResult, ""
	}
	return "", ""
}

func shellIntent(command string) string {
	segments := shellSegments(command)
	if len(segments) == 0 {
		return command
	}
	if len(segments) > 1 && strings.HasPrefix(segments[0], changeDirectory) {
		segments = segments[1:]
	}
	if len(segments) == 1 {
		return segments[0]
	}
	return segments[0] + " +" + strconv.Itoa(len(segments)-1) + " more"
}

func shellSegments(command string) []string {
	var segments []string
	flattened := strings.NewReplacer("&&", ";", "||", ";", "\n", ";").Replace(command)
	for _, part := range strings.Split(flattened, ";") {
		if trimmed := oneLine(part); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
}

func resultSummary(content string) string {
	if size, stored := storedWhole(content); stored {
		return byteSize(size) + storedNote
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return noOutput
	}
	lines := strings.Count(trimmed, "\n") + 1
	if lines == 1 && len([]rune(trimmed)) <= quotedColumns {
		return oneLine(trimmed)
	}
	if lines == 1 {
		return "1 line, " + byteSize(len(content))
	}
	return strconv.Itoa(lines) + " lines, " + byteSize(len(content))
}

func storedWhole(content string) (int, bool) {
	head, _, _ := strings.Cut(content, "\n")
	rest, ok := strings.CutPrefix(head, artifactPrefix)
	if !ok {
		return 0, false
	}
	_, after, ok := strings.Cut(rest, artifactHolds)
	if !ok {
		return 0, false
	}
	digits, _, ok := strings.Cut(after, artifactUnit)
	if !ok {
		return 0, false
	}
	size, err := strconv.Atoi(digits)
	return size, err == nil
}

func byteSize(bytes int) string {
	if bytes < kilobyte {
		return strconv.Itoa(bytes) + " bytes"
	}
	return strconv.FormatFloat(float64(bytes)/kilobyte, 'f', 1, 64) + " KB"
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
