package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/sys"
	"tofu/library/changelog"
)

const (
	exitOK      = 0
	exitVerdict = 1
	exitUsage   = 2
)

const usage = `tofu is a coding agent harness.

Usage:
  tofu                    start the app in this directory
  tofu --continue         start the app on the session you last worked in
  tofu <verb> [arguments]

Verbs:
  version   print the version, the commit and the Go version
  changelog print what changed since the version you last read
  update    install the latest release over this tofu,
            or --check to only say whether one is newer
  docs      print what you can ask tofu and the command that does it,
            a page with docs <topic>, or the closest answers with docs "a few words"
  doctor    say whether tofu can run here, and what is wrong if it cannot
  login     sign in or store a key by role: login llm claude-sub|codex-sub|meta,
            login classifier openrouter|typesafe, login search brave
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
  undo      put back the files the last turn changed, or the last N turns,
            and refuse by name a file changed after the turn
  context   print the context bands of a recorded session and what fills them
  sift      mark what is worth reading in a message and elide the rest
  label     attach an outcome to a ledger row, by id or the last one
  replay    re-score ledger rows against changed thresholds, no network
  library   resolve a library entry and show the origin of each field
  lint      run a house-rule check over the tree
  rules     list the rule library, run it, read back what fired, or index a task
  frame     print one recorded interface frame, at a width and a height
  drive     run a script of what a person would do against the real app,
            with no terminal and no model call, and print the screens it asks for
  settings  list, get or set a declared setting, global or project
  reload    re-read settings, rules, skills, sub-agents, models, roles, tiers,
            instruction files and keys, and print what changed since the last reload
  migrate   move what tofu wrote out of this project's .tofu and into ~/.tofu,
            or --dry-run to list what would move
  browser   list the Chrome tabs tofu can reach, install or uninstall the extension,
            or open and close a background tab tofu owns

Every verb that reports state takes --json and prints one JSON document.
`

func main() {
	opensTheApp := len(os.Args) < 2 || os.Args[1] == "--continue"
	if opensTheApp || os.Args[1] == "migrate" {
		copyLegacyStateDirs(os.Stderr)
	}
	if wd, err := os.Getwd(); err == nil && opensTheApp {
		moveProjectState(os.Stderr, wd)
	}
	moveHomeKeys(os.Stderr)
	pages := sys.ReadConsolePages()
	code := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	pages.Restore()
	os.Exit(code)
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
		_, _ = fmt.Fprint(out, usage)
		return exitOK
	default:
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, page.ErrorLine("tofu: unknown verb "+strconv.Quote(args[0]), "tofu help"))
		return exitUsage
	}
}
