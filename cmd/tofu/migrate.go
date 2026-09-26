package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/sys"
)

const migrateUsage = `usage: tofu migrate [--dry-run]

Moves what tofu wrote about its own runs out of this project's .tofu and into
the home folder: sessions, log, artifacts, cache, salvage, calibration, shells
and promotions go to ~/.tofu/projects/<key>, and quota readings to ~/.tofu/quota.
What a person wrote stays. --dry-run lists the move and touches nothing.`

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
	var config string
	var moves []stateMove
	if err == nil {
		config, moves, err = plannedStateMoves(project)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu migrate: %v\n", err)
		return exitVerdict
	}
	if len(moves) == 0 {
		_, _ = fmt.Fprintf(out, "tofu migrate: %s holds no state, so there is nothing to move\n", config)
		return exitOK
	}
	if !dryRun {
		if _, failed := moveProjectState(out, project); failed > 0 {
			return exitVerdict
		}
		return exitOK
	}
	_, _ = fmt.Fprintf(out, "tofu migrate --dry-run: would move out of %s\n", config)
	for _, move := range moves {
		files, size := treeSize(move.from)
		_, _ = fmt.Fprintf(out, "  %-16s -> %s  (%d files, %d bytes)\n", move.name, move.to, files, size)
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
