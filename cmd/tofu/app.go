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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"tofu/interface/tui"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/internal/cron"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/keymap"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
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
	gateOffNote      = "gate off: no tool call is judged until tofu login classifier openrouter stores the key"
	noCredential     = "no subscription is signed in, so no model can answer"
	noGateKey        = "there is no openrouter key, so jev judges no tool call"
	modelStep        = "language model"
	classifierStep   = "classifier · jev, required"
	unreadableSource = "unreadable: "
	jevName          = "jev"
	freshSessionNote = "the next task starts a new session and carries nothing from the last one"
	sessionEnded     = "tofu: session ended"
	stoppingPrefix   = ", stopping "
	leavingPrefix    = ", leaving running for the next launch "
	leftOverNote     = "shells left over from an earlier tofu: "
	leftOverFix      = ", end them in the shells screen"
	noteSeparator    = "  ·  "
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
	roundMark           = "-r"
	subAgentVerdict     = "sub-agent "
	cancelledAt         = "cancelled at"
	askTool             = "ask"
	messageTool         = "message"
	subAgentsTool       = "subagents"
	askAnswered         = "the orchestrator answers: "
	askAssumed          = "the orchestrator did not answer, so your default stands, assumed and not confirmed: "
	answeredReply       = "orchestrator: "
	assumedReply        = "orchestrator did not answer, assumed: "
	ranPreface          = "before sending this, the person ran a command in the project with !, outside any turn, and it printed:\n$ "
)

type appWiring struct {
	open      func(runOpts) (appWire, error)
	wires     func() []tui.Wire
	blockers  func() []tui.Requirement
	clipboard func() (sys.Clipboard, error)
	quota     func() []frame.Quota
	reload    func(ctx context.Context, out io.Writer) int
}

type appLaunch struct {
	resumed     sessionResume
	fresh       bool
	registry    *shell.Registry
	registryErr error
	note        string
	tabs        *tools.BrowserTabs
	stopWarm    *func()
	endSession  *func() []string
}

func launchOf(dir string, resumed sessionResume, fresh bool) appLaunch {
	registry, registryErr := launchShellRegistry(dir)
	home, _ := os.UserHomeDir()
	launch := appLaunch{resumed: resumed, fresh: fresh, registry: registry, registryErr: registryErr, tabs: tools.NewBrowserTabs(home), stopWarm: new(func()), endSession: new(func() []string)}
	if registryErr != nil {
		return launch
	}
	_ = registry.Prune()
	found, _ := registry.List()
	if leftOver := len(slices.DeleteFunc(found, func(one shell.Shell) bool { return !one.LeftOver() })); leftOver > 0 {
		launch.note = leftOverNote + strconv.Itoa(leftOver) + leftOverFix
	}
	return launch
}

func appOptions(dir string, arms runOpts, wiring appWiring, launch appLaunch) tui.Options {
	answers := make(chan tui.Answer, 1)
	steering, stopLead := make(chan string, queuedMessages), make(chan struct{}, 1)
	live := newAppSession(dir, wiring.open, answers, time.Now, launch.resumed)
	notes := []string{launch.note}
	if _, err := gateKey(); err != nil {
		notes = append(notes, gateOffNote)
	}
	if err := live.loadCron(launch.resumed.Session); err != nil {
		notes = append(notes, err.Error())
	}
	note := strings.Join(slices.DeleteFunc(notes, func(one string) bool { return one == "" }), noteSeparator)
	live.steer, live.stopLead = steering, stopLead
	live.arms = arms
	live.shells = launch.registry
	live.tabs, live.warm.tabs = launch.tabs, launch.tabs
	*launch.stopWarm = func() { live.warm.Close() }
	*launch.endSession = live.end
	settingsStore, _ := openSettings(dir)
	shortcuts, _ := keymap.ShortcutsPath()
	return tui.Options{
		Repo:         filepath.Base(dir),
		Root:         dir,
		Branch:       branchOf(dir),
		Note:         note,
		Requirements: wiring.blockers(),
		Recheck:      wiring.blockers,
		Login:        loginCommand(string(cred.ClaudeSub)),
		SaveKey:      storeKeyFor,
		Wires:        wiring.wires,
		Models:       func() (models.Library, error) { return modelLibrary(dir) },
		Providers:    appProviders(),
		Quota:        wiring.quota,
		Settings:     settingsStore,
		Reload:       appReload(dir),
		ReloadModels: modelsReload(dir, wiring.reload),
		ModelsStale:  catalogStale(time.Now()),
		Turn:         live.run,
		Paste:        paste.Board{Read: wiring.clipboard, Dir: live.pendingSessionDir, Recorded: live.recordAttachment},
		Answers:      answers,
		Steering:     steering,
		StopLead:     stopLead,
		Paths:        appPaths(dir),
		Sessions:     live.sessions,
		Resume:       live.resume,
		NewSession:   live.startFresh,
		Compact:      live.compact,
		Undo:         live.undo,
		Shells:       appShells(dir, launch.registry, launch.registryErr),
		KillShell:    appKillShell(launch.registry, launch.registryErr),
		RunCommand:   live.runCommand,
		Fresh:        launch.fresh,
		Resumed:      resumedChat(launch.resumed),
		Keymap:       shortcuts,
		Agents:       func() roster.Found { found, _ := discoverAgents(); return found },
		Cron:         live.cron,
	}
}

const (
	reloadStampSuffix = ".reload"
	reloadSucceeded   = "succeeded"
	reloadFailed      = "failed"
	reloadFound       = "models reload found "
)

