package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	boardyverb "tofu/apps/cli/verbs/boardy"
	browserverb "tofu/apps/cli/verbs/browser"
	doctorverb "tofu/apps/cli/verbs/doctor"
	guardverb "tofu/apps/cli/verbs/guard"
	hooksverb "tofu/apps/cli/verbs/hooks"
	judgeverb "tofu/apps/cli/verbs/judge"
	learnverb "tofu/apps/cli/verbs/learn"
	libraryverb "tofu/apps/cli/verbs/library"
	lintverb "tofu/apps/cli/verbs/lint"
	loginverb "tofu/apps/cli/verbs/login"
	memoryverb "tofu/apps/cli/verbs/memory"
	modelsverb "tofu/apps/cli/verbs/models"
	rulesverb "tofu/apps/cli/verbs/rules"
	settingsverb "tofu/apps/cli/verbs/settings"
	shellsverb "tofu/apps/cli/verbs/shells"
	statusverb "tofu/apps/cli/verbs/status"
	wallsverb "tofu/apps/cli/verbs/walls"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/subagents"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/platform/console"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/profile"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/platform/text"
	"tofu/internal/store/walls"
	"tofu/library/changelog"
)

const (
	profileFlag      = "--profile"
	profiledVariable = "TOFU_PROFILED"
)

const usage = `tofu is a coding agent harness.

Usage:
  tofu                    start the app in this directory
  tofu --continue         start the app on the session you last worked in
  tofu --temporary-project
                          start the app in a project tofu forgets %d hours after
                          its last use: its sessions, undo history and logs;
                          tofu run takes the same flag
  tofu --profile          start the app writing CPU and heap profiles and a goroutine
                          dump into the project's profile folder, every few minutes
                          and at exit; works with --continue and tofu run
  tofu <verb> [arguments]

Verbs:
  version   print the version, the commit and the Go version
  changelog print what changed since the version you last read
  update    install the latest release over this tofu,
            or --check to only say whether one is newer
  docs      print what you can ask tofu and the command that does it,
            a page with docs <topic>, or the closest answers with docs "a few words"
  doctor    say whether tofu can run here, and what is wrong if it cannot
  login     sign in or store a key by role: login llm claude-sub|codex-sub|meta|openrouter,
            login classifier typesafe, login search brave
  logout    remove what login stored: logout <role> <provider> [number]
  usage     print every credential's quota windows and when each resets,
            or --history for the readings already recorded, each with its moment
  models    list the models each subscription serves, and why one is excluded
  agents    list the sub-agents read from .tofu, .agents, .claude and the library,
            each with where it came from and the model it runs,
            or add, set or remove one
  why       explain a ledger row, or the last one
  run       work a task in a directory until it is done
  tool      list the tools a run offers, or run one tool call by hand
            and print the result the model would receive
  judge     read a state and a question battery, print the answers
  check     run the tool gate on a shell command, in shadow, and log it
  session   list the recorded sessions, read one, rename one, or continue one
  projects  list every project tofu knows, newest opened first, with its sessions
  shells    list the persistent processes an agent left running, read one's log, kill one
  hooks     list the Claude Code, codex and tofu hooks a turn runs, with where each
            came from, its trust and its last result, or trust this project's
  guard     run one of this repository's Claude Code or git hook rules on any OS:
            guard <rule> reads the hook JSON on stdin, guard git commit-msg <file>
  undo      put back the files the last turn changed, or the last N turns,
            and refuse by name a file changed after the turn
  context   print the context bands of a recorded session and what fills them
  sift      mark what is worth reading in a message and elide the rest
  label     attach an outcome to a ledger row, by id or the last one
  replay    re-score ledger rows against changed thresholds, no network
  calibrate fit a decision point's cut on labelled cases and write its lock
  library   resolve a library entry and show the origin of each field
  lint      run a house-rule check over the tree
  rules     list the rule library, run it, read back what fired, or index a task
  boardy    keep this project's tickets as files: boards, tickets, managers,
            triage, views, reports, epics and sprints; tofu boardy lists each
  scratch   list this project's scratch folders and their sizes, clean the ones
            no tofu runs, or print the scratch root with open
  memory    list what you asked tofu to remember, global and for this project,
            or add or remove an entry
  learn     read your sessions for the correction you keep repeating,
            and propose at most five changes with your words behind each
  frame     print one recorded interface frame, at a width and a height
  drive     run a script of what a person would do against the real app,
            with no terminal and no model call, and print the screens it asks for;
            drive desk opens the desk app in a fresh home and keeps screenshots
  host      start, read or stop this project's host, the one process every
            tofu and desk of the project joins: ensure, status, stop
  serve     serve this project to a frontend: --stdio speaks tofu.host/1,
            JSON-RPC on standard input and output, and --schema prints its schema
  settings  list, get or set a declared setting, global or project
  reload    re-read settings, rules, skills, sub-agents, models, roles, tiers,
            instruction files and keys, and print what changed since the last reload
  migrate   move what tofu wrote out of this project's .tofu and into ~/.tofu,
            or --dry-run to list what would move, or --relink to give a moved
            repository back its sessions
  browser   list the Chrome tabs tofu can reach, install or uninstall the extension,
            or open and close a background tab tofu owns
  walls     print how long each kind of operation took, in ms: n, p50, p90 and max
            over the last %d days, or --since, --op and --all

Every verb that reports state takes --json and prints one JSON document.
`

