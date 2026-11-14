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

	"tofu/apps/cli/output"
	loginverb "tofu/apps/cli/verbs/login"
	modelsverb "tofu/apps/cli/verbs/models"
	shellsverb "tofu/apps/cli/verbs/shells"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	roster "tofu/internal/agent/subagents"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	llm "tofu/internal/models/llm/core"
	platformclipboard "tofu/internal/platform/clipboard"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/outside"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/platform/scratch"
	memory "tofu/internal/store/memory/entries"
	sessionstore "tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/store/walls"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
	"tofu/internal/tools/shell/process"
	"tofu/internal/tools/web/browser"
	"tofu/internal/ui/keymap"
	"tofu/internal/ui/tui/app"
	"tofu/internal/ui/tui/composer/paste"
	"tofu/internal/ui/tui/frame/frame"
	"tofu/internal/ui/tui/panels/shells"
)

const (
	noTerminal       = "tofu: the app needs a terminal. with input redirected, use tofu run --dir <dir> <task>"
	freshSessionNote = "the next task starts a new session and carries nothing from the last one"
	sessionEnded     = "tofu: session ended"
	stoppingPrefix   = ", stopping "
	leavingPrefix    = ", leaving running for the next launch "
	leftOverNote     = "shells left over from an earlier tofu: "
	leftOverFix      = ", end them in the shells screen"
	noteSeparator    = "  ·  "
	artifactsDirName = "artifacts"
	sessionsDirName  = "sessions"
	sweptStampName   = "swept"
)

const (
	stateFileKeptDays = 7
	shellPruneEvery   = time.Hour
)

type appWiring struct {
	open      wiring.Opener
	wires     func() []app.Wire
	blockers  func() []app.Requirement
	clipboard func() (platformclipboard.Clipboard, error)
	copy      func(text string) error
	quota     func() []frame.Quota
	update    func(ctx context.Context, out io.Writer) int
	now       func() time.Time
	joins     bool
}

type appLaunch struct {
	resumed       sessionResume
	fresh         bool
	registry      *process.Registry
	registryErr   error
	note          string
	tabs          *browser.BrowserTabs
	release       *func()
	endSession    *func() []string
	turnRunning   *func() (string, bool)
	stopRestoring func()
	started       time.Time
}

func launchOf(dir string, resumed sessionResume, fresh bool) appLaunch {
	started, known := processStarted()
	if !known {
		started = time.Now()
	}
	registry, registryErr := shellsverb.LaunchShellRegistry(dir)
	home, _ := os.UserHomeDir()
	launch := appLaunch{resumed: resumed, fresh: fresh, registry: registry, registryErr: registryErr, tabs: browser.NewBrowserTabs(home), release: new(func()), endSession: new(func() []string), turnRunning: new(func() (string, bool)), stopRestoring: func() {}, started: started}
	state, stateErr := platformproject.ProjectStateDirAt(dir)
	if stateErr == nil {
		stateErr = os.MkdirAll(state, 0o755)
	}
	safe.Go(platformproject.Crashes(dir), "state sweep", func() {
		if stateErr != nil {
			return
		}
		scratch.OncePerDay(filepath.Join(state, sweptStampName), time.Now(), func() {
			aged := time.Now().AddDate(0, 0, -stateFileKeptDays)
			_ = scratch.RemoveUnreferencedArtifacts(filepath.Join(state, artifactsDirName), filepath.Join(state, sessionsDirName), aged)
			_ = scratch.RemoveAged(state, safe.CrashPrefix, safe.CrashSuffix, aged)
			if stateHome, err := fs.HomeConfigDir(); err == nil {
				_ = scratch.RemoveAged(stateHome, safe.CrashPrefix, safe.CrashSuffix, aged)
			}
		})
	})
	if registryErr != nil {
		return launch
	}
	_ = registry.Prune()
	found, _ := registry.List()
	if leftOver := len(slices.DeleteFunc(found, func(one process.Shell) bool { return !one.LeftOver() })); leftOver > 0 {
		launch.note = leftOverNote + strconv.Itoa(leftOver) + leftOverFix
	}
	return launch
}

