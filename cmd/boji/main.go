package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

const (
	exitOK      = 0
	exitVerdict = 1
	exitUsage   = 2
)

const usage = `boji is a coding agent harness.

Usage:
  boji                    start the app in this directory
  boji <verb> [arguments]

Verbs:
  version   print the version, the commit and the Go version
  doctor    report the environment and what is wrong with it
  login     mint a subscription credential for a provider
  usage     print every credential's quota windows and when each resets
  models    list the models each subscription serves, and why one is excluded
  why       explain a ledger row, or the last one
  run       work a task in a directory until it is done
  judge     read a state and a question battery, print the answers
  check     run the tool gate on a shell command, in shadow, and log it
  label     attach an outcome to a ledger row, by id or the last one
  replay    re-score ledger rows against changed thresholds, no network
  catalog   resolve a catalog entry and show the origin of each field
  lint      run a house-rule check over the tree
  rules     list or run the rule catalog
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) == 0 {
		return appVerb(in, out, errOut)
	}
	switch args[0] {
	case "version":
		return version(out)
	case "doctor":
		return doctor(out, args[1:]...)
	case "login":
		return loginVerb(args[1:], in, out, errOut)
	case "usage":
		return usageVerb(args[1:], out, errOut)
	case "models":
		return modelsVerb(args[1:], out, errOut)
	case "why":
		return whyVerb(args[1:], out, errOut, time.Now)
	case "run":
		return runVerb(args[1:], out, errOut)
	case "judge":
		return judgeVerb(args[1:], in, out, errOut)
	case "check":
		return checkVerb(args[1:], out, errOut)
	case "label":
		return labelVerb(args[1:], out, errOut, time.Now)
	case "replay":
		return replayVerb(args[1:], out, errOut, time.Now)
	case "catalog":
		return catalogVerb(args[1:], out, errOut)
	case "lint":
		return lintVerb(args[1:], out, errOut)
	case "rules":
		return rulesVerb(args[1:], out, errOut)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return exitOK
	default:
		_, _ = fmt.Fprintf(errOut, "boji: unknown verb %q\n\n%s", args[0], usage)
		return exitUsage
	}
}