func loadLocalTimeZone() { _ = time.Local.String() }

func main() {
	started, known := processStarted()
	if !known {
		started = time.Now()
	}
	stopProfile := profile.StartProfile()
	reapTemporaryProjects(os.Stderr)
	args, temporary := temporaryProject(os.Args[1:])
	args, profiled := withoutProjectFlag(args, profileFlag)
	if profiled {
		_ = os.Setenv(profiledVariable, profileFlag)
	}
	profiled = profiled || hostRuns(args) && os.Getenv(profiledVariable) != ""
	opensTheApp := len(args) == 0 || args[0] == "--continue"
	if opensTheApp || args[0] == "migrate" {
		copyLegacyStateDirs(os.Stderr)
	}
	dir, opens := openedProject(args)
	register := platformproject.Project.Register
	if opensTheApp {
		register = platformproject.Project.RegisterIdentityLater
	}
	letGo := func() {}
	if opens {
		safe.Go(wiring.CrashTold(".", func(said string) { _, _ = fmt.Fprintln(os.Stderr, "tofu: "+said) }), "time zone load", loadLocalTimeZone)
		openProject(os.Stderr, os.Stdin, dir, opensTheApp && isTerminal(os.Stdin) && isTerminal(os.Stderr), temporary, register)
		letGo = platformproject.HoldProjectOpen(dir)
	}
	stopSnapshots := func() {}
	if profiled {
		stopSnapshots = startSnapshots(os.Stderr, dir)
	}
	if wd, err := os.Getwd(); err == nil && (opensTheApp || opens) {
		moveProjectState(os.Stderr, wd)
	}
	moveHomeKeys(os.Stderr)
	pages := console.ReadConsolePages()
	ctx, clock := outside.Open(context.Background(), time.Now)
	code := runIn(ctx, args, os.Stdin, os.Stdout, os.Stderr)
	if name, timed := verbWallName(args); timed {
		if verbNamed(args[0]).clocked == nil {
			clock = nil
		}
		appendVerbWall(dir, name, started, code, clock)
	}
	pages.Restore()
	letGo()
	stopSnapshots()
	stopProfile()
	os.Exit(code)
}