func appOptions(dir string, arms wiring.RunOpts, wired appWiring, launch appLaunch, held wiring.HeldStore) app.Options {
	defer launch.stopRestoring()
	places, _ := platformproject.ResolvePlaces(".")
	engine := &appEngine{dir: dir, arms: arms, open: wired.open, quota: wired.quota, tabs: launch.tabs, places: places}
	var readKeys func() (map[string]string, error)
	if held.Store != nil {
		readKeys = held.Store.Keys
	}
	live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: shellsverb.CronChecker(dir), Resumed: launch.resumed.hosted(), Joins: wired.joins, Now: wired.now, ReadKeys: readKeys})
	notes := append([]string{launch.note}, troubles...)
	if _, err := wiring.GateKeyFrom(held.Keys()); err != nil {
		notes = append(notes, wiring.GateOffSaid(err))
	}
	note := strings.Join(slices.DeleteFunc(notes, func(one string) bool { return one == "" }), noteSeparator)
	*launch.release = func() {
		live.Close()
		engine.warm.Close()
	}
	*launch.turnRunning = live.Turn
	*launch.endSession = func() []string {
		if wired.joins {
			return nil
		}
		return loop.EndSession(context.Background(), dir, live.ID(), sessionEndExit)
	}
	settingsStore, _ := settingspkg.OpenFor(dir)
	shortcuts, _ := keymap.ShortcutsPath()
	opening := live.Opening(launch.resumed.hosted())
	if note := resumeNote(launch.resumed); note != "" {
		opening.Chat = append(opening.Chat, app.Event{Kind: app.EventNote, Text: note})
	}
	if memoryOn(dir) {
		shelves, err := memory.Open(dir)
		if err != nil {
			shelves.Copied = append(shelves.Copied, "your memory is unreadable: "+err.Error())
		}
		for _, line := range shelves.Copied {
			opening.Chat = append(opening.Chat, app.Event{Kind: app.EventNote, Text: line})
		}
	}
	return app.Options{
		Repo:         filepath.Base(dir),
		Root:         dir,
		Branch:       branchOf(dir),
		Note:         note,
		Requirements: wired.blockers(),
		Recheck:      wired.blockers,
		SignIn:       func(source string) *exec.Cmd { return wiring.LoginCommand(source)() },
		SaveKey:      loginverb.StoreKeyFor,
		Wires:        wired.wires,
		Models:       func() (catalog.Library, error) { return wiring.ModelLibrary(dir) },
		Providers:    held.Providers(),
		Quota:        wired.quota,
		Settings:     settingsStore,
		Reload:       appReload(dir),
		UpdateModels: modelsUpdate(dir, wired.update),
		ModelsStale:  wiring.CatalogStale(time.Now()),
		Host:         live,
		Paste:        paste.Board{Read: wired.clipboard, Dir: live.AttachmentDir, Recorded: live.Attached},
		Copy:         wired.copy,
		Paths:        appPaths(dir),
		Sessions:     func() ([]app.SessionRow, error) { return appSessions(live.ID()) },
		Resume:       func(handle string) (string, host.Opened) { return appResume(live, handle) },
		NewSession:   func() string { return freshSession(live) },
		Compact:      func(focus string) string { return forkNote(live, loop.ForkCompact, focus) },
		Fork:         func() string { return forkNote(live, loop.ForkByHand, "") },
		BranchAt:     func(message string) (string, string) { return branchLine(live, message) },
		Undo:         func(count string) string { return undoTurns(live.ID(), count) },
		Updates:      appUpdates(dir),
		Shells:       appShells(dir, launch.registry, launch.registryErr),
		KillShell:    appKillShell(launch.registry, launch.registryErr),
		RunCommand: func(ctx context.Context, command string) (string, bool) {
			printed, stopped := shellCommand(ctx, dir, launch.registry, command)
			return live.Ran(command, printed, stopped), stopped
		},
		Fresh:   launch.fresh,
		Resumed: opening,
		Keymap:  shortcuts,
		Agents:  func() roster.Found { found, _ := discoverAgents(); return found },
		FirstFrame: func() {
			name := "resume"
			if launch.fresh {
				name = "fresh"
			}
			if places, err := platformproject.ResolvePlaces(dir); err == nil {
				_ = walls.Append(places.State, walls.Wall{Op: walls.OpFirstFrame, Name: name, MS: time.Since(launch.started).Milliseconds(),
					At: launch.started.UnixMilli(), Session: live.ID(), OK: true, OutsideMS: new(int64)})
			}
		},
	}
}

