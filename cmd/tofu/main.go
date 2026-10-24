package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
	"tofu/library/changelog"
)

const (
	exitOK           = 0
	exitVerdict      = 1
	exitUsage        = 2
	profileFlag      = "--profile"
	profiledVariable = "TOFU_PROFILED"

	nearestSlackDivisor = 3
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
  judge     read a state and a question battery, print the answers
  check     run the tool gate on a shell command, in shadow, and log it
  session   list the recorded sessions, read one, rename one, or continue one
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
            with no terminal and no model call, and print the screens it asks for
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

Every verb that reports state takes --json and prints one JSON document.
`

func loadLocalTimeZone() { _ = time.Local.String() }

func crashDir(project string) string {
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		return os.TempDir()
	}
	return state
}

func crashTold(project string, tell func(string)) safe.Report {
	return func(p safe.Panic) { tell(p.Saved(crashDir(project))) }
}

func crashed(project string, err error) error {
	var caught safe.Panic
	if !errors.As(err, &caught) {
		return err
	}
	return errors.New(caught.Saved(crashDir(project)))
}

func main() {
	safe.Go(crashTold(".", func(said string) { _, _ = fmt.Fprintln(os.Stderr, "tofu: "+said) }), "time zone load", loadLocalTimeZone)
	stopProfile := sys.StartProfile()
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
	register := sys.Project.Register
	if opensTheApp {
		register = sys.Project.RegisterIdentityLater
	}
	letGo := func() {}
	if opens {
		openProject(os.Stderr, os.Stdin, dir, opensTheApp && isTerminal(os.Stdin) && isTerminal(os.Stderr), temporary, register)
		letGo = sys.HoldProjectOpen(dir)
	}
	stopSnapshots := func() {}
	if profiled {
		stopSnapshots = startSnapshots(os.Stderr, dir)
	}
	if wd, err := os.Getwd(); err == nil && (opensTheApp || opens) {
		moveProjectState(os.Stderr, wd)
	}
	moveHomeKeys(os.Stderr)
	pages := sys.ReadConsolePages()
	code := run(args, os.Stdin, os.Stdout, os.Stderr)
	pages.Restore()
	letGo()
	stopSnapshots()
	stopProfile()
	os.Exit(code)
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
	state, err := sys.ProjectStateDirAt(project)
	var dir string
	var stop func()
	if err == nil {
		dir, stop, err = sys.StartSnapshots(state)
	}
	if err != nil {
		_ = page.Print(errOut, page.ErrorLine("tofu: profiling not started: "+err.Error(), ""))
		return func() {}
	}
	_ = page.Print(errOut, []string{page.Receipt(cli.Changed, "profiling: CPU, heap and goroutines every "+sys.SnapshotEvery.String()+" and at exit, the last "+strconv.Itoa(sys.SnapshotsKept)+" kept", dir)})
	return func() {
		stop()
		_ = page.Print(errOut, []string{page.Receipt(cli.Changed, "profile written", dir)})
	}
}

func moveHomeKeys(errOut io.Writer) {
	migration, err := sys.MigrateHomeKeys()
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
	code := dispatch(args, in, out, errOut)
	if code != exitUsage || len(args) == 0 || slices.Contains(args, jsonFlag) {
		return code
	}
	if guess, found := nearestCommand(args); found {
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, cli.Indent(page.Hint("did you mean "+guess)))
	}
	return code
}

func nearestCommand(args []string) (string, bool) {
	_, verbLines, _ := strings.Cut(usage, "\nVerbs:\n")
	var verbs []string
	for _, line := range strings.Split(verbLines, "\n") {
		if name, indented := strings.CutPrefix(line, "  "); indented && name != "" && name[0] != ' ' {
			verbs = append(verbs, strings.Fields(name)[0])
		}
	}
	if !slices.Contains(verbs, args[0]) {
		near, found := nearestWord(args[0], verbs)
		return "tofu " + near, found
	}
	known := usageWords(verbUsage(args))
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
	best, distance := "", len(word)/nearestSlackDivisor+1
	for _, candidate := range candidates {
		if d := editDistance(word, candidate); d < distance {
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

func verbUsage(args []string) string {
	switch args[0] {
	case "models":
		return modelsUsage
	case "session":
		return strings.Join([]string{sessionSubcommands, sessionBranchUsage, sessionDrainUsage, sessionFindUsage, sessionTraceUsage}, " ")
	case "boardy":
		if len(args) > 1 {
			return boardyUsage + " " + boardyUsageOf(args[1])
		}
		return boardyUsage
	case "agents":
		return strings.Join([]string{agentsUsage, agentsAddUsage, agentsSetUsage, agentsRemoveUsage}, " ")
	case "rules":
		return strings.Join([]string{rulesSubcommands, rulesListUsage, rulesOverridesUsage, rulesCheckUsage, rulesAddUsage, rulesOffUsage, rulesIDUsage}, " ")
	case "settings":
		return settingsUsage
	case "memory":
		return memoryUsage + " " + memoryTreeUsage
	case "scratch":
		return scratchUsage
	case "changelog":
		return changelogUsage
	case "docs":
		return docsUsage
	case "doctor":
		return doctorUsage
	case "update":
		return updateUsage
	case "migrate":
		return migrateUsage
	case "reload":
		return reloadUsage
	case "library":
		return libraryUsage
	case "lint":
		return lintUsage
	case "undo":
		return undoUsage
	case "why":
		return whyUsage
	case "label":
		return labelUsage
	case "check":
		return checkUsage
	case "judge":
		return judgeUsage
	case "replay":
		return replayUsage
	case "learn":
		return learnUsage
	case "frame":
		return frameUsage
	case "drive":
		return driveUsage
	case "serve":
		return serveUsage
	case "guard":
		return guardUsage
	case "login":
		return loginUsage + " " + asideUsage
	case "logout":
		return logoutUsage
	case "usage":
		return usageFlags
	case host.HubVerb:
		return projectHostUsage
	}
	return ""
}

func dispatch(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) == 0 {
		return appVerb(in, out, errOut, sessionResume{})
	}
	if strings.HasPrefix(args[0], "chrome-extension://") {
		return hostVerb(args[0], in, out, errOut)
	}
	switch args[0] {
	case "--continue":
		return continueVerb(args[1:], in, out, errOut)
	case "session":
		return sessionVerb(args[1:], in, out, errOut)
	case "shells":
		return shellsVerb(args[1:], out, errOut)
	case "hooks":
		return hooksVerb(args[1:], out, errOut)
	case "guard":
		return guardVerb(args[1:], in, out, errOut)
	case "undo":
		return undoVerb(args[1:], out, errOut)
	case "version":
		return version(args[1:], out, errOut)
	case "update":
		return updateVerb(args[1:], out, errOut)
	case "changelog":
		return changelogVerb(args[1:], changelog.Markdown, out, errOut)
	case "docs":
		return docsVerb(args[1:], out, errOut)
	case "doctor":
		return doctor(args[1:], out, errOut)
	case "login":
		return loginVerb(args[1:], in, out, errOut)
	case "logout":
		return logoutVerb(args[1:], out, errOut)
	case "usage":
		return usageVerb(args[1:], out, errOut)
	case "models":
		return modelsVerb(args[1:], out, errOut)
	case "agents":
		return agentsVerb(args[1:], out, errOut)
	case "why":
		return whyVerb(args[1:], out, errOut, time.Now)
	case "frame":
		return frameVerb(args[1:], out, errOut)
	case "drive":
		return driveVerb(args[1:], in, out, errOut)
	case "serve":
		return serveVerb(args[1:], in, out, errOut)
	case host.HubVerb:
		return projectHostVerb(args[1:], out, errOut)
	case "run":
		return runVerb(args[1:], out, errOut)
	case "judge":
		return judgeVerb(args[1:], in, out, errOut)
	case "check":
		return checkVerb(args[1:], out, errOut)
	case "context":
		return contextVerb(args[1:], out, errOut)
	case "sift":
		return siftVerb(args[1:], in, out, errOut)
	case "label":
		return labelVerb(args[1:], out, errOut, time.Now)
	case "replay":
		return replayVerb(args[1:], out, errOut, time.Now)
	case "library":
		return libraryVerb(args[1:], out, errOut)
	case "catalog":
		_, _ = fmt.Fprintln(errOut, "tofu catalog is now tofu library")
		return libraryVerb(args[1:], out, errOut)
	case "lint":
		return lintVerb(args[1:], out, errOut)
	case "rules":
		return rulesVerb(args[1:], out, errOut)
	case "boardy":
		return boardyVerb(args[1:], out, errOut)
	case "scratch":
		return runScratch(args[1:], out, errOut)
	case "memory":
		return memoryVerb(args[1:], out, errOut)
	case "learn":
		return learnVerb(args[1:], out, errOut)
	case "settings":
		return settingsVerb(args[1:], out, errOut)
	case "reload":
		return reloadVerb(args[1:], out, errOut)
	case "migrate":
		return migrateVerb(args[1:], out, errOut)
	case "browser":
		return browserVerb(args[1:], out, errOut)
	case "preview":
		return previewVerb(args[1:], out, errOut)
	case "help", "-h", "--help":
		_, _ = fmt.Fprintf(out, usage, konst.TemporaryProjectIdleHours)
		return exitOK
	default:
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, page.ErrorLine("tofu: unknown verb "+strconv.Quote(args[0]), "tofu help"))
		return exitUsage
	}
}