func verbWallName(args []string) (string, bool) {
	switch {
	case len(args) == 0 || !verbNamed(args[0]).known() || args[0] == "--continue" || args[0] == "serve" || hostRuns(args):
		return "", false
	case len(args) > 1 && !strings.HasPrefix(args[1], "-") && (args[0] == "tool" || slices.Contains(usageWords(verbNamed(args[0]).usage), args[1])):
		return args[0] + " " + args[1], true
	}
	return args[0], true
}

func appendVerbWall(dir, name string, started time.Time, code int, measured *outside.Clock) {
	wall := walls.Wall{Op: walls.OpVerb, Name: name, At: started.UnixMilli(), Session: os.Getenv(subagents.SessionEnv), OK: code == output.ExitOK}
	if measured != nil {
		outsideMS := measured.Outside().Milliseconds()
		wall.OutsideMS = &outsideMS
	}
	wall.MS = time.Since(started).Milliseconds()
	if places, err := platformproject.ResolvePlaces(cmp.Or(dir, ".")); err == nil {
		_ = walls.Append(places.State, wall)
	}
}

func withoutProjectFlag(args []string, flag string) ([]string, bool) {
	rest := slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == flag })
	if _, opens := openedProject(rest); !opens || len(rest) == len(args) {
		return args, false
	}
	return rest, true
}

func startSnapshots(errOut io.Writer, project string) func() {
	page := cli.Detect(errOut, os.Environ())
	state, err := platformproject.ProjectStateDirAt(project)
	var dir string
	var stop func()
	if err == nil {
		dir, stop, err = profile.StartSnapshots(state)
	}
	if err != nil {
		_ = page.Print(errOut, page.ErrorLine("tofu: profiling not started: "+err.Error(), ""))
		return func() {}
	}
	_ = page.Print(errOut, []string{page.Receipt(cli.Changed, "profiling: CPU, heap and goroutines every "+profile.SnapshotEvery.String()+" and at exit, the last "+strconv.Itoa(profile.SnapshotsKept)+" kept", dir)})
	return func() {
		stop()
		_ = page.Print(errOut, []string{page.Receipt(cli.Changed, "profile written", dir)})
	}
}

func moveHomeKeys(errOut io.Writer) {
	migration, err := credential.MigrateHomeKeys()
	page := cli.Detect(errOut, os.Environ())
	if err != nil {
		_ = page.Print(errOut, page.ErrorLine(page.Path(migration.From)+" not moved into the credential store: "+err.Error(), ""))
		return
	}
	if len(migration.Moved) == 0 {
		return
	}
	fate := "file removed"
	if !migration.Removed {
		fate = "other lines kept"
	}
	_ = page.Print(errOut, []string{page.Receipt(cli.Changed, strings.Join(migration.Moved, ", ")+" moved into the credential store · "+fate, migration.From)})
}

func run(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	return runIn(context.Background(), args, in, out, errOut)
}

func runIn(ctx context.Context, args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	code := dispatch(ctx, args, in, out, errOut)
	if code != output.ExitUsage || len(args) == 0 || slices.Contains(args, output.JSONFlag) {
		return code
	}
	if guess, found := nearestCommand(args); found {
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, cli.Indent(page.Hint("did you mean "+guess)))
	}
	return code
}

func nearestCommand(args []string) (string, bool) {
	if listed := usageVerbs(); !slices.Contains(listed, args[0]) {
		near, found := nearestWord(args[0], listed)
		return "tofu " + near, found
	}
	known := usageWords(verbNamed(args[0]).usage)
	if args[0] == "boardy" && len(args) > 1 {
		known = append(known, usageWords(boardyverb.UsageOf(args[1]))...)
	}
	if len(known) == 0 {
		return "", false
	}
	for at, arg := range args[1:] {
		if slices.Contains(known, arg) || !slices.Equal(usageWords(arg), []string{arg}) {
			continue
		}
		guess := slices.Clone(args)
		if arg == "list" {
			return "tofu " + strings.Join(slices.Delete(guess, at+1, at+2), " "), true
		}
		if near, found := nearestWord(arg, known); found {
			guess[at+1] = near
			return "tofu " + strings.Join(guess, " "), true
		}
		if strings.HasPrefix(arg, "-") {
			return "", false
		}
	}
	return "", false
}

