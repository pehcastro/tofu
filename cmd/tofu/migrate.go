package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/session"
	"tofu/internal/sys"
)

const migrateUsage = `usage: tofu migrate [--dry-run]

Moves what tofu wrote about its own runs out of this project's .tofu and into
the home folder: sessions, log, artifacts, cache, salvage, calibration, shells
and promotions go to ~/.tofu/projects/<key>, and quota readings to ~/.tofu/quota.
What a person wrote stays. Then it converts every session recorded in the old
layout, a turn-<id> folder or a single turn file, into one folder per session
holding session.json and events.jsonl: a sub-agent's run joins the session that
spawned it, a fork becomes a session carried from the one it left, and a
message a later turn repeated is kept once. --dry-run lists the move and the
sessions it would build, and touches nothing.`

type stateMove struct {
	name, from, to string
}

func plannedStateMoves(project string) (config string, moves []stateMove, err error) {
	config = sys.StateDir(project)
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		return "", nil, err
	}
	quota, err := sys.QuotaDir()
	if err != nil {
		return "", nil, err
	}
	for _, name := range sys.MovedStateNames() {
		move := stateMove{name: name, from: filepath.Join(config, name), to: filepath.Join(state, name)}
		if name == sys.QuotaDirName {
			move.to = quota
		}
		if _, err := os.Stat(move.from); err == nil && move.from != move.to {
			moves = append(moves, move)
		}
	}
	return config, moves, nil
}

func moveProjectState(out io.Writer, project string) (moved, failed int) {
	config, moves, err := plannedStateMoves(project)
	if err != nil {
		_, _ = fmt.Fprintf(out, "tofu: the state in %s was not moved: %v\n", sys.StateDir(project), err)
		return 0, 1
	}
	var names, targets []string
	var files int
	var size int64
	for _, move := range moves {
		copied, bytes, err := copyTreeInto(os.DirFS(config), move.name, filepath.Dir(move.to))
		if err == nil {
			err = os.RemoveAll(move.from)
		}
		if err != nil {
			failed++
			_, _ = fmt.Fprintf(out, "tofu: %s stays in %s and is unread until tofu migrate moves it: %v\n", move.name, config, err)
			continue
		}
		moved, files, size = moved+1, files+copied, size+bytes
		names = append(names, move.name)
		if target := filepath.Dir(move.to); !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	if moved > 0 {
		_, _ = fmt.Fprintf(out, "tofu: moved %s (%d files, %d bytes) out of %s into %s\n",
			strings.Join(names, ", "), files, size, config, strings.Join(targets, " and "))
	}
	return moved, failed
}

func migrateVerb(args []string, out, errOut io.Writer) int {
	dryRun := false
	for _, arg := range args {
		if arg != "--dry-run" {
			_, _ = fmt.Fprintf(errOut, "tofu migrate: unknown argument %q\n\n%s\n", arg, migrateUsage)
			return exitUsage
		}
		dryRun = true
	}
	project, err := os.Getwd()
	var config, state string
	var moves []stateMove
	if err == nil {
		config, moves, err = plannedStateMoves(project)
	}
	if err == nil {
		state, err = sys.ProjectStateDirAt(project)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu migrate: %v\n", err)
		return exitVerdict
	}
	switch {
	case len(moves) == 0:
		_, _ = fmt.Fprintf(out, "tofu migrate: %s holds no state, so there is nothing to move\n", config)
	case dryRun:
		_, _ = fmt.Fprintf(out, "tofu migrate --dry-run: would move out of %s\n", config)
		for _, move := range moves {
			files, size := treeSize(move.from)
			_, _ = fmt.Fprintf(out, "  %-16s -> %s  (%d files, %d bytes)\n", move.name, move.to, files, size)
		}
	default:
		if _, failed := moveProjectState(out, project); failed > 0 {
			return exitVerdict
		}
	}
	sessions := session.OpenAt(state)
	for _, move := range moves {
		if dryRun && move.name == "sessions" {
			sessions = session.NewStore(move.from)
		}
	}
	return convertSessions(out, errOut, sessions, dryRun)
}

func convertSessions(out, errOut io.Writer, sessions *session.Store, dryRun bool) int {
	plan, err := sessions.PlanConversion()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu migrate: the sessions in the old layout were not read: %v\n", err)
		return exitVerdict
	}
	for _, skipped := range plan.Skipped {
		_, _ = fmt.Fprintf(out, "tofu migrate: %s stays in the old layout, it does not read: %v\n", skipped.ID, skipped.Reason)
	}
	if len(plan.Sessions) == 0 {
		_, _ = fmt.Fprintln(out, "tofu migrate: no session is recorded in the old layout, so there is nothing to convert")
		return exitOK
	}
	entries, turns, events, agents := 0, 0, 0, 0
	lead := "tofu migrate --dry-run: would convert"
	if !dryRun {
		lead = "tofu migrate: converting"
	}
	_, _ = fmt.Fprintf(out, "%s %d sessions:\n", lead, len(plan.Sessions))
	for _, converted := range plan.Sessions {
		header := converted.Header
		entries, turns, events, agents = entries+len(converted.From), turns+header.Turns, events+len(converted.Events), agents+len(header.Agents)
		_, _ = fmt.Fprintf(out, "  %s  %d turns  %d events  %d sub-agent runs  from %s\n",
			header.ID, header.Turns, len(converted.Events), len(header.Agents), strings.Join(converted.From, " "))
	}
	_, _ = fmt.Fprintf(out, "  %d old entries, %d sessions, %d turns, %d events, %d sub-agent runs; head %s\n",
		entries, len(plan.Sessions), turns, events, agents, cmp.Or(plan.Head, "unchanged"))
	if dryRun {
		return exitOK
	}
	written, err := sessions.Convert(plan)
	_, _ = fmt.Fprintf(out, "tofu migrate: wrote %d of %d sessions\n", written, len(plan.Sessions))
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu migrate: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func treeSize(root string) (files int, size int64) {
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil {
			files, size = files+1, size+info.Size()
		}
		return err
	})
	return files, size
}
