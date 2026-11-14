package snapshot

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	ledgerLifetime     = 7 * 24 * time.Hour
	undoableTurns      = 20
	PruneGrace         = "1.hour.ago"
	pinRefs            = "refs/undo/"
	pruneLockName      = "prune.lock"
	pruneLockLifetime  = time.Hour
	pruneEvery         = time.Hour
	prunedStampName    = "pruned"
	ledgerName         = "undo-turns.jsonl"
	ledgerLockName     = "undo-turns.lock"
	sharedIndexPin     = "shared"
	driftedWhy         = "changed after the turn ended, so it was left as it is: --force puts it back anyway"
	sessionIndexName   = "undo.index"
	sessionExcludeName = "undo.exclude"
)

type Repo struct {
	State   string
	Session string
	Tree    string
}

type Pruned struct {
	Turn string
}

func (e Pruned) Error() string {
	return "the files of turn " + e.Turn + " are no longer in the undo store, so it cannot be undone"
}

func (r Repo) files() Files { return Files{State: r.State, Tree: r.Tree} }

func (r Repo) Prune(ctx context.Context) error {
	files := r.files()
	if info, err := os.Stat(files.prunedStamp()); err == nil && time.Since(info.ModTime()) < pruneEvery {
		return nil
	}
	lock := files.path(pruneLockName)
	if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > pruneLockLifetime {
		_ = os.Remove(lock)
	}
	held, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	_ = held.Close()
	defer func() { _ = os.Remove(lock) }()
	sessions := filepath.Dir(r.Session)
	dirs, err := os.ReadDir(sessions)
	if err != nil {
		return err
	}
	pins := map[string]string{}
	for _, dir := range dirs {
		live := Repo{State: r.State, Session: filepath.Join(sessions, dir.Name()), Tree: r.Tree}
		info, err := os.Stat(live.path(ledgerName))
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > ledgerLifetime {
			for _, name := range []string{ledgerName, sessionIndexName, sessionExcludeName} {
				if err := os.Remove(live.path(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			continue
		}
		entries, err := live.entries()
		if err != nil {
			return err
		}
		for i, one := range entries[max(0, len(entries)-undoableTurns):] {
			for side, tree := range []string{one.Start, one.End} {
				if tree != "" {
					pins[fmt.Sprintf("%s%s/%d", pinRefs, dir.Name(), 2*i+side)] = tree
				}
			}
		}
	}
	if indexed, err := files.writeTree(ctx); err == nil {
		pins[pinRefs+sharedIndexPin] = indexed
	}
	if err := files.pinOnly(ctx, pins); err != nil {
		return err
	}
	if _, err := files.Git(ctx, "", "prune", "--expire="+PruneGrace); err != nil {
		return err
	}
	return files.stampPruned()
}

func (f Files) pinOnly(ctx context.Context, pins map[string]string) error {
	var trees strings.Builder
	for _, tree := range pins {
		trees.WriteString(tree + "\n")
	}
	checked, err := f.Git(ctx, trees.String(), "cat-file", "--batch-check")
	if err != nil {
		return err
	}
	for line := range strings.Lines(checked) {
		if tree, absent := strings.CutSuffix(strings.TrimSpace(line), " missing"); absent {
			maps.DeleteFunc(pins, func(_, pinned string) bool { return pinned == tree })
		}
	}
	listed, err := f.Git(ctx, "", "for-each-ref", "--format=%(refname)", pinRefs)
	if err != nil {
		return err
	}
	var moves strings.Builder
	for _, ref := range strings.Fields(listed) {
		if pins[ref] == "" {
			moves.WriteString("delete " + ref + "\n")
		}
	}
	for ref, tree := range pins {
		moves.WriteString("update " + ref + " " + tree + "\n")
	}
	_, err = f.Git(ctx, moves.String(), "update-ref", "--stdin")
	return err
}

type Ask struct {
	Turn   string
	Turns  int
	Force  bool
	DryRun bool
}

type Refusal struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

type Report struct {
	Turns    []string  `json:"turns"`
	Restored []string  `json:"restored"`
	Removed  []string  `json:"removed"`
	Refused  []Refusal `json:"refused"`
	DryRun   bool      `json:"dry_run"`
}

type NothingRecorded struct {
	Asked    int
	Recorded int
}

func (e NothingRecorded) Error() string {
	if e.Recorded == 0 {
		return "nothing to undo: no turn of this session recorded its files"
	}
	return fmt.Sprintf("%d turns were asked, and this session has recorded %d", e.Asked, e.Recorded)
}

type StillRunning struct {
	Turn string
}

func (e StillRunning) Error() string {
	return "turn " + e.Turn + " has a start and no end: it is still running, or it was killed. --force undoes it to the files as they are now"
}

type NotNewest struct {
	Asked  string
	Newest string
}

func (e NotNewest) Error() string {
	return "turn " + e.Asked + " is not the newest recorded turn of this session, " + e.Newest + " is: it was undone already, or a later turn came after it"
}

type LedgerHeld struct {
	Lock string
}

func (e LedgerHeld) Error() string {
	return fmt.Sprintf("%s is held by another undo or turn past %d ms", e.Lock, konst.UndoLockWaitMillis)
}

type entry struct {
	Turn  string `json:"turn,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func (r Repo) Begin(ctx context.Context, turn string) error {
	return r.BeginFrom(ctx, turn, func() (string, error) { return r.files().Snapshot(ctx) })
}

func (r Repo) BeginFrom(ctx context.Context, turn string, start func() (string, error)) error {
	return r.locked(ctx, func() error {
		tree, err := start()
		if err != nil {
			return err
		}
		entries, err := r.entries()
		if err != nil {
			return err
		}
		if len(entries) >= undoableTurns {
			if err := r.rewrite(entries[len(entries)-undoableTurns+1:]); err != nil {
				return err
			}
		}
		return r.append(entry{Turn: turn, Start: tree})
	})
}

func (r Repo) End(ctx context.Context) error {
	return r.EndFrom(ctx, func() (string, error) { return r.files().Snapshot(ctx) })
}

func (r Repo) EndFrom(ctx context.Context, end func() (string, error)) error {
	return r.locked(ctx, func() error {
		entries, err := r.entries()
		if err != nil {
			return err
		}
		tree, err := end()
		if err != nil {
			return err
		}
		closing := entry{End: tree}
		if open := len(entries) - 1; open >= 0 && entries[open].End == "" {
			closing.Turn = entries[open].Turn
		}
		return r.append(closing)
	})
}

func (r Repo) Undo(ctx context.Context, ask Ask) (Report, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Report{}, ErrNoGit
	}
	var report Report
	err := r.locked(ctx, func() error {
		return r.files().held(ctx, func() (err error) {
			report, err = r.undo(ctx, ask)
			return err
		})
	})
	return report, err
}

func (r Repo) locked(ctx context.Context, work func() error) error {
	if err := os.MkdirAll(r.Session, 0o755); err != nil {
		return err
	}
	return hold(ctx, r.path(ledgerLockName), konst.UndoLockLifetimeSeconds*time.Second, work)
}

func (r Repo) undo(ctx context.Context, ask Ask) (Report, error) {
	files := r.files()
	entries, err := r.entries()
	if err != nil {
		return Report{}, err
	}
	if ask.Turns > len(entries) {
		return Report{}, NothingRecorded{Asked: ask.Turns, Recorded: len(entries)}
	}
	if newest := entries[len(entries)-1].Turn; ask.Turn != "" && ask.Turn != newest {
		return Report{}, NotNewest{Asked: ask.Turn, Newest: newest}
	}
	kept, undone := entries[:len(entries)-ask.Turns], entries[len(entries)-ask.Turns:]
	last := undone[len(undone)-1]
	if last.End == "" && !ask.Force {
		return Report{}, StillRunning{Turn: last.Turn}
	}
	present, err := files.Git(ctx, undone[0].Start+"\n"+cmp.Or(last.End, undone[0].Start)+"\n", "cat-file", "--batch-check")
	if err != nil {
		return Report{}, err
	}
	if missing := strings.Split(present, "\n"); strings.HasSuffix(missing[0], " missing") {
		return Report{}, Pruned{Turn: undone[0].Turn}
	} else if strings.HasSuffix(missing[1], " missing") {
		return Report{}, Pruned{Turn: last.Turn}
	}
	now, err := files.track(ctx)
	if err != nil {
		return Report{}, err
	}
	start, end := undone[0].Start, cmp.Or(last.End, now)
	changed, err := files.diff(ctx, "--name-status", start, end)
	if err != nil {
		return Report{}, err
	}
	notAsStarted, err := files.diff(ctx, "--name-only", start, now)
	if err != nil {
		return Report{}, err
	}
	drifted, err := files.diff(ctx, "--name-only", end, now)
	if err != nil {
		return Report{}, err
	}
	report := Report{Restored: []string{}, Removed: []string{}, Refused: []Refusal{}, DryRun: ask.DryRun}
	for _, one := range undone {
		report.Turns = append(report.Turns, one.Turn)
	}
	failed := false
	for i := 0; i+1 < len(changed); i += 2 {
		created, path := changed[i] == "A", changed[i+1]
		if !slices.Contains(notAsStarted, path) {
			continue
		}
		if slices.Contains(drifted, path) && !ask.Force {
			report.Refused = append(report.Refused, Refusal{Path: path, Why: driftedWhy})
			continue
		}
		var err error
		switch {
		case ask.DryRun:
		case created:
			err = r.remove(path)
		default:
			_, err = files.Git(ctx, ":(literal)"+path+"\x00", "checkout", start, "--pathspec-from-file=-", "--pathspec-file-nul")
		}
		switch {
		case err != nil:
			failed = true
			report.Refused = append(report.Refused, Refusal{Path: path, Why: err.Error()})
		case created:
			report.Removed = append(report.Removed, path)
		default:
			report.Restored = append(report.Restored, path)
		}
	}
	if ask.DryRun || failed {
		return report, nil
	}
	return report, r.rewrite(kept)
}

func (r Repo) path(name string) string { return filepath.Join(r.Session, name) }

func (f Files) diff(ctx context.Context, shape, from, to string) ([]string, error) {
	out, err := f.Git(ctx, "", "diff-tree", "-r", "-z", "--no-renames", shape, from, to)
	return nulSplit(out), err
}

func (r Repo) remove(path string) error {
	full := filepath.Join(r.Tree, filepath.FromSlash(path))
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(full)
	for dir != filepath.Clean(r.Tree) && os.Remove(dir) == nil {
		dir = filepath.Dir(dir)
	}
	return nil
}

func (r Repo) append(line entry) error {
	file, err := os.OpenFile(r.path(ledgerName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	return errors.Join(json.NewEncoder(file).Encode(line), file.Close())
}

func (r Repo) entries() ([]entry, error) {
	raw, err := os.ReadFile(r.path(ledgerName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []entry
	for i, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		var mark entry
		if err := json.Unmarshal([]byte(line), &mark); err != nil {
			return nil, fmt.Errorf("line %d of %s does not parse: %w", i+1, ledgerName, err)
		}
		switch {
		case mark.Start != "":
			if open := len(entries) - 1; open >= 0 && entries[open].End == "" {
				entries[open].End = mark.Start
			}
			entries = append(entries, mark)
		case len(entries) > 0 && entries[len(entries)-1].End == "":
			entries[len(entries)-1].End = mark.End
		}
	}
	return entries, nil
}

func (r Repo) rewrite(kept []entry) error {
	var lines bytes.Buffer
	for _, one := range kept {
		if err := json.NewEncoder(&lines).Encode(one); err != nil {
			return err
		}
	}
	path := r.path(ledgerName)
	if err := os.WriteFile(path+".tmp", lines.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