func nearestWord(word string, candidates []string) (string, bool) {
	best, distance := "", len(word)/konst.NearestWordBytesPerEdit+1
	for _, candidate := range candidates {
		if d := text.EditDistance(word, candidate); d < distance {
			best, distance = candidate, d
		}
	}
	return best, best != ""
}

func usageWords(line string) []string {
	var plain strings.Builder
	for rest := line; rest != ""; {
		before, after, _ := strings.Cut(rest, "<")
		plain.WriteString(before + " ")
		_, rest, _ = strings.Cut(after, ">")
	}
	return strings.FieldsFunc(plain.String(), func(r rune) bool { return !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '-' })
}

type verbRun = func(args []string, in io.Reader, out, errOut io.Writer) int

type clockedRun = func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int

type verb struct {
	run     verbRun
	clocked clockedRun
	usage   string
}

func (v verb) known() bool { return v.run != nil || v.clocked != nil }

func withoutIn(run func(args []string, out, errOut io.Writer) int) verbRun {
	return func(args []string, _ io.Reader, out, errOut io.Writer) int { return run(args, out, errOut) }
}

func clockedWithoutIn(run func(ctx context.Context, args []string, out, errOut io.Writer) int) clockedRun {
	return func(ctx context.Context, args []string, _ io.Reader, out, errOut io.Writer) int {
		return run(ctx, args, out, errOut)
	}
}

func withClock(run func(args []string, out, errOut io.Writer, now func() time.Time) int) verbRun {
	return func(args []string, _ io.Reader, out, errOut io.Writer) int { return run(args, out, errOut, time.Now) }
}

func whyVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	return statusverb.WhyVerb(args, out, errOut, now)
}

func loginVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	return verbNamed("login").clocked(context.Background(), args, in, out, errOut)
}