func modelsUpdate(dir string, update func(context.Context, io.Writer) int) func() string {
	if update == nil {
		return nil
	}
	return func() string {
		before, unreadBefore := wiring.ModelLibrary(dir)
		wiring.StampUpdate(update(context.Background(), io.Discard))
		debug.FreeOSMemory()
		after, err := wiring.ModelLibrary(dir)
		if err != nil {
			return catalog.UpdateVerb + ": " + err.Error()
		}
		var found []string
		for _, model := range after.Models {
			known := slices.ContainsFunc(before.Models, func(was catalog.Model) bool { return was.Slug() == model.Slug() })
			if !known && model.Use != catalog.UseExcluded {
				found = append(found, model.Slug())
			}
		}
		if len(found) == 0 || unreadBefore != nil {
			return ""
		}
		return wiring.UpdateFound + strings.Join(found, ", ")
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
		return output.ExitUsage
	}
	collectForALongLife()
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu: the working directory is unreadable: %v\n", err)
		return output.ExitVerdict
	}
	stopRestoring := restoring(out, resumed)
	held := wiring.HoldStore()
	defer held.Close()
	update := func(ctx context.Context, out io.Writer) int { return modelsverb.Update(ctx, held, out) }
	live := appWiring{open: wiring.OpenWire, wires: held.Wires, blockers: held.Requirements, quota: held.Quota, update: update, joins: true}
	launch := launchOf(dir, resumed, resumed.Session == "")
	launch.stopRestoring = stopRestoring
	var identity sync.WaitGroup
	var identityErr error
	identity.Go(func() {
		identityErr = platformproject.Crashed(dir, safe.Call(nil, "project identity", func() error { return platformproject.RecordIdentity(dir) }))
	})
	err = app.Run(appOptions(dir, wiring.RunOpts{}, live, launch, held))
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
		return output.ExitVerdict
	}
	_, _ = fmt.Fprintln(out, sessionEndLine(launch.registry, launch.registryErr))
	leaveShells(launch.registry)
	return output.ExitOK
}

func leaveShells(registry *process.Registry) {
	if registry == nil || registry.Lifetime == process.OutlivesTofu {
		return
	}
	for _, one := range registry.Own() {
		_ = registry.Kill(one.Name)
	}
}

func sessionEndLine(registry *process.Registry, openErr error) string {
	var running []string
	if openErr == nil {
		for _, one := range registry.Own() {
			running = append(running, one.Name+" ("+one.Command+")")
		}
	}
	if len(running) == 0 {
		return sessionEnded
	}
	if registry.Lifetime == process.OutlivesTofu {
		return sessionEnded + leavingPrefix + strings.Join(running, ", ")
	}
	return sessionEnded + stoppingPrefix + strings.Join(running, ", ")
}

func appPaths(dir string) func() []string {
	return func() []string {
		listed, err := glob.ListPaths(dir)
		if err != nil {
			return nil
		}
		return listed
	}
}

func branchOf(dir string) string {
	head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
}

func appShells(dir string, registry *process.Registry, openErr error) func() []shells.Entry {
	var tails process.Tails
	return func() []shells.Entry {
		if openErr != nil {
			return nil
		}
		_ = registry.PruneEvery(shellPruneEvery)
		found, err := registry.List()
		if err != nil {
			return nil
		}
		tail, err := strconv.Atoi(settingspkg.TextSaying(dir, settingspkg.LogTail, nil))
		if err != nil {
			tail = konst.ShellLogTailLinesDefault
		}
		entries := make([]shells.Entry, 0, len(found))
		for _, one := range found {
			entry := shells.Entry{Name: one.Name, Command: one.Command, Started: one.Started, Ended: one.Ended, ExitCode: one.ExitCode, PID: one.PID, Dir: one.Dir, Owner: wiring.OwnerName(one.Owner), OneShot: one.OneShot()}
			switch one.State {
			case process.Running:
				entry.State = shells.Running
				if one.LeftOver() {
					entry.State = shells.LeftOver
				}
			case process.Exited:
				entry.State = shells.Exited
			case process.Killed:
				entry.State = shells.Killed
			}
			entry.Log, _ = tails.Tail(registry, one.Name, tail)
			entries = append(entries, entry)
		}
		tails.Forget(found)
		return entries
	}
}

func appKillShell(registry *process.Registry, openErr error) func(string) error {
	return func(name string) error {
		if openErr != nil {
			return openErr
		}
		return registry.Kill(name)
	}
}