func catalogStale(now time.Time) bool {
	catalog, err := models.CatalogDir()
	if err != nil {
		return false
	}
	stamp := catalog + reloadStampSuffix
	outcome, readErr := os.ReadFile(stamp)
	info, statErr := os.Stat(stamp)
	if readErr != nil || statErr != nil {
		return true
	}
	age := now.Sub(info.ModTime())
	switch string(outcome) {
	case reloadSucceeded:
		return age >= konst.CatalogStaleHours*time.Hour
	case reloadFailed:
		return age >= konst.CatalogRetryHours*time.Hour
	}
	return true
}

func stampReload(exit int) {
	catalog, err := models.CatalogDir()
	if err != nil || os.MkdirAll(filepath.Dir(catalog), 0o755) != nil {
		return
	}
	outcome := reloadFailed
	if exit == exitOK {
		outcome = reloadSucceeded
	}
	_ = os.WriteFile(catalog+reloadStampSuffix, []byte(outcome), 0o644)
}

func modelsReload(dir string, reload func(context.Context, io.Writer) int) func() string {
	if reload == nil {
		return nil
	}
	return func() string {
		before, unreadBefore := modelLibrary(dir)
		stampReload(reload(context.Background(), io.Discard))
		after, err := modelLibrary(dir)
		if err != nil {
			return models.ReloadVerb + ": " + err.Error()
		}
		var found []string
		for _, model := range after.Models {
			known := slices.ContainsFunc(before.Models, func(was models.Model) bool { return was.Slug() == model.Slug() })
			if !known && model.Use != models.UseExcluded {
				found = append(found, model.Slug())
			}
		}
		if len(found) == 0 || unreadBefore != nil {
			return ""
		}
		return reloadFound + strings.Join(found, ", ")
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
	live := appWiring{open: openAppWire, wires: appWires, blockers: appRequirements, quota: appQuota, reload: reloadAccounts}
	launch := launchOf(dir, resumed, resumed.Session == "")
	err = tui.Run(appOptions(dir, runOpts{}, live, launch))
	(*launch.stopWarm)()
	for _, warning := range (*launch.endSession)() {
		_, _ = fmt.Fprintln(errOut, "tofu: "+warning)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintln(out, sessionEndLine(launch.registry, launch.registryErr))
	leaveShells(launch.registry)
	return exitOK
}

func leaveShells(registry *shell.Registry) {
	if registry == nil || registry.Lifetime == shell.OutlivesTofu {
		return
	}
	for _, one := range registry.Own() {
		_ = registry.Kill(one.Name)
	}
}

func sessionEndLine(registry *shell.Registry, openErr error) string {
	var running []string
	if openErr == nil {
		for _, one := range registry.Own() {
			running = append(running, one.Name+" ("+one.Command+")")
		}
	}
	if len(running) == 0 {
		return sessionEnded
	}
	if registry.Lifetime == shell.OutlivesTofu {
		return sessionEnded + leavingPrefix + strings.Join(running, ", ")
	}
	return sessionEnded + stoppingPrefix + strings.Join(running, ", ")
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
	return func() *exec.Cmd {
		return exec.Command(os.Args[0], append([]string{"login"}, loginWords(provider)...)...)
	}
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
	return append(wires, keyWires()...)
}

func keyWires() []tui.Wire {
	if _, err := jev.KeyFor(sys.CredentialFileName, models.Meta.KeyName()); err != nil {
		return nil
	}
	library, err := modelLibrary("")
	if err != nil {
		return nil
	}
	chosen, err := library.KeyDefault(models.Meta)
	if err != nil {
		return nil
	}
	return []tui.Wire{{Name: wireMeta, Model: chosen.ID, Provider: string(models.Meta), Efforts: chosen.Efforts}}
}

type startStep struct {
	label   string
	step    string
	what    string
	command string
	done    string
	choices []tui.Choice
}

func startSteps() []startStep {
	model := startStep{
		label:   string(cred.ClaudeSub),
		step:    modelStep,
		what:    noCredential,
		command: loginHint(string(cred.ClaudeSub)),
		choices: []tui.Choice{
			{Label: "Claude subscription, signs in through the browser", Run: loginCommand(string(cred.ClaudeSub))},
			{Label: "Codex subscription, signs in through the browser", Run: loginCommand(string(cred.CodexSub))},
			{Label: "Meta API key", Key: sys.MetaMuseKeyName},
		},
	}
	if wires := appWires(); len(wires) > 0 {
		model.done = wires[0].Provider + " · " + wires[0].Model
	}
	classifier := startStep{
		label:   jevName,
		step:    classifierStep,
		what:    noGateKey,
		command: loginHint(openRouterName),
		choices: []tui.Choice{
			{Label: "OpenRouter key", Key: sys.OpenRouterKeyName},
			{Label: "TypeSafe key", Key: sys.TypeSafeKeyName},
		},
	}
	if key, err := gateKey(); err == nil {
		bound, _ := boundClassifier()
		classifier.done = bound.Provider.Display() + " key " + widget.Mask(key)
	}
	return []startStep{model, classifier}
}

func startBlockers() []startStep {
	return slices.DeleteFunc(startSteps(), func(step startStep) bool { return step.done != "" })
}

func appRequirements() []tui.Requirement {
	steps := startSteps()
	needed := make([]tui.Requirement, 0, len(steps))
	for _, step := range steps {
		needed = append(needed, tui.Requirement{Step: step.step, What: step.what, Fix: step.command, Done: step.done, Choices: step.choices})
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
		row.Fix = "run " + loginHint(string(provider))
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
	key, keyErr := jev.Key(sys.CredentialFileName)
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

func appShells(dir string, registry *shell.Registry, openErr error) func() []shells.Entry {
	return func() []shells.Entry {
		if openErr != nil {
			return nil
		}
		found, err := registry.List()
		if err != nil {
			return nil
		}
		tail, err := strconv.Atoi(settingText(dir, settingspkg.LogTail, nil))
		if err != nil {
			tail = konst.ShellLogTailLinesDefault
		}
		entries := make([]shells.Entry, 0, len(found))
		for _, one := range found {
			entry := shells.Entry{Name: one.Name, Command: one.Command, Started: one.Started, Ended: one.Ended, ExitCode: one.ExitCode, PID: one.PID, Dir: one.Dir, Owner: ownerName(one.Owner)}
			switch one.State {
			case shell.Running:
				entry.State = shells.Running
				if one.LeftOver() {
					entry.State = shells.LeftOver
				}
			case shell.Exited:
				entry.State = shells.Exited
			case shell.Killed:
				entry.State = shells.Killed
			}
			entry.Log, _ = registry.Tail(one.Name, tail)
			entries = append(entries, entry)
		}
		return entries
	}
}

func ownerName(owner string) string {
	at := strings.LastIndex(owner, roundMark)
	if at < 0 {
		return owner
	}
	if _, err := strconv.Atoi(owner[at+len(roundMark):]); err != nil {
		return owner
	}
	return owner[:at]
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
	var quotas []frame.Quota
	for _, result := range results {
		if result.err != nil {
			continue
		}
		for _, window := range result.report.Windows {
			quotas = append(quotas, frame.Quota{
				Label:    string(result.report.Provider) + " " + window.ID,
				Account:  "#" + strconv.FormatInt(result.row, 10),
				Fraction: window.Used.Fraction,
				Reported: window.Used.Reported,
				ResetsAt: window.ResetsAt,
			})
		}
	}
	return quotas
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
	opts.effort = selected.EffortTaken(opts.effort)
	held, spend, err := openAccounts(opts, selected)
	return appWire{held: held, spend: spend, selected: selected}, err
}

func awaitPerson(emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns, answers <-chan tui.Answer, granted map[string]bool) turn.Person {
	return func(ctx context.Context, request turn.GateRequest, decision turn.GateDecision) (turn.PersonAnswer, error) {
		place := askedPlace(request)
		var overriding struct{ Rule, Question string }
		if request.Tool == (tools.RuleOverride{}).Name() {
			_ = json.Unmarshal(request.Args, &overriding)
		}
		switch {
		case overriding.Question != "":
			emit(tui.Event{Kind: tui.EventNote, Text: overriding.Question})
			emit(tui.Event{Kind: tui.EventDecision, Decision: &session.Decision{Tool: request.Tool, Verdict: session.Ask, OverridesRule: overriding.Rule}})
		case granted[place]:
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
				if overriding.Question == "" {
					granted[place] = true
				}
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
	dir      string
	arms     runOpts
	open     func(runOpts) (appWire, error)
	answers  <-chan tui.Answer
	steer    <-chan string
	stopLead <-chan struct{}
	now      func() time.Time
	id       string
	carried  []llm.Message
	reads    *turn.ReadLedger
	warm     *warmProcesses
	tabs     *tools.BrowserTabs
	shown    map[string]bool
	granted  map[string]bool
	pending  []pendingImage
	shells   *shell.Registry
	inbox    *turn.Inbox
	roster   *roster.Roster
	cron     *cron.Book
	ranLock  sync.Mutex
	ran      []string
	started  string
}

type pendingImage struct {
	index int
	name  string
}

func newAppSession(dir string, open func(runOpts) (appWire, error), answers <-chan tui.Answer, now func() time.Time, resumed sessionResume) *appSession {
	live := &appSession{
		dir:     dir,
		open:    open,
		answers: answers,
		now:     now,
		id:      resumed.Session,
		shown:   map[string]bool{},
		granted: map[string]bool{},
		cron:    &cron.Book{Check: cronChecker(dir)},
		started: sessionStartup,
	}
	if resumed.Session != "" {
		live.started = sessionResumed
	}
	live.carry(resumed.messages)
	return live
}

func (s *appSession) end() []string {
	return turn.EndSession(context.Background(), s.dir, s.id, sessionEndExit)
}

func (s *appSession) loadCron(id string) error {
	if id == "" {
		return s.cron.Load("")
	}
	store, err := sessionstore.Open()
	if err != nil {
		return err
	}
	return s.cron.Load(cronFile(store, id))
}

func (s *appSession) renew() {
	s.warm.Close()
	s.warm = newWarmProcesses(s.tabs)
	s.warm.checkers.Warm(s.dir)
	s.reads, s.inbox, s.roster = turn.NewReadLedger(), turn.NewInbox(), &roster.Roster{}
	s.takeRan()
}

func (s *appSession) runCommand(ctx context.Context, command string) (string, bool) {
	output, stopped := s.shellCommand(ctx, command)
	redactor := sys.LoadKeyRedactor()
	output = ansi.Strip(redactor.Redact(output))
	if !stopped {
		s.ranLock.Lock()
		s.ran = append(s.ran, redactor.Redact(ranPreface+command+"\n"+output))
		s.ranLock.Unlock()
	}
	return output, stopped
}

func (s *appSession) shellCommand(ctx context.Context, command string) (string, bool) {
	chosen, err := turn.ResolveRunShell(settingText(s.dir, settingspkg.Shell, nil))
	if err != nil {
		return err.Error(), false
	}
	bash, err := turn.NewBashToolFromShell(s.dir, chosen)
	if err != nil {
		return err.Error(), false
	}
	args, _ := json.Marshal(map[string]string{"command": command})
	result, err := bash.Run(turn.WithShellRegistry(ctx, s.shells), args)
	if err != nil {
		return err.Error(), false
	}
	return result.Content, result.Outcome == turn.ResultAborted
}

func (s *appSession) takeRan() string {
	s.ranLock.Lock()
	defer s.ranLock.Unlock()
	taken := s.ran
	s.ran = nil
	if len(taken) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(taken, "\n\n")
}

func (s *appSession) carry(messages []llm.Message) {
	s.renew()
	s.carried = messages
	for _, message := range messages {
		if message.ToolCallID != "" {
			s.shown[message.ToolCallID] = true
		}
	}
}

func (s *appSession) startFresh() string {
	s.renew()
	s.id, s.carried, s.pending, s.started = "", nil, nil, sessionCleared
	_ = s.loadCron("")
	return freshSessionNote
}

func (s *appSession) pendingID() string {
	if s.id == "" {
		s.id = sessionstore.NewEventID()
	}
	return s.id
}

func (s *appSession) pendingSessionDir() (string, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return "", err
	}
	return store.AttachmentDir(s.pendingID()), nil
}

func (s *appSession) recordAttachment(index int, name string, bytes int, format string) {
	store, err := sessionstore.Open()
	if err != nil {
		return
	}
	_ = store.AppendEvent(s.pendingID(), sessionstore.EventAttachment, sessionstore.Attachment{File: sessionstore.AttachmentPath(s.pendingID(), name), Bytes: bytes, Format: format})
	s.pending = append(s.pending, pendingImage{index: index, name: name})
}

func (s *appSession) takePendingImages(task string) ([]llm.Image, error) {
	var wanted []pendingImage
	for _, image := range s.pending {
		if strings.Contains(task, session.ImageToken(image.index)) {
			wanted = append(wanted, image)
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	dir, err := s.pendingSessionDir()
	if err != nil {
		return nil, err
	}
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

func imagesRefused(setting string, model models.Model, attached int) string {
	held := strconv.Itoa(attached) + " attached image(s) stayed out of this turn"
	switch setting {
	case settingspkg.ImagesOff:
		return "images is off, so " + held
	case settingspkg.ImagesAuto:
		if model.Vision == models.VisionBlind {
			return "images is auto and " + model.Slug() + " cannot see images, so " + held + "; pick a model that can, or set images to inline"
		}
		return ""
	case settingspkg.ImagesInline:
		return ""
	}
	panic("tofu: unknown images setting " + setting)
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

func (s *appSession) label(store *sessionstore.Store) (tui.Event, bool) {
	id := s.id
	if id == "" {
		return tui.Event{}, false
	}
	labelled := tui.Event{Kind: tui.EventSession, ID: id, Root: id}
	header, err := store.Header(id)
	if err != nil {
		if writeErr := store.Write(sessionstore.Header{ID: id, Root: id, At: s.now()}, nil); writeErr != nil {
			return labelled, true
		}
		header, err = store.Header(id)
		if err != nil {
			return labelled, true
		}
	}
	labelled.Root = cmp.Or(header.Root, id)
	if header.Name != nil {
		labelled.Text = *header.Name
	}
	return labelled, true
}

func (s *appSession) resume(handle string) (string, []tui.Event) {
	store, err := sessionstore.Open()
	if err != nil {
		return "the session store does not open, so nothing is carried: " + err.Error(), nil
	}
	carry, err := resumeOf(store, handle)
	if err != nil {
		return err.Error(), nil
	}
	s.id, s.pending, s.started = carry.Session, nil, sessionResumed
	s.carry(carry.messages)
	said := "continuing " + carry.Session + ", " + strconv.Itoa(carry.Carried) + " messages from " + sessionSteps(carry.Steps)
	if err := s.loadCron(carry.Session); err != nil {
		said += "; " + err.Error()
	}
	return said, resumedChat(carry)
}

func (s *appSession) sessions() ([]tui.SessionRow, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return nil, err
	}
	now := s.now()
	report, err := sessionListing(store, sessionstore.DefaultSettings().Lifetime, now)
	if err != nil {
		return nil, err
	}
	rows := make([]tui.SessionRow, len(report.Sessions))
	for i, row := range report.Sessions {
		rows[i] = tui.SessionRow{ID: row.ID, Name: row.Name, Task: oneLine(row.Task), Facts: sessionWhen(row.At, now) + " · " + countOf(row.Turns, "turn"), InUse: row.ID == s.id}
	}
	return rows, nil
}

func gateOffEvent(gateErr error) tui.Event {
	var missing jev.MissingKey
	errors.As(gateErr, &missing)
	return tui.Event{Kind: tui.EventGateOff, Text: gateErr.Error(), GateWhy: missing.Why}
}

func pickedOpts(dir, session, task string, pick tui.Pick, maxSteps int) runOpts {
	return onTheBoundKeyWire(runOpts{
		dir:              dir,
		task:             task,
		turnID:           turn.NewID(time.Now()),
		session:          session,
		wire:             cmp.Or(pick.Wire, wireSubscription),
		model:            pick.Model,
		toolSet:          toolSetFull,
		effort:           cmp.Or(pick.Effort, llm.EffortDefault),
		loopGuardRepeats: konst.TurnLoopGuardRepeats,
		loopGuardWindow:  konst.TurnLoopGuardWindow,
		maxSteps:         maxSteps,
	})
}

func redacting(emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) tui.CalledFromInsideTheTurnAndNeverAfterItReturns {
	mask := sys.LoadKeyRedactor().Redact
	return func(event tui.Event) {
		event.Text, event.Detail, event.Diff, event.Created = mask(event.Text), mask(event.Detail), mask(event.Diff), mask(event.Created)
		if event.Decision != nil {
			decided := *event.Decision
			decided.Failure, decided.OverridesRule = mask(decided.Failure), mask(decided.OverridesRule)
			event.Decision = &decided
		}
		event.Plan = slices.Clone(event.Plan)
		for i := range event.Plan {
			event.Plan[i].Phase, event.Plan[i].Text = mask(event.Plan[i].Phase), mask(event.Plan[i].Text)
		}
		event.SubAgents = slices.Clone(event.SubAgents)
		for i := range event.SubAgents {
			row := &event.SubAgents[i]
			row.Doing, row.Report = mask(row.Doing), mask(row.Report)
			row.Owns, row.Calls = slices.Clone(row.Owns), slices.Clone(row.Calls)
			for j := range row.Owns {
				row.Owns[j] = mask(row.Owns[j])
			}
			for j := range row.Calls {
				row.Calls[j].Text, row.Calls[j].Result = mask(row.Calls[j].Text), mask(row.Calls[j].Result)
			}
		}
		emit(event)
	}
}

func (s *appSession) run(ctx context.Context, pick tui.Pick, task string, emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {
	emit = redacting(emit)
	fail := func(err error) { emit(tui.Event{Kind: tui.EventFailure, Text: err.Error()}) }
	task += s.takeRan()
	images, imagesErr := s.takePendingImages(task)
	if imagesErr != nil {
		fail(imagesErr)
		return
	}
	say := func(unreadable string) { emit(tui.Event{Kind: tui.EventNote, Text: unreadable}) }
	opts := pickedOpts(s.dir, s.pendingID(), task, pick, cmp.Or(s.arms.maxSteps, settingInt(s.dir, settingspkg.DecisionCap, say)))
	opts.gateArm, opts.siftArm, opts.noInstructions, opts.noDocs = s.arms.gateArm, s.arms.siftArm, s.arms.noInstructions, s.arms.noDocs
	opts.toolSet, opts.contextCeiling = cmp.Or(s.arms.toolSet, opts.toolSet), s.arms.contextCeiling
	opts.noSubAgents = s.arms.noSubAgents || settingInt(s.dir, settingspkg.TurnMaySpawn, say) == 0
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
	if len(images) > 0 {
		if refused := imagesRefused(settingText(s.dir, settingspkg.Images, say), opened.selected, len(images)); refused != "" {
			say(refused)
			images = nil
		}
	}
	built, plan, builtErr := buildRunToolsForRun(s.dir, opts.toolSet, readsWhen(opts.readBeforeEdit, s.reads), s.warm, shell)
	sessions, sessionsErr := sessionstore.Open()
	if err := cmp.Or(builtErr, sessionsErr); err != nil {
		fail(err)
		return
	}
	if pick.Fired != "" {
		built = withoutPush(built)
	}
	if err := s.cron.Keep(cronFile(sessions, opts.session)); err != nil {
		say("cron jobs were not written: " + err.Error())
	}
	if labelled, named := s.label(sessions); named {
		emit(labelled)
	}
	var gate *toolGate
	var gateErr error
	if opts.gateArm != gateOff {
		gate, gateErr = newToolGate(s.dir)
	}
	var unusable unusableRule
	if errors.As(gateErr, &unusable) {
		fail(fmt.Errorf("no turn starts while the tool gate rule is unusable: %w", unusable))
		return
	}
	if gateErr != nil {
		emit(gateOffEvent(gateErr))
	}
	sifter, scorer, siftErr := buildShellSift(opts.siftArm)
	if siftErr != nil {
		say("no shell result is cut: " + siftErr.Error())
	}
	budget, budgetErr := contextBudget(opts, opened.selected)
	if budgetErr != nil {
		fail(budgetErr)
		return
	}
	watch := &appWatcher{gate: gate, held: s.roster, emit: emit, now: s.now, turnID: opts.turnID, seen: s.shown, maxSteps: opts.maxSteps, stop: &leadStop{}}
	opened.held.wrap = func(model turn.Model) (turn.Model, error) {
		asked, guardErr := guarded(model, budget)
		if guardErr != nil {
			return nil, guardErr
		}
		watch.inner = asked
		return watch, nil
	}
	wrapSubAgent := func(model turn.Model) (turn.Model, error) {
		asked, guardErr := guarded(model, budget)
		return watchedSubAgent{watch: watch, inner: asked}, guardErr
	}
	notify := func(notice string) { emit(tui.Event{Kind: tui.EventNote, Text: notice}) }
	var person turn.Person
	if s.answers != nil {
		person = awaitPerson(emit, s.answers, s.granted)
	}
	config, spawner, configErr := runConfig(opts, built, runtime{accounts: opened.held.forTurn(), spend: opened.spend, budget: budget, gate: gate, sift: sifter, scorer: scorer, sessions: sessions, notify: notify, roster: s.roster, inbox: s.inbox, now: s.now,
		open: s.open, wrapSubAgent: wrapSubAgent, orchestrator: opened.selected, tabs: s.tabs, leadAsks: person, cron: s.cron})
	if configErr != nil {
		fail(configErr)
		return
	}
	config.SessionSource, s.started = s.started, ""
	if gate != nil {
		gate.watch = func(ctx context.Context, tool string, gated turn.GateDecision, err error) {
			decided := session.Decision{Tool: tool, Verdict: session.Ask}
			switch {
			case err != nil:
				decided.Failure = err.Error()
			case gated.Verdict != ledger.VerdictUnset:
				decided = gateDecision(tool, gated)
				decided.Enforced = config.GateMode == turn.GateEnforce
			default:
				return
			}
			emit(tui.Event{Kind: tui.EventDecision, Decision: &decided, Agent: turn.SubAgentAsking(ctx), Promote: watch.spawning(tool)})
		}
	}
	config.History = s.carried
	config.Images = images
	config.Person = person
	if settingText(s.dir, settingspkg.GatePrompt, say) != settingspkg.GatePromptAsk {
		config.Person = person.RunsWhatJevAsks()
	}
	if s.steer != nil {
		config.Steering = func() []string { return steered(s.steer, emit) }
	}
	watch.spawner = spawner
	config.ToolResult = func(answered llm.Message) { watch.result(answered, "") }
	config.Step = func(step turn.StepRow) {
		if plan != nil {
			emit(tui.Event{Kind: tui.EventPlan, Plan: statedPlan(plan.Items())})
		}
		if step.Occupancy == nil {
			return
		}
		emit(tui.Event{Kind: tui.EventContext, Context: frame.Context{
			Used:   step.Occupancy.Total(),
			Budget: budget.CeilingTokens,
		}})
	}
	config.EndedSession = func(ended turn.Row) error {
		emit(tui.Event{Kind: tui.EventForkStart})
		emit(tui.Event{Kind: tui.EventNote, Text: endedForkWords(ended)})
		emit(tui.Event{Kind: tui.EventForkEnd})
		return nil
	}
	stopClocks := watch.clockRunningSubAgents()
	stopListening := watch.stop.listen(s.stopLead)
	heard := func(typed string) { emit(tui.Event{Kind: tui.EventSteered, Text: typed}) }
	var reported []error
	leadErr := turn.Lead(turn.WithShellRegistry(ctx, s.shells), config, s.steer, heard, func(row turn.Row, err error) {
		watch.stop.reset()
		if err != nil {
			reported = append(reported, err)
		}
		words := doneWords(row.Outcome, row.Guard)
		switch {
		case errors.Is(err, context.Canceled):
			words = cancelledAt
		case err != nil:
			fail(err)
		}
		if row.Conversation != nil {
			s.carried = turn.Sendable(row.Conversation)
		}
		if row.Session != "" {
			forked := row.Session != s.id
			s.id = row.Session
			if headErr := sessions.SetHead(row.Session); headErr != nil {
				fail(headErr)
			}
			if keepErr := s.cron.Keep(cronFile(sessions, row.Session)); keepErr != nil && forked {
				say("cron jobs were not written: " + keepErr.Error())
			}
			if labelled, named := s.label(sessions); named && forked {
				emit(labelled)
			}
		}
		watch.readCalls()
		if said, ended := wordsAfterLastCalls(row); ended && strings.TrimSpace(row.Steps[len(row.Steps)-1].AssistantText) == "" {
			emit(tui.Event{Kind: tui.EventText, Text: said})
		}
		done := tui.Event{Kind: tui.EventDone, Text: words}
		if ctx.Err() == nil {
			done.SubAgents = watch.subAgents()
		}
		emit(done)
	})
	stopClocks()
	stopListening()
	watch.sendSubAgents()
	if unseen := unreported(leadErr, reported); unseen != nil {
		fail(unseen)
	}
}

func unreported(err error, reported []error) error {
	parts := []error{err}
	if joined, many := err.(interface{ Unwrap() []error }); many {
		parts = joined.Unwrap()
	}
	unseen := slices.DeleteFunc(slices.Clone(parts), func(part error) bool {
		return slices.ContainsFunc(reported, func(seen error) bool { return errors.Is(part, seen) })
	})
	return errors.Join(unseen...)
}

func endedForkWords(ended turn.Row) string {
	last := len(ended.Steps) - 1
	if last < 0 || ended.Steps[last].Fork == nil {
		return forkWords(ended.ForkedInto, "", nil)
	}
	fork := ended.Steps[last].Fork
	return forkWords(ended.ForkedInto, string(fork.Kind),
		&contextForkCounts{TokensBefore: fork.TokensBefore, TokensAfter: fork.TokensAfter})
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
	case turn.OutcomeRetiredCostCap:
		return "stopped at a cap this build no longer sets, after"
	case turn.OutcomeRetiredWallClockCap:
		return "stopped at the wall clock cap after"
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
	marks     sync.Mutex
	shows     sync.Mutex
	spent     map[string]int
	asked     map[string][]subagent.Call
	calls     map[string][]subagent.Call
	spawns    []string
	thoughts  atomic.Int64
	stop      *leadStop
}

type watchedSubAgent struct {
	watch *appWatcher
	inner turn.Model
}

func (c watchedSubAgent) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	return c.watch.askThrough(ctx, c.inner, request)
}

func (a *appWatcher) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if a.stop != nil && turn.SubAgentAsking(ctx) == "" {
		var release context.CancelFunc
		ctx, release = a.stop.during(ctx)
		defer release()
		if err := ctx.Err(); err != nil {
			return llm.Decision{}, err
		}
	}
	return a.askThrough(ctx, a.inner, request)
}

type leadStop struct {
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
}

func (l *leadStop) during(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopped {
		cancel()
	}
	l.cancel = cancel
	return ctx, cancel
}

func (l *leadStop) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	if l.cancel != nil {
		l.cancel()
	}
}

func (l *leadStop) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped, l.cancel = false, nil
}

func (l *leadStop) listen(stops <-chan struct{}) (quiet func()) {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-stops:
				l.stop()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func (a *appWatcher) askThrough(ctx context.Context, inner turn.Model, request llm.Request) (llm.Decision, error) {
	asker := turn.SubAgentAsking(ctx)
	for _, message := range request.Messages {
		if message.Role == llm.RoleTool {
			a.result(message, asker)
		}
	}
	thinking := a.eventID(asker, "thinking "+a.turnID+" "+strconv.FormatInt(a.thoughts.Add(1), 10))
	request.OnThinking = func(text string) {
		a.emit(tui.Event{Kind: tui.EventThinking, ID: thinking, Agent: asker, Text: text})
	}
	streamed := false
	request.OnRetry = func() {
		streamed = false
		a.emit(tui.Event{Kind: tui.EventStreamReset, ID: thinking, Agent: asker})
	}
	a.sendSubAgents()
	if asker == "" {
		a.emit(tui.Event{Kind: tui.EventRequesting})
		request.OnDelta = func(text string) {
			streamed = true
			a.emit(tui.Event{Kind: tui.EventTextDelta, Text: text})
		}
	}
	decision, err := inner.Ask(ctx, request)
	if err != nil {
		return decision, err
	}
	fresh := decision.PromptAccounting.FreshTokens(decision.Usage.InputTokens, decision.CacheReadTokens)
	a.marks.Lock()
	a.in += fresh
	a.out += decision.Usage.OutputTokens
	a.cacheRead += decision.CacheReadTokens
	stats := tui.Event{Kind: tui.EventStats, Agent: asker, Model: decision.Build, TokensIn: a.in, TokensOut: a.out, CacheRead: a.cacheRead}
	a.marks.Unlock()
	a.noteSubAgentsAsk(asker, fresh+decision.Usage.OutputTokens, decision.ToolCalls)
	if a.gate != nil {
		stats.Decisions = a.gate.decisions
	}
	a.emit(stats)
	if text := strings.TrimSpace(decision.Content); text != "" && !streamed && asker == "" {
		a.emit(tui.Event{Kind: tui.EventText, Text: text})
	}
	for _, call := range decision.ToolCalls {
		a.called(call, asker)
	}
	return decision, nil
}

func (a *appWatcher) spawning(tool string) bool {
	return a.spawner != nil && tool == a.spawner.Name()
}

func (a *appWatcher) eventID(asker, call string) string {
	return sessionstore.EventIDFor(cmp.Or(asker, a.turnID), call)
}

func (a *appWatcher) called(call llm.ToolCall, asker string) {
	intent, detail := callIntent(call)
	id, promotes := a.eventID(asker, call.ID), a.spawning(call.Name)
	a.marks.Lock()
	if promotes {
		a.spawns = append(a.spawns, id)
	}
	a.noteWholeFile(call)
	a.marks.Unlock()
	a.emit(tui.Event{Kind: tui.EventToolCall, ID: id, Tool: call.Name, Text: intent, Detail: detail, Promote: promotes, Agent: asker})
}

func resumedChat(carry sessionResume) []tui.Event {
	if carry.Session == "" {
		return nil
	}
	var chat []tui.Event
	watch := &appWatcher{emit: redacting(func(event tui.Event) { chat = append(chat, event) }), turnID: carry.Session, seen: map[string]bool{}, spawner: &turn.SpawnTool{}}
	root := carry.Session
	if store, err := sessionstore.Open(); err == nil {
		if header, err := store.Header(carry.Session); err == nil {
			root = cmp.Or(header.Root, root)
		}
	}
	watch.emit(tui.Event{Kind: tui.EventSession, Text: carry.Name, ID: carry.Session, Root: root})
	for _, message := range carry.messages {
		switch message.Role {
		case llm.RoleUser:
			watch.emit(tui.Event{Kind: tui.EventTask, Text: carry.taskIn(message.Content)})
		case llm.RoleAssistant:
			if text := strings.TrimSpace(message.Content); text != "" {
				watch.emit(tui.Event{Kind: tui.EventText, Text: text})
			}
			for _, call := range message.ToolCalls {
				watch.called(call, "")
			}
		case llm.RoleTool:
			watch.result(message, "")
		case llm.RoleSystem, llm.RoleUnknown:
		}
	}
	return chat
}

func (a *appWatcher) result(message llm.Message, asker string) {
	killedWithNothingToShow := message.ToolOutcome == llm.ToolOutcomeAborted && message.ToolResultBytes == 0
	a.marks.Lock()
	if a.seen[message.ToolCallID] || killedWithNothingToShow {
		a.marks.Unlock()
		return
	}
	a.seen[message.ToolCallID] = true
	result := tui.Event{
		Kind:   tui.EventToolResult,
		ID:     a.eventID(asker, message.ToolCallID),
		Text:   resultSummary(message.Content),
		Detail: message.Content,
		Bytes:  message.ToolResultBytes,
		Failed: message.ToolOutcome.Failed(),
		Agent:  asker,
	}
	if verdict, spawned := a.verdictOf(result.ID, message.Content); spawned {
		result.Text = verdict
	}
	if reply, asked := answerOf(message.Content); asked {
		result.Text = reply
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
	a.marks.Unlock()
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

func (a *appWatcher) anyWorking() bool {
	return a.held != nil && slices.ContainsFunc(a.held.SubAgents(), func(agent roster.SubAgent) bool { return agent.State == roster.Working })
}

func (a *appWatcher) noteSubAgentsAsk(subAgent string, tokens int, calls []llm.ToolCall) {
	if subAgent == "" {
		return
	}
	a.shows.Lock()
	if a.asked == nil {
		a.spent, a.asked = map[string]int{}, map[string][]subagent.Call{}
	}
	a.spent[subAgent] += tokens
	for _, call := range calls {
		a.asked[subAgent] = append(a.asked[subAgent], subagent.Call{ID: a.eventID(subAgent, call.ID), At: a.now(), Tool: call.Name})
	}
	a.shows.Unlock()
	a.sendSubAgents()
}

func (a *appWatcher) sendSubAgents() {
	a.readCalls()
	a.draw()
}

func (a *appWatcher) readCalls() {
	if a.held == nil {
		return
	}
	rows, agents := subAgentRows(a.spawner), a.held.SubAgents()
	a.shows.Lock()
	defer a.shows.Unlock()
	if a.calls == nil {
		a.calls = map[string][]subagent.Call{}
	}
	for _, agent := range agents {
		a.calls[agent.ID] = recordedOrCalling(recordedCalls(rows, agent.ID), a.asked[agent.ID], agent.Calling, agent.CallsDropped)
	}
}

func (a *appWatcher) draw() {
	if subAgents := a.subAgents(); len(subAgents) > 0 {
		a.emit(tui.Event{Kind: tui.EventSubAgent, SubAgents: subAgents})
	}
}

func (a *appWatcher) subAgents() []subagent.Row {
	if a.held == nil {
		return nil
	}
	agents := a.held.SubAgents()
	for index := range agents {
		if agents[index].State == roster.InReview && (a.spawner == nil || a.spawner.Review == nil) {
			agents[index].State = roster.Finished
		}
	}
	a.shows.Lock()
	defer a.shows.Unlock()
	return subagent.Rows(agents, a.now(), a.maxSteps, a.spent, func(agent roster.SubAgent) []subagent.Call { return a.calls[agent.ID] })
}

func (a *appWatcher) verdictOf(id, report string) (string, bool) {
	if !slices.Contains(a.spawns, id) {
		return "", false
	}
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, subAgentVerdict) {
			return line, true
		}
	}
	return "", false
}

func (a *appWatcher) clockRunningSubAgents() (stop func()) {
	ticking, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		every := time.NewTicker(konst.SubAgentRedrawMillis * time.Millisecond)
		defer every.Stop()
		for {
			select {
			case <-ticking:
				return
			case <-every.C:
				if a.anyWorking() {
					a.draw()
				}
			}
		}
	}()
	return func() {
		close(ticking)
		<-stopped
	}
}

func recordedOrCalling(recorded, asked []subagent.Call, calling []string, dropped int) []subagent.Call {
	for index := range recorded {
		if at := slices.IndexFunc(asked, func(call subagent.Call) bool { return call.ID == recorded[index].ID }); at >= 0 {
			recorded[index].At = asked[at].At
		}
	}
	switch {
	case len(recorded) > 0:
		for _, call := range asked {
			if !slices.ContainsFunc(recorded, func(done subagent.Call) bool { return done.ID == call.ID }) {
				recorded = append(recorded, call)
			}
		}
		return inThePane(recorded)
	case len(asked) > 0:
		return inThePane(asked)
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
	spawnedBy := ""
	for _, row := range rows {
		if row.ID == id {
			spawnedBy = row.SpawnedBy
		}
	}
	var calls []subagent.Call
	for _, row := range rows {
		if row.ID != id && (spawnedBy == "" || row.SpawnedBy != spawnedBy) {
			continue
		}
		for _, step := range row.Steps {
			for _, ran := range step.ToolCalls {
				calls = append(calls, subagent.Call{ID: ran.ID, Tool: ran.Tool, Text: ran.Command, Result: cmp.Or(ran.Error, byteSize(ran.ResultBytes))})
			}
		}
	}
	return calls
}

func inThePane(calls []subagent.Call) []subagent.Call {
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
	switch call.Name {
	case askTool:
		return text("question"), ""
	case messageTool:
		return text("to") + ": " + text("text"), ""
	case subAgentsTool:
		return "what each sub-agent is doing", ""
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
		brief, _ := fields["task"].(string)
		first, _, _ := strings.Cut(strings.TrimSpace(brief), "\n")
		return cmp.Or(text("mission"), strings.TrimSpace(first)), brief
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

func answerOf(content string) (string, bool) {
	first, _, _ := strings.Cut(content, "\n\n")
	if answer, answered := strings.CutPrefix(first, askAnswered); answered {
		return answeredReply + oneLine(answer), true
	}
	if assumed, wasAssumed := strings.CutPrefix(first, askAssumed); wasAssumed {
		return assumedReply + oneLine(assumed), true
	}
	return "", false
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