func verbNamed(name string) verb {
	switch name {
	case "--continue":
		return verb{run: continueVerb}
	case "session":
		return verb{run: sessionVerb, usage: strings.Join([]string{sessionSubcommands, sessionBranchUsage, sessionDrainUsage, sessionFindUsage, sessionTraceUsage, sessionTranscriptUsage}, " ")}
	case "projects":
		return verb{run: withoutIn(projectsVerb), usage: projectsUsage}
	case "shells":
		return verb{run: withoutIn(shellsverb.Verb)}
	case "hooks":
		return verb{run: withoutIn(hooksverb.Verb)}
	case "guard":
		return verb{run: guardverb.Verb, usage: guardverb.Usage}
	case "undo":
		return verb{clocked: clockedWithoutIn(undoVerb), usage: undoUsage}
	case "version":
		return verb{run: withoutIn(version)}
	case "update":
		return verb{clocked: clockedWithoutIn(updateVerb), usage: updateUsage}
	case "changelog":
		return verb{run: func(args []string, _ io.Reader, out, errOut io.Writer) int {
			return changelogVerb(args, changelog.Markdown, out, errOut)
		}, usage: changelogUsage}
	case "docs":
		return verb{run: withoutIn(libraryverb.DocsVerb), usage: libraryverb.DocsUsage}
	case "doctor":
		return verb{clocked: doctorverb.Clocked, usage: doctorverb.Usage}
	case "login":
		return verb{clocked: loginverb.Verb(loginverb.Deps{Status: statusverb.LoginVerb}), usage: loginverb.Usage + " " + loginverb.AsideUsage}
	case "logout":
		return verb{run: withoutIn(loginverb.LogoutVerb), usage: loginverb.LogoutUsage}
	case "usage":
		return verb{clocked: loginverb.UsageVerb, usage: loginverb.UsageFlags}
	case "models":
		return verb{clocked: modelsverb.Clocked, usage: modelsverb.Usage}
	case "agents":
		return verb{run: withoutIn(agentsVerb), usage: strings.Join([]string{agentsUsage, agentsAddUsage, agentsSetUsage, agentsRemoveUsage}, " ")}
	case "why":
		return verb{run: withClock(whyVerb), usage: statusverb.WhyUsage}
	case "frame":
		return verb{run: withoutIn(frameVerb), usage: frameUsage}
	case "drive":
		return verb{clocked: func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
			if len(args) > 0 && args[0] == "desk" {
				return driveDeskVerb(args[1:], out, errOut)
			}
			return driveIn(ctx, args, in, out, errOut)
		}, usage: driveUsage + "\n" + driveDeskUsage}
	case "serve":
		return verb{run: serveVerb, usage: serveUsage}
	case host.HubVerb:
		return verb{run: withoutIn(projectHostVerb), usage: projectHostUsage}
	case "run":
		return verb{clocked: clockedWithoutIn(runVerb)}
	case "tool":
		return verb{clocked: clockedWithoutIn(toolVerb), usage: toolUsage}
	case "judge":
		return verb{clocked: judgeverb.Verb, usage: judgeverb.Usage}
	case "check":
		return verb{run: withoutIn(judgeverb.CheckVerb), usage: judgeverb.CheckUsage}
	case "context":
		return verb{run: withoutIn(contextVerb)}
	case "sift":
		return verb{clocked: judgeverb.SiftVerb}
	case "label":
		return verb{run: withClock(judgeverb.LabelVerb), usage: judgeverb.LabelUsage}
	case "calibrate":
		return verb{run: withoutIn(judgeverb.CalibrateVerb), usage: judgeverb.CalibrateUsage}
	case "replay":
		return verb{run: withClock(judgeverb.ReplayVerb), usage: judgeverb.ReplayUsage}
	case "library":
		return verb{run: withoutIn(libraryverb.Verb(usageVerbs())), usage: libraryverb.Usage}
	case "catalog":
		return verb{run: func(args []string, _ io.Reader, out, errOut io.Writer) int {
			_, _ = fmt.Fprintln(errOut, "tofu catalog is now tofu library")
			return libraryverb.Verb(usageVerbs())(args, out, errOut)
		}}
	case "lint":
		return verb{run: withoutIn(lintverb.Verb), usage: lintverb.Usage}
	case "rules":
		return verb{run: withoutIn(rulesverb.Verb(rulesverb.Deps{Agents: agentsIn, Why: rulesWhyVerb})), usage: rulesverb.Usage + " " + rulesWhyUsage}
	case "boardy":
		return verb{run: withoutIn(boardyverb.Verb), usage: boardyverb.Usage}
	case "scratch":
		return verb{run: withoutIn(shellsverb.Scratch), usage: shellsverb.ScratchUsage}
	case "memory":
		return verb{run: withoutIn(memoryverb.Verb(wireOpener())), usage: memoryverb.Usage + " " + memoryverb.TreeUsage}
	case "learn":
		return verb{run: withoutIn(learnverb.Verb(learnverb.Deps{SetSetting: settingsverb.Verb, AddRule: rulesverb.AddVerb})), usage: learnverb.Usage}
	case "settings":
		return verb{run: withoutIn(settingsverb.Verb), usage: settingsverb.Usage}
	case "reload":
		return verb{run: withoutIn(reloadVerb), usage: reloadUsage}
	case "migrate":
		return verb{run: withoutIn(migrateVerb), usage: migrateUsage}
	case "browser":
		return verb{clocked: clockedWithoutIn(browserverb.Verb)}
	case "walls":
		return verb{run: withoutIn(wallsverb.Verb), usage: wallsverb.Usage}
	case "preview":
		return verb{run: withoutIn(previewVerb)}
	case "help", "-h", "--help":
		return verb{run: func(_ []string, _ io.Reader, out, _ io.Writer) int {
			_, _ = io.WriteString(out, verbHelp("help", nil))
			return output.ExitOK
		}}
	}
	return verb{}
}