func shellCommand(ctx context.Context, dir string, registry *process.Registry, command string) (string, bool) {
	chosen, err := loop.ResolveRunShell(ctx, settingspkg.TextSaying(dir, settingspkg.Shell, nil))
	if err != nil {
		return err.Error(), false
	}
	bash, err := loop.NewBashToolFromShell(dir, chosen)
	if err != nil {
		return err.Error(), false
	}
	args, _ := json.Marshal(map[string]string{"command": command})
	result, err := bash.Run(loop.WithShellRegistry(ctx, registry), args)
	if err != nil {
		return err.Error(), false
	}
	return result.Content, result.Outcome == contract.ResultAborted
}

func freshSession(live *host.Host) string {
	if err := live.Fresh(); err != nil {
		return err.Error()
	}
	return freshSessionNote
}

func appResume(live *host.Host, handle string) (string, host.Opened) {
	store, err := sessionstore.Open()
	if err != nil {
		return "the session store does not open, so nothing is carried: " + err.Error(), host.Opened{}
	}
	carry, err := resumeOf(store, handle)
	if err != nil {
		return err.Error(), host.Opened{}
	}
	opened, err := live.Resume(carry.hosted())
	if err != nil {
		return err.Error(), host.Opened{}
	}
	return "continuing " + carry.Session + ", " + strconv.Itoa(carry.Carried) + " messages from " + output.Plural(carry.Steps, "step"), opened
}

func appSessions(inUse string) ([]app.SessionRow, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	report, err := sessionListing(store)
	if err != nil {
		return nil, err
	}
	open, _ := store.Identity(inUse)
	rows := make([]app.SessionRow, len(report.Sessions))
	for i, row := range report.Sessions {
		rows[i] = app.SessionRow{ID: row.ID, Name: row.Handle, Task: output.OneLine(row.Task), InUse: row.ID == inUse || (open.Family != "" && row.Family == open.Family),
			Facts: sessionWhen(row.At, now) + " · " + output.Plural(row.Turns, "turn") + " · " + output.Plural(row.Generations, "generation")}
	}
	return rows, nil
}

func imagesRefused(setting string, model catalog.Model, attached int) string {
	held := strconv.Itoa(attached) + " attached image(s) stayed out of this turn"
	switch setting {
	case settingspkg.ImagesOff:
		return "images is off, so " + held
	case settingspkg.ImagesAuto:
		if model.Vision == catalog.VisionBlind {
			return "images is auto and " + model.Slug() + " cannot see images, so " + held + "; pick a model that can, or set images to inline"
		}
		return ""
	case settingspkg.ImagesInline:
		return ""
	}
	panic("tofu: unknown images setting " + setting)
}

func pickedOpts(dir string, start host.Turn, maxSteps int) wiring.RunOpts {
	return wiring.OnTheBoundWire(wiring.RunOpts{
		Dir:              dir,
		Task:             start.Task,
		TurnID:           start.ID,
		Session:          start.Session,
		Wire:             start.Pick.Wire,
		Model:            start.Pick.Model,
		ToolSet:          toolSetFull,
		Effort:           cmp.Or(start.Pick.Effort, llm.EffortDefault),
		LoopGuardRepeats: konst.TurnLoopGuardRepeats,
		LoopGuardWindow:  konst.TurnLoopGuardWindow,
		MaxSteps:         maxSteps,
	})
}

type missingJudge int

const (
	missingJudgeRunsWithout missingJudge = iota
	missingJudgeRefuses
)

type appEngine struct {
	dir          string
	arms         wiring.RunOpts
	open         wiring.Opener
	quota        func() []frame.Quota
	tabs         *browser.BrowserTabs
	warm         *warmProcesses
	places       platformproject.Places
	timed        string
	missingJudge missingJudge
}

type startedTurn struct {
	host.Prepared
	gate   *toolGate
	scorer *wiring.ShellScorer
	budget recall.Budget
}

func settled(opts wiring.RunOpts, say func(string)) wiring.RunOpts {
	dir := cmp.Or(opts.Dir, ".")
	if opts.MaxSteps == 0 {
		opts.MaxSteps = settingspkg.IntSaying(dir, settingspkg.DecisionCap, say)
	}
	opts.NoSubAgents = opts.NoSubAgents || settingspkg.IntSaying(dir, settingspkg.TurnMaySpawn, say) == 0
	opts.ReadBeforeEdit = settingspkg.IntSaying(dir, settingspkg.ReadBeforeEdit, say) != 0
	opts.PlanEveryStep = settingspkg.IntSaying(dir, settingspkg.PlanEveryStep, say) != 0
	if opts.DoneArm == "" && spawnlessFlag(opts) == "" {
		opts.DoneArm = settingspkg.TextSaying(dir, settingspkg.DoneReview, say)
	}
	opts.DoneArm = cmp.Or(opts.DoneArm, settingspkg.DoneReviewOff)
	return opts
}

