package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"tofu/interface/cli"
	"tofu/internal/konst"
	sessionstore "tofu/internal/session"
	rewind "tofu/internal/snapshot"
)

const undoUsage = "tofu undo [N] [--dir path] [--session id] [--force] [--dry-run] [--json]"

func undoVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "undo", usageLine: undoUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	ask, handle, dir, err := undoArgs(withoutJSON(args))
	if err != nil {
		return o.usage(err)
	}
	store, err := sessionstore.OpenIn(dir)
	if err != nil {
		return o.fail(err)
	}
	if handle == "" {
		head, err := store.Head()
		if err != nil {
			return o.fail(rewind.NothingRecorded{Asked: ask.Turns})
		}
		handle = head.ID
	}
	report, err := undoSession(store, handle, ask)
	if err != nil {
		return o.fail(err)
	}
	return o.done(len(report.Refused) == 0, report, func(page cli.Page) []string { return undoLines(page, report) })
}

func undoArgs(args []string) (rewind.Ask, string, string, error) {
	var ask rewind.Ask
	handle, dir, count := "", ".", ""
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--force":
			ask.Force = true
		case arg == "--dry-run":
			ask.DryRun = true
		case arg == "--session" && i+1 < len(args):
			i++
			handle = args[i]
		case arg == "--dir" && i+1 < len(args):
			i++
			dir = args[i]
		case strings.HasPrefix(arg, "-") || count != "":
			return ask, "", "", fmt.Errorf("unknown argument %q", arg)
		default:
			count = arg
		}
	}
	turns, err := undoCount(count)
	ask.Turns = turns
	return ask, handle, dir, err
}

func undoCount(word string) (int, error) {
	if word == "" {
		return 1, nil
	}
	turns, err := strconv.Atoi(word)
	if err != nil || turns < 1 {
		return 0, fmt.Errorf("%q is not a number of turns: give 1 or more", word)
	}
	return turns, nil
}

func undoSession(store *sessionstore.Store, handle string, ask rewind.Ask) (rewind.Report, error) {
	headers, err := store.Resolve(handle)
	if err != nil {
		return rewind.Report{}, err
	}
	if len(headers) > 1 {
		return rewind.Report{}, fmt.Errorf("%s names %d sessions: give the id", handle, len(headers))
	}
	repo := rewind.Repo{State: store.State(), Session: store.Dir(cmp.Or(headers[0].Root, headers[0].ID)), Tree: store.Project()}
	return repo.Undo(context.Background(), ask)
}

func undoLines(page cli.Page, report rewind.Report) []string {
	restored, removed := "restored", "removed"
	if report.DryRun {
		restored, removed = "would restore", "would remove"
	}
	lines := page.Title("Undo", []string{plural(len(report.Turns), "turn"), strings.Join(report.Turns, ", ")}, cli.Verdict{})
	for _, path := range report.Restored {
		lines = append(lines, page.Glyph(cli.Changed)+" "+restored+" "+path)
	}
	for _, path := range report.Removed {
		lines = append(lines, page.Glyph(cli.Removed)+" "+removed+" "+path)
	}
	for _, refused := range report.Refused {
		lines = append(lines, page.Glyph(cli.Fail)+" refused "+refused.Path+cli.Gap+page.Label(refused.Why))
	}
	if len(report.Restored)+len(report.Removed)+len(report.Refused) == 0 {
		lines = append(lines, page.Glyph(cli.Idle)+" nothing to put back: the files already match the start of those turns")
	}
	return lines
}

func undoTurns(id, count string) string {
	turns, err := undoCount(count)
	if err != nil {
		return "/undo: " + err.Error()
	}
	store, err := sessionstore.Open()
	if err != nil {
		return "/undo: " + err.Error()
	}
	report, err := undoSession(store, id, rewind.Ask{Turns: turns})
	if id == "" || errors.Is(err, fs.ErrNotExist) {
		err = rewind.NothingRecorded{Asked: turns}
	}
	if err != nil {
		return "/undo: " + err.Error()
	}
	return strings.Join(undoLines(cli.Page{Profile: colorprofile.NoTTY, Width: konst.ProseWidthChars}, report), "\n")
}