func verbHelp(name string, args []string) string {
	text := verbNamed(name).usage
	switch name {
	case "help", "-h", "--help", "--continue":
		return fmt.Sprintf(usage, konst.TemporaryProjectIdleHours, konst.WallsWindowDays)
	case "run":
		return runUsage()
	case "sift":
		return judgeverb.SiftUsage
	case "catalog":
		return verbHelp("library", args)
	case "boardy":
		text = cmp.Or(boardyverb.UsageOf(subcommandOf(args)), text)
	case "shells":
		text = shellsverb.Subcommands
	case "hooks":
		text = hooksverb.Subcommands
	case "version":
		text = "tofu version [--json]"
	case "drive":
		if len(args) > 0 && args[0] == "desk" {
			text = driveDeskUsage
		}
	case "context":
		text = "tofu context [<name|id|head>] [--json]"
	case "preview":
		text = "tofu preview [--json]"
	case "browser":
		text = "tofu browser [install | uninstall | tabs | build | recipes | open <url> | close <tab id> | bench [--jev] [--n 12] [--rows file] | motion capture <scenario.json>] [--json], " +
			"tofu browser observe [--all] | click <ref> | fill <ref> <text> | select <ref> <option> | press <key> | scroll [<ref>] [up|down] | back --tab <id> [--json], " +
			"tofu browser read [--tab <id>] [--json], tofu browser batch [<steps.json> | -] [--tab <id>] [--json], " + browserverb.ProfileUsage
	}
	if strings.Contains(text, "\n") {
		return text
	}
	prefix, sub := "tofu "+name+" ", subcommandOf(args)
	var all, picked []string
	for _, part := range strings.Split(strings.TrimPrefix(text, "usage: "), prefix)[1:] {
		part = strings.TrimRight(strings.TrimSuffix(strings.TrimSpace(part), " or"), ", |")
		form := "usage: " + prefix + part
		all = append(all, form)
		switch first, _, _ := strings.Cut(part, " "); {
		case sub != "" && first == sub:
			picked = slices.Insert(picked, 0, form)
		case sub != "" && slices.Contains(strings.Split(first, "|"), sub):
			picked = append(picked, form)
		}
	}
	if len(picked) == 0 {
		picked = all
	}
	return strings.Join(picked, "\n") + "\n"
}

func usageVerbs() []string {
	_, listing, _ := strings.Cut(usage, "\nVerbs:\n")
	var verbs []string
	for _, line := range strings.Split(listing, "\n") {
		if name, indented := strings.CutPrefix(line, "  "); indented && name != "" && name[0] != ' ' {
			verbs = append(verbs, strings.Fields(name)[0])
		}
	}
	return verbs
}

func subcommandOf(args []string) string {
	if at := slices.IndexFunc(args, func(arg string) bool { return !strings.HasPrefix(arg, "-") }); at >= 0 {
		return args[at]
	}
	return ""
}

func dispatch(ctx context.Context, args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) == 0 {
		return appVerb(in, out, errOut, sessionResume{})
	}
	if strings.HasPrefix(args[0], "chrome-extension://") {
		return browserverb.HostVerb(args[0], in, out, errOut)
	}
	if named := verbNamed(args[0]); named.known() {
		if slices.Contains(args[1:], "--help") || slices.Contains(args[1:], "-h") {
			_, _ = io.WriteString(out, verbHelp(args[0], args[1:]))
			return output.ExitOK
		}
		if named.clocked != nil {
			return named.clocked(ctx, args[1:], in, out, errOut)
		}
		return named.run(args[1:], in, out, errOut)
	}
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine("tofu: unknown verb "+strconv.Quote(args[0]), "tofu help"))
	return output.ExitUsage
}