func (e *appEngine) Renew() {
	e.warm.Close()
	e.warm = newWarmProcesses(e.tabs)
	e.warm.checkers.Warm(e.dir)
}

func (e *appEngine) OneTurnPerProject() bool {
	return settingspkg.IntSaying(e.dir, settingspkg.OneWorkingSessionPerProject, nil) != 0
}

func (e *appEngine) Prepare(start host.Turn, hooks host.Hooks) (host.Prepared, error) {
	clock := clockFromNow()
	opts := pickedOpts(e.dir, start, e.arms.MaxSteps)
	opts.GateArm, opts.SiftArm, opts.DoneArm, opts.NoInstructions, opts.NoDocs = e.arms.GateArm, e.arms.SiftArm, e.arms.DoneArm, e.arms.NoInstructions, e.arms.NoDocs
	opts.ToolSet, opts.ContextCeiling, opts.NoSubAgents = cmp.Or(e.arms.ToolSet, opts.ToolSet), e.arms.ContextCeiling, e.arms.NoSubAgents
	started, err := e.startTurn(settled(opts, hooks.Say), start, hooks, clock)
	if started.scorer != nil {
		closing := started.Close
		started.Close = func() {
			closing()
			if calls, cost := started.scorer.Spend(); calls > 0 {
				hooks.Say(fmt.Sprintf("sift decisions %d cost $%.6f", calls, cost))
			}
		}
	}
	return started.Prepared, err
}

