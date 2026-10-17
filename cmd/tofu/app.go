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
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"

	"tofu/interface/tui"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/internal/host"
	"tofu/internal/judge/jev"
	"tofu/internal/keymap"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/memory"
	"tofu/internal/safe"
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
	roundMark        = "-r"
	artifactsDirName = "artifacts"
	sessionsDirName  = "sessions"
)

const (
	stateFileKeptDays = 7
	shellPruneEvery   = time.Hour
)

type appWiring struct {
	open      func(runOpts) (appWire, error)
	wires     func() []tui.Wire
	blockers  func() []tui.Requirement
	clipboard func() (sys.Clipboard, error)
	quota     func() []frame.Quota
	reload    func(ctx context.Context, out io.Writer) int
	now       func() time.Time
	detached  bool
}

type appLaunch struct {
	resumed       sessionResume
	fresh         bool
	registry      *shell.Registry
	registryErr   error
	note          string
	tabs          *tools.BrowserTabs
	release       *func()
	endSession    *func() []string
	stopRestoring func()
}

func launchOf(dir string, resumed sessionResume, fresh bool) appLaunch {
	registry, registryErr := launchShellRegistry(dir)
	home, _ := os.UserHomeDir()
	launch := appLaunch{resumed: resumed, fresh: fresh, registry: registry, registryErr: registryErr, tabs: tools.NewBrowserTabs(home), release: new(func()), endSession: new(func() []string), stopRestoring: func() {}}
	safe.Go(sys.Crashes(dir), "state sweep", func() {
		aged := time.Now().AddDate(0, 0, -stateFileKeptDays)
		if state, err := sys.ProjectStateDirAt(dir); err == nil {
			_ = sys.RemoveUnreferencedArtifacts(filepath.Join(state, artifactsDirName), filepath.Join(state, sessionsDirName), aged)
			_ = sys.RemoveAged(state, safe.CrashPrefix, safe.CrashSuffix, aged)
		}
		if stateHome, err := sys.HomeConfigDir(); err == nil {
			_ = sys.RemoveAged(stateHome, safe.CrashPrefix, safe.CrashSuffix, aged)
		}
	})
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

func appOptions(dir string, arms runOpts, wiring appWiring, launch appLaunch, held heldStore) tui.Options {
	defer launch.stopRestoring()
	places, _ := sys.ResolvePlaces(".")
	engine := &appEngine{dir: dir, arms: arms, open: wiring.open, quota: wiring.quota, tabs: launch.tabs, places: places}
	live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: cronChecker(dir), Resumed: launch.resumed.hosted(), Detached: wiring.detached, Now: wiring.now})
	notes := append([]string{launch.note}, troubles...)
	if _, err := gateKeyFrom(held.keys()); err != nil {
		notes = append(notes, gateOffNote)
	}
	note := strings.Join(slices.DeleteFunc(notes, func(one string) bool { return one == "" }), noteSeparator)
	*launch.release = func() {
		live.Close()
		engine.warm.Close()
	}
	*launch.endSession = func() []string {
		if wiring.detached {
			return nil
		}
		return turn.EndSession(context.Background(), dir, live.ID(), sessionEndExit)
	}
	settingsStore, _ := openSettings(dir)
	shortcuts, _ := keymap.ShortcutsPath()
	opening := live.Opening(launch.resumed.hosted())
	if note := resumeNote(launch.resumed); note != "" {
		opening = append(opening, tui.Event{Kind: tui.EventNote, Text: note})
	}
	if memoryOn(dir) {
		shelves, err := memory.Open(dir)
		if err != nil {
			shelves.Copied = append(shelves.Copied, "your memory is unreadable: "+err.Error())
		}
		for _, line := range shelves.Copied {
			opening = append(opening, tui.Event{Kind: tui.EventNote, Text: line})
		}
	}
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
		Providers:    held.providers(),
		Quota:        wiring.quota,
		Settings:     settingsStore,
		Reload:       appReload(dir),
		ReloadModels: modelsReload(dir, wiring.reload),
		ModelsStale:  catalogStale(time.Now()),
		Host:         live,
		Paste:        paste.Board{Read: wiring.clipboard, Dir: live.AttachmentDir, Recorded: live.Attached},
		Paths:        appPaths(dir),
		Sessions:     func() ([]tui.SessionRow, error) { return appSessions(live.ID()) },
		Resume:       func(handle string) (string, []tui.Event) { return appResume(live, handle) },
		NewSession:   func() string { return freshSession(live) },
		Compact:      func() string { return compactSession(live) },
		Undo:         func(count string) string { return undoTurns(live.ID(), count) },
		Updates:      appUpdates(dir),
		Shells:       appShells(dir, launch.registry, launch.registryErr),
		KillShell:    appKillShell(launch.registry, launch.registryErr),
		RunCommand: func(ctx context.Context, command string) (string, bool) {
			output, stopped := shellCommand(ctx, dir, launch.registry, command)
			return live.Ran(command, output, stopped), stopped
		},
		Fresh:   launch.fresh,
		Resumed: opening,
		Keymap:  shortcuts,
		Agents:  func() roster.Found { found, _ := discoverAgents(); return found },
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
		debug.FreeOSMemory()
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

func isTerminal(stream any) bool {
	file, isFile := stream.(*os.File)
	return isFile && term.IsTerminal(file.Fd())
}

func collectForALongLife() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(konst.LongLivedGCPercent)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(konst.LongLivedMemoryLimitBytes)
	}
}

func appVerb(in io.Reader, out, errOut io.Writer, resumed sessionResume) int {
	if !isTerminal(in) {
		_, _ = fmt.Fprintln(errOut, noTerminal)
		return exitUsage
	}
	collectForALongLife()
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu: the working directory is unreadable: %v\n", err)
		return exitVerdict
	}
	stopRestoring := restoring(out, resumed)
	held := holdStore()
	defer held.close()
	live := appWiring{open: openAppWire, wires: held.wires, blockers: held.requirements, quota: held.quota, reload: held.reload, detached: true}
	launch := launchOf(dir, resumed, resumed.Session == "")
	launch.stopRestoring = stopRestoring
	var keeperCrashed []string
	stopKeeping := keepAccountsAlive(held, func(string) {}, func(said string) { keeperCrashed = append(keeperCrashed, said) })
	var identity sync.WaitGroup
	var identityErr error
	identity.Go(func() {
		identityErr = crashed(dir, safe.Call(nil, "project identity", func() error { return sys.RecordIdentity(dir) }))
	})
	err = tui.Run(appOptions(dir, runOpts{}, live, launch, held))
	stopKeeping()
	for _, said := range keeperCrashed {
		_, _ = fmt.Fprintln(errOut, "tofu: "+said)
	}
	identity.Wait()
	if identityErr != nil {
		_, _ = fmt.Fprintf(errOut, "tofu: the git remote and root of this project were not recorded: %v\n", identityErr)
	}
	(*launch.release)()
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