func (e *appEngine) startTurn(opts wiring.RunOpts, start host.Turn, hooks host.Hooks, clock *startupClock) (startedTurn, error) {
	say, dir := hooks.Say, cmp.Or(opts.Dir, ".")
	stacked := readRuleStack(wiring.CrashTold(dir, say), dir)
	started := startedTurn{Prepared: host.Prepared{MaxSteps: opts.MaxSteps}}
	prepared := &started.Prepared
	probing, probe := outside.Open(context.Background(), time.Now)
	shell, err := loop.ResolveRunShell(probing, settingspkg.TextSaying(dir, settingspkg.Shell, say))
	if err != nil {
		return started, err
	}
	opts.Shell = shell
	clock.step("settings", probe.Outside())
	opened, err := e.open(opts)
	if opened.Held != nil {
		prepared.Close = opened.Held.Close
	}
	if err != nil {
		return started, err
	}
	checked := clock.checkUsageAlongside(opened.Held)
	prepared.Close = func() {
		checked()
		opened.Held.Close()
	}
	clock.step("model choice and credentials", 0)
	images := start.Images
	if len(images) > 0 {
		if refused := imagesRefused(settingspkg.TextSaying(dir, settingspkg.Images, say), opened.Selected, len(images)); refused != "" {
			say(refused)
			images = nil
		}
	}
	built, plan, builtErr := buildRunToolsForRun(opts.Dir, opts.ToolSet, readsWhen(opts.ReadBeforeEdit, hooks.Reads), e.warm, shell, stacked)
	unstarted, builtErr := unstartedIn(builtErr)
	sayUnstarted(unstarted, say)
	sessions, sessionsErr := sessionstore.OpenIn(dir)
	if err := cmp.Or(builtErr, sessionsErr); err != nil {
		return started, err
	}
	sessions.Redacts(e.warm.keys.redactor)
	built, plan = planAsOffered(opened.Selected, built, plan)
	if start.Pick.Fired != "" {
		built = shellsverb.WithoutPush(built)
	}
	clock.step("shell, tools and session store", 0)
	places := e.places
	if places == (platformproject.Places{}) {
		if places, err = platformproject.ResolvePlaces("."); err != nil {
			return started, err
		}
	}
	var gate *toolGate
	var gateErr error
	if opts.GateArm != gateOff {
		gate, gateErr = toolGateOn(opts.Dir, places.Log, e.warm.keys.keyring())
	}
	var unusable unusableRule
	switch {
	case gateErr == nil:
	case e.missingJudge == missingJudgeRefuses:
		return started, gateErr
	case errors.As(gateErr, &unusable):
		return started, fmt.Errorf("no turn starts while the tool gate rule is unusable: %w", unusable)
	case core.IsUnbound(gateErr):
		sayEach, unbound := say, gateErr.Error()
		say = func(note string) {
			if !strings.HasSuffix(note, unbound) {
				sayEach(note)
			}
		}
	}
	sifter, scorer, siftErr := wiring.ShellSiftOn(opts.SiftArm, places.Log, e.warm.keys.keyring())
	var contradiction loop.RuleContradictsTheTableError
	if siftErr != nil && e.missingJudge == missingJudgeRefuses && !errors.As(siftErr, &contradiction) {
		return started, siftErr
	}
	if siftErr != nil {
		say("no shell result is cut: " + siftErr.Error())
	}
	clock.step("gate and sift", 0)
	budget, err := wiring.ContextBudget(opts, opened.Selected)
	if err != nil {
		return started, err
	}
	clock.step("model catalogue", 0)
	opened.Held.Wrapping(func(model loop.Model, budget recall.Budget) (loop.Model, error) {
		asked, guardErr := wiring.Guarded(model, budget)
		if guardErr != nil {
			return nil, guardErr
		}
		return hooks.Lead(clock.leading(asked)), nil
	}, budget, opts)
	wrapSubAgent := func(model loop.Model, budget recall.Budget) (loop.Model, error) {
		asked, guardErr := wiring.Guarded(model, budget)
		return hooks.SubAgent(asked), guardErr
	}
	person := hooks.Inbox.LeadAsks(hooks.Person)
	turnAccounts, account := opened.Held.ForTurn(), spentAccount(opened.Held, opened.Selected.ID)
	if stand := e.standIns(opened.Held, opts.Wire); len(stand.Mine()) > 0 {
		turnAccounts, account = stand.ForTurn(), stand.Spent
	}
	config, spawner, err := runConfig(opts, built, runtime{accounts: clock.picking(turnAccounts, opened.Held.Store), spend: opened.Spend, budget: budget, gate: gate, sift: sifter, scorer: scorer, sessions: sessions, notify: say, roster: hooks.Roster, inbox: hooks.Inbox, now: hooks.Now,
		open: e.open, wrapSubAgent: wrapSubAgent, orchestrator: opened.Selected, tabs: e.tabs, leadAsks: person, cron: hooks.Cron, plan: plan, places: places, startup: clock, keys: &e.warm.keys, memory: &e.warm.memory, drops: e.warm.drops, ruleStack: stacked})
	if err != nil {
		return started, err
	}
	if spawner != nil {
		spawner.Review = newDoneReview(opts.DoneArm, say)
	}
	clock.step("lead tools, board and spawn", 0)
	turn := laterTurn
	if config.Session != e.timed {
		e.timed, turn = config.Session, firstTurn
	}
	closeAccounts := prepared.Close
	prepared.Close = func() {
		closeAccounts()
		if err := clock.record(sessions, config.Session, turn); err != nil {
			say("the startup steps were not recorded: " + err.Error())
		}
	}
	if gate != nil {
		gate.watch = hooks.Gate
		prepared.Decisions = func() int { return gate.decisions }
	}
	config.Images = images
	config.Person = person.Asking(config.GatePromptAsks)
	prepared.Config, prepared.Spawner, prepared.Plan, prepared.Ceiling, prepared.Sessions, prepared.GateOff, prepared.Account = config, spawner, plan, budget.CeilingTokens, sessions, gateErr, account
	started.gate, started.scorer, started.budget = gate, scorer, budget
	return started, nil
}

func (e *appEngine) standIns(held *wiring.Accounts, wire string) wiring.StandIns {
	if _, recorded := held.Fixed.(*cassette); !recorded || e.quota == nil {
		return wiring.StandIns{}
	}
	library, _ := wiring.ModelLibrary(e.dir)
	spec, _ := library.ForWire(wire)
	return wiring.StandIns{Held: held, Source: string(spec.ID), Readings: e.quota()}
}

func spentAccount(held *wiring.Accounts, model string) func(int64) host.TurnAccount {
	return func(id int64) host.TurnAccount {
		spent := host.TurnAccount{Source: string(held.Provider), AccountID: id, Model: model}
		if held.Store == nil {
			return spent
		}
		rows, err := held.Store.List()
		if at := slices.IndexFunc(rows, func(row accounts.Row) bool { return row.ID == id }); err == nil && at >= 0 {
			spent.Login = wiring.AccountName(rows[at].Credential.Identity, false)
		}
		return spent
	}
}