func (h heldStore) wires() []tui.Wire {
	if h.err != nil {
		return nil
	}
	now := time.Now()
	var wires []tui.Wire
	for _, known := range []subscriptionSource{{cred.ClaudeSub, wireSubscription}, {cred.CodexSub, wireCodex}} {
		row, present, err := h.store.RowAt(known.source, now)
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
	return append(wires, keyWires(h.keys())...)
}

func keyWires(keys jev.Keyring) []tui.Wire {
	if _, err := keys.KeyFor(sys.CredentialFileName, models.Meta.KeyName()); err != nil {
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

func (h heldStore) startSteps() []startStep {
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
	if wires := h.wires(); len(wires) > 0 {
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
	keys := h.keys()
	if key, err := gateKeyFrom(keys); err == nil {
		bound, _ := classifierFrom(keys)
		classifier.done = bound.Provider.Display() + " key " + widget.Mask(key)
	}
	return []startStep{model, classifier}
}

func (h heldStore) startBlockers() []startStep {
	return slices.DeleteFunc(h.startSteps(), func(step startStep) bool { return step.done != "" })
}

func (h heldStore) requirements() []tui.Requirement {
	steps := h.startSteps()
	needed := make([]tui.Requirement, 0, len(steps))
	for _, step := range steps {
		needed = append(needed, tui.Requirement{Step: step.step, What: step.what, Fix: step.command, Done: step.done, Choices: step.choices})
	}
	return needed
}

func (h heldStore) providers() []settings.Provider {
	return []settings.Provider{
		h.subscriptionProvider(cred.ClaudeSub),
		openRouterProvider(h.keys()),
		jevProvider(),
		h.subscriptionProvider(cred.CodexSub),
	}
}

func (h heldStore) subscriptionProvider(provider cred.Provider) settings.Provider {
	row := settings.Provider{Name: string(provider), Source: h.path}
	if h.err != nil {
		row.Fix = unreadableSource + h.err.Error()
		return row
	}
	now := time.Now()
	stored, present, err := h.store.RowAt(provider, now)
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

func openRouterProvider(keys jev.Keyring) settings.Provider {
	located, err := locateGateKey(keys)
	row := settings.Provider{Name: openRouterName, Source: located.Path}
	if located.Source == jev.SourceEnvironment {
		row.Source = located.Name + " in the environment"
	}
	key, keyErr := keys.KeyFor(sys.CredentialFileName, jev.OpenRouterVariable)
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
	var tails shell.Tails
	return func() []shells.Entry {
		if openErr != nil {
			return nil
		}
		_ = registry.PruneEvery(shellPruneEvery)
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
			entry := shells.Entry{Name: one.Name, Command: one.Command, Started: one.Started, Ended: one.Ended, ExitCode: one.ExitCode, PID: one.PID, Dir: one.Dir, Owner: ownerName(one.Owner), OneShot: one.OneShot()}
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
			entry.Log, _ = tails.Tail(registry, one.Name, tail)
			entries = append(entries, entry)
		}
		tails.Forget(found)
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

func (h heldStore) quota() []frame.Quota {
	if h.err != nil {
		return nil
	}
	return quotaFrames(pollStore(context.Background(), h.store, time.Now, nil))
}

func quotaFrames(results []pollResult, err error) []frame.Quota {
	if err != nil {
		return nil
	}
	var quotas []frame.Quota
	for _, result := range results {
		for _, window := range result.report.Windows {
			quotas = append(quotas, frame.Quota{
				Label:    string(result.report.Provider) + " " + window.ID,
				Account:  "#" + strconv.FormatInt(result.row, 10),
				Fraction: window.Used.Fraction,
				Reported: window.Used.Reported,
				ResetsAt: window.ResetsAt,
				ReadAt:   result.report.FetchedAt,
				Stale:    result.report.Stale,
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

func shellCommand(ctx context.Context, dir string, registry *shell.Registry, command string) (string, bool) {
	chosen, err := turn.ResolveRunShell(settingText(dir, settingspkg.Shell, nil))
	if err != nil {
		return err.Error(), false
	}
	bash, err := turn.NewBashToolFromShell(dir, chosen)
	if err != nil {
		return err.Error(), false
	}
	args, _ := json.Marshal(map[string]string{"command": command})
	result, err := bash.Run(turn.WithShellRegistry(ctx, registry), args)
	if err != nil {
		return err.Error(), false
	}
	return result.Content, result.Outcome == turn.ResultAborted
}

func freshSession(live *host.Host) string {
	if err := live.Fresh(); err != nil {
		return err.Error()
	}
	return freshSessionNote
}

func appResume(live *host.Host, handle string) (string, []tui.Event) {
	store, err := sessionstore.Open()
	if err != nil {
		return "the session store does not open, so nothing is carried: " + err.Error(), nil
	}
	carry, err := resumeOf(store, handle)
	if err != nil {
		return err.Error(), nil
	}
	chat, err := live.Resume(carry.hosted())
	if err != nil {
		return err.Error(), nil
	}
	return "continuing " + carry.Session + ", " + strconv.Itoa(carry.Carried) + " messages from " + plural(carry.Steps, "step"), chat
}

func appSessions(inUse string) ([]tui.SessionRow, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	report, err := sessionListing(store, sessionstore.DefaultSettings().Lifetime, now)
	if err != nil {
		return nil, err
	}
	open, _ := store.Identity(inUse)
	rows := make([]tui.SessionRow, len(report.Sessions))
	for i, row := range report.Sessions {
		rows[i] = tui.SessionRow{ID: row.ID, Name: row.Handle, Task: oneLine(row.Task), InUse: row.ID == inUse || (open.Family != "" && row.Family == open.Family),
			Facts: sessionWhen(row.At, now) + " · " + plural(row.Turns, "turn") + " · " + plural(row.Generations, "generation")}
	}
	return rows, nil
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

func pickedOpts(dir string, start host.Turn, maxSteps int) runOpts {
	return onTheBoundKeyWire(runOpts{
		dir:              dir,
		task:             start.Task,
		turnID:           start.ID,
		session:          start.Session,
		wire:             cmp.Or(start.Pick.Wire, wireSubscription),
		model:            start.Pick.Model,
		toolSet:          toolSetFull,
		effort:           cmp.Or(start.Pick.Effort, llm.EffortDefault),
		loopGuardRepeats: konst.TurnLoopGuardRepeats,
		loopGuardWindow:  konst.TurnLoopGuardWindow,
		maxSteps:         maxSteps,
	})
}

type appEngine struct {
	dir    string
	arms   runOpts
	open   func(runOpts) (appWire, error)
	quota  func() []frame.Quota
	tabs   *tools.BrowserTabs
	warm   *warmProcesses
	places sys.Places
	timed  string
}

func (e *appEngine) Renew() {
	e.warm.Close()
	e.warm = newWarmProcesses(e.tabs)
	e.warm.checkers.Warm(e.dir)
}

func (e *appEngine) OneTurnPerProject() bool {
	return settingInt(e.dir, settingspkg.OneWorkingSessionPerProject, nil) != 0
}

func (e *appEngine) Prepare(start host.Turn, hooks host.Hooks) (host.Prepared, error) {
	say := hooks.Say
	clock := clockFromNow()
	opts := pickedOpts(e.dir, start, cmp.Or(e.arms.maxSteps, settingInt(e.dir, settingspkg.DecisionCap, say)))
	opts.gateArm, opts.siftArm, opts.noInstructions, opts.noDocs = e.arms.gateArm, e.arms.siftArm, e.arms.noInstructions, e.arms.noDocs
	opts.toolSet, opts.contextCeiling = cmp.Or(e.arms.toolSet, opts.toolSet), e.arms.contextCeiling
	opts.noSubAgents = e.arms.noSubAgents || settingInt(e.dir, settingspkg.TurnMaySpawn, say) == 0
	opts.readBeforeEdit = settingInt(e.dir, settingspkg.ReadBeforeEdit, say) != 0
	opts.planEveryStep = settingInt(e.dir, settingspkg.PlanEveryStep, say) != 0
	prepared := host.Prepared{MaxSteps: opts.maxSteps}
	shell, err := turn.ResolveRunShell(settingText(e.dir, settingspkg.Shell, say))
	if err != nil {
		return prepared, err
	}
	opts.shell = shell
	clock.step("settings")
	opened, err := e.open(opts)
	if opened.held != nil {
		prepared.Close = opened.held.close
	}
	if err != nil {
		return prepared, err
	}
	checked := opened.held.checkUsageAlongside(clock)
	prepared.Close = func() {
		checked()
		opened.held.close()
	}
	clock.step("model choice and credentials")
	images := start.Images
	if len(images) > 0 {
		if refused := imagesRefused(settingText(e.dir, settingspkg.Images, say), opened.selected, len(images)); refused != "" {
			say(refused)
			images = nil
		}
	}
	built, plan, builtErr := buildRunToolsForRun(e.dir, opts.toolSet, readsWhen(opts.readBeforeEdit, hooks.Reads), e.warm, shell)
	sessions, sessionsErr := sessionstore.Open()
	if err := cmp.Or(builtErr, sessionsErr); err != nil {
		return prepared, err
	}
	sessions.Redacts(e.warm.keys.redactor)
	built, plan = planAsOffered(opened.selected, built, plan)
	if start.Pick.Fired != "" {
		built = withoutPush(built)
	}
	clock.step("shell, tools and session store")
	places := e.places
	if places == (sys.Places{}) {
		if places, err = sys.ResolvePlaces("."); err != nil {
			return prepared, err
		}
	}
	var gate *toolGate
	var gateErr error
	if opts.gateArm != gateOff {
		gate, gateErr = toolGateOn(e.dir, places.Log, e.warm.keys.keyring())
	}
	var unusable unusableRule
	if errors.As(gateErr, &unusable) {
		return prepared, fmt.Errorf("no turn starts while the tool gate rule is unusable: %w", unusable)
	}
	sifter, scorer, siftErr := shellSiftOn(opts.siftArm, places.Log, e.warm.keys.keyring())
	if siftErr != nil {
		say("no shell result is cut: " + siftErr.Error())
	}
	clock.step("gate and sift")
	budget, err := contextBudget(opts, opened.selected)
	if err != nil {
		return prepared, err
	}
	clock.step("model catalogue")
	opened.held.wrap = func(model turn.Model) (turn.Model, error) {
		asked, guardErr := guarded(model, budget)
		if guardErr != nil {
			return nil, guardErr
		}
		return hooks.Lead(clock.leading(asked)), nil
	}
	wrapSubAgent := func(model turn.Model) (turn.Model, error) {
		asked, guardErr := guarded(model, budget)
		return hooks.SubAgent(asked), guardErr
	}
	person := hooks.Inbox.LeadAsks(hooks.Person)
	turnAccounts, others, account := opened.held.forTurn(), opened.held.others, spentAccount(opened.held, opened.selected.ID)
	if stand := e.standIns(opened.held, opts.wire); len(stand.mine()) > 0 {
		turnAccounts, others, account = stand.forTurn(), stand.others, stand.spent
	}
	config, spawner, err := runConfig(opts, built, runtime{accounts: clock.picking(turnAccounts, opened.held.store), spend: opened.spend, budget: budget, gate: gate, sift: sifter, scorer: scorer, sessions: sessions, notify: say, roster: hooks.Roster, inbox: hooks.Inbox, now: hooks.Now,
		open: e.open, wrapSubAgent: wrapSubAgent, orchestrator: opened.selected, tabs: e.tabs, leadAsks: person, cron: hooks.Cron, plan: plan, places: places, startup: clock, keys: &e.warm.keys, memory: &e.warm.memory})
	if err != nil {
		return prepared, err
	}
	clock.step("lead tools, board and spawn")
	if config.Session != e.timed {
		e.timed = config.Session
		closeAccounts := prepared.Close
		prepared.Close = func() {
			closeAccounts()
			if err := clock.record(sessions, config.Session); err != nil {
				say("the startup steps were not recorded: " + err.Error())
			}
		}
	}
	if gate != nil {
		gate.watch = hooks.Gate
		prepared.Decisions = func() int { return gate.decisions }
	}
	config.Images = images
	config.Person = person.Asking(config.GatePromptAsks)
	prepared.Config, prepared.Spawner, prepared.Plan, prepared.Ceiling, prepared.Sessions, prepared.GateOff = config, spawner, plan, budget.CeilingTokens, sessions, gateErr
	prepared.Account, prepared.Others = account, others
	return prepared, nil
}

func (e *appEngine) standIns(held *accounts, wire string) standIns {
	if _, recorded := held.fixed.(*cassette); !recorded || e.quota == nil {
		return standIns{}
	}
	return standIns{held: held, source: string(subscriptionOf(wire)), readings: e.quota()}
}

func spentAccount(held *accounts, model string) func(int64) host.TurnAccount {
	return func(id int64) host.TurnAccount {
		spent := host.TurnAccount{Source: string(held.provider), AccountID: id, Model: model}
		if held.store == nil {
			return spent
		}
		rows, err := held.store.List()
		if at := slices.IndexFunc(rows, func(row cred.Row) bool { return row.ID == id }); err == nil && at >= 0 {
			spent.Login = accountName(rows[at].Credential.Identity, false)
		}
		return spent
	}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
