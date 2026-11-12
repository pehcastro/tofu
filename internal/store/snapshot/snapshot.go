package snapshot

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	platformfs "tofu/internal/platform/fs"
)

const (
	largestUntrackedBytes = 10 << 20
	ledgerLifetime        = 7 * 24 * time.Hour
	undoableTurns         = 20
	pruneGrace            = "1.hour.ago"
	pinRefs               = "refs/undo/"
	pruneLockName         = "prune.lock"
	pruneLockLifetime     = time.Hour
	pruneEvery            = time.Hour
	prunedStampName       = "pruned"
	gitDirName            = "undo.git"
	ledgerName            = "undo-turns.jsonl"
	ledgerLockName        = "undo-turns.lock"
	indexName             = "undo.index"
	sharedIndexTemp       = "undo.index.shared"
	sharedIndexPin        = "shared"
	packEveryBlob         = "core.bigFileThreshold=1"
	excludeName           = "undo.exclude"
	walkName              = "undo.walk"
	verbatimAttributes    = "* -text -filter -ident -working-tree-encoding\n"
	driftedWhy            = "changed after the turn ended, so it was left as it is: --force puts it back anyway"
)

var ErrNoGit = errors.New("undo needs git on PATH, and there is none")

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

func (r Repo) Prune(ctx context.Context) error {
	if info, err := os.Stat(r.prunedStamp()); err == nil && time.Since(info.ModTime()) < pruneEvery {
		return nil
	}
	lock := filepath.Join(r.gitDir(), pruneLockName)
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
	var pins strings.Builder
	for _, dir := range dirs {
		live := Repo{State: r.State, Session: filepath.Join(sessions, dir.Name()), Tree: r.Tree}
		info, err := os.Stat(live.path(ledgerName))
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > ledgerLifetime {
			for _, name := range []string{ledgerName, indexName, excludeName} {
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
		trees := []string{}
		for _, one := range entries[max(0, len(entries)-undoableTurns):] {
			trees = append(trees, one.Start, one.End)
		}
		if indexed, err := live.writeTree(ctx); err == nil {
			trees = append(trees, indexed)
		}
		for i, tree := range trees {
			if tree != "" {
				fmt.Fprintf(&pins, "update %s%s/%d %s\n", pinRefs, dir.Name(), i, tree)
			}
		}
	}
	if indexed, err := r.shared().writeTree(ctx); err == nil {
		fmt.Fprintf(&pins, "update %s%s %s\n", pinRefs, sharedIndexPin, indexed)
	}
	if err := os.RemoveAll(filepath.Join(r.gitDir(), filepath.FromSlash(pinRefs))); err != nil {
		return err
	}
	if _, err := r.git(ctx, pins.String(), "update-ref", "--stdin"); err != nil {
		return err
	}
	if _, err := r.git(ctx, "", "prune", "--expire="+pruneGrace); err != nil {
		return err
	}
	return r.stampPruned()
}

func (r Repo) prunedStamp() string {
	return filepath.Join(r.gitDir(), "objects", "info", prunedStampName)
}

func (r Repo) stampPruned() error {
	return os.WriteFile(r.prunedStamp(), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644)
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
	return r.locked(ctx, func() error {
		tree, err := r.track(ctx)
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
	return r.locked(ctx, func() error {
		entries, err := r.entries()
		if err != nil {
			return err
		}
		tree, err := r.track(ctx)
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
	err := r.locked(ctx, func() (err error) {
		report, err = r.undo(ctx, ask)
		return err
	})
	return report, err
}

func (r Repo) locked(ctx context.Context, work func() error) error {
	if err := os.MkdirAll(r.Session, 0o755); err != nil {
		return err
	}
	lock := r.path(ledgerLockName)
	for deadline := time.Now().Add(konst.UndoLockWaitMillis * time.Millisecond); ; time.Sleep(konst.UndoLockRetryMillis * time.Millisecond) {
		held, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = held.Close()
			defer func() { _ = os.Remove(lock) }()
			return work()
		}
		if !errors.Is(err, fs.ErrExist) && !errors.Is(err, fs.ErrPermission) {
			return err
		}
		if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > konst.UndoLockLifetimeSeconds*time.Second {
			_ = os.Remove(lock)
		} else if time.Now().After(deadline) {
			return LedgerHeld{Lock: lock}
		} else if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (r Repo) undo(ctx context.Context, ask Ask) (Report, error) {
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
	present, err := r.git(ctx, undone[0].Start+"\n"+cmp.Or(last.End, undone[0].Start)+"\n", "cat-file", "--batch-check")
	if err != nil {
		return Report{}, err
	}
	if missing := strings.Split(string(present), "\n"); strings.HasSuffix(missing[0], " missing") {
		return Report{}, Pruned{Turn: undone[0].Turn}
	} else if strings.HasSuffix(missing[1], " missing") {
		return Report{}, Pruned{Turn: last.Turn}
	}
	now, err := r.track(ctx)
	if err != nil {
		return Report{}, err
	}
	start, end := undone[0].Start, cmp.Or(last.End, now)
	changed, err := r.diff(ctx, "--name-status", start, end)
	if err != nil {
		return Report{}, err
	}
	notAsStarted, err := r.diff(ctx, "--name-only", start, now)
	if err != nil {
		return Report{}, err
	}
	drifted, err := r.diff(ctx, "--name-only", end, now)
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
			_, err = r.git(ctx, ":(literal)"+path+"\x00", "checkout", start, "--pathspec-from-file=-", "--pathspec-file-nul")
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

func (r Repo) gitDir() string { return filepath.Join(r.State, gitDirName) }

func (r Repo) path(name string) string { return filepath.Join(r.Session, name) }

func (r Repo) shared() Repo { return Repo{State: r.State, Session: r.gitDir(), Tree: r.Tree} }

func (r Repo) git(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd, stderr := r.command(ctx, stdin, args...)
	out, err := cmd.Output()
	return out, failed(args[0], err, stderr)
}

func (r Repo) command(ctx context.Context, stdin string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.autocrlf=false", "-c", "core.fsmonitor=false", "-c", "core.longpaths=true",
		"-c", "core.quotepath=false", "-c", "core.excludesFile=" + r.path(excludeName), "--git-dir", r.gitDir(), "--work-tree", r.Tree}, args...)...)
	cmd.Dir, cmd.Stdin, cmd.WaitDelay = r.Tree, strings.NewReader(stdin), konst.BashWaitDelayMillis*time.Millisecond
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(strings.ToUpper(variable), "GIT_") }),
		"GIT_INDEX_FILE="+r.path(indexName))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd, &stderr
}

func failed(verb string, err error, stderr *bytes.Buffer) error {
	if errors.Is(err, exec.ErrNotFound) {
		return ErrNoGit
	}
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", verb, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r Repo) track(ctx context.Context) (string, error) {
	if err := os.MkdirAll(r.Session, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(r.gitDir(), "info", "attributes")); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(r.gitDir(), 0o755); err != nil {
			return "", err
		}
		if _, err := r.git(ctx, "", "init", "--quiet"); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(r.gitDir(), "info", "attributes"), []byte(verbatimAttributes), 0o644); err != nil {
			return "", err
		}
		if err := r.stampPruned(); err != nil {
			return "", err
		}
	}
	index := r.path(indexName)
	if err := os.Remove(index + ".lock"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	cold := false
	if _, err := os.Stat(index); err != nil {
		shared, err := os.ReadFile(r.shared().path(indexName))
		if cold = err != nil; !cold {
			if err := os.WriteFile(index, shared, 0o644); err != nil {
				return "", err
			}
		}
	}
	if err := r.exclude(nil); err != nil {
		return "", err
	}
	skipped, err := r.junctions(ctx)
	if err != nil {
		return "", err
	}
	if err := r.exclude(skipped); err != nil {
		return "", err
	}
	untracked, err := r.git(ctx, "", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	for _, path := range nulSplit(untracked) {
		if info, err := os.Lstat(filepath.Join(r.Tree, filepath.FromSlash(path))); err == nil && info.Size() > largestUntrackedBytes {
			skipped = append(skipped, path)
		}
	}
	if err := r.exclude(skipped); err != nil {
		return "", err
	}
	add := []string{"add", "--all"}
	if cold {
		add = append([]string{"-c", packEveryBlob}, add...)
	}
	if _, err := r.git(ctx, "", add...); err != nil {
		return "", err
	}
	tree, err := r.writeTree(ctx)
	if err != nil {
		return "", err
	}
	if kept, err := os.ReadFile(index); err == nil && os.WriteFile(r.path(sharedIndexTemp), kept, 0o644) == nil {
		_ = os.Rename(r.path(sharedIndexTemp), r.shared().path(indexName))
	}
	return tree, nil
}

type walkedDir struct {
	Changed  int64    `json:"changed"`
	Ignore   string   `json:"ignore,omitempty"`
	Children []string `json:"children,omitempty"`
	Ignored  []string `json:"ignored,omitempty"`
	Links    []string `json:"links,omitempty"`
}

type walk struct {
	Rules string               `json:"rules"`
	Dirs  map[string]walkedDir `json:"dirs"`
}

type visit struct {
	dir     string
	trusted bool
}

func (r Repo) junctions(ctx context.Context) ([]string, error) {
	rules, err := os.ReadFile(r.path(excludeName))
	if err != nil {
		return nil, err
	}
	var was walk
	if raw, err := os.ReadFile(filepath.Join(r.State, walkName)); err == nil {
		_ = json.Unmarshal(raw, &was)
	}
	now := walk{Rules: string(rules), Dirs: map[string]walkedDir{}}
	var asker ignoreAsker
	defer asker.close()
	for level := []visit{{dir: ".", trusted: was.Rules == now.Rules}}; len(level) > 0; {
		next, fresh := r.readLevel(level, was, now)
		ignored, err := r.askIgnored(ctx, &asker, fresh)
		if err != nil {
			return nil, err
		}
		for _, child := range fresh {
			parent, name := path.Dir(child), path.Base(child)
			seen := now.Dirs[parent]
			if ignored[child] {
				seen.Ignored = append(seen.Ignored, name)
			} else {
				seen.Children = append(seen.Children, name)
				next = append(next, visit{dir: child})
			}
			now.Dirs[parent] = seen
		}
		level = next
	}
	if raw, err := json.Marshal(now); err == nil && os.WriteFile(r.path(walkName), raw, 0o644) == nil {
		_ = os.Rename(r.path(walkName), filepath.Join(r.State, walkName))
	}
	var found []string
	for dir, seen := range now.Dirs {
		for _, link := range seen.Links {
			found = append(found, path.Join(dir, link))
		}
	}
	slices.Sort(found)
	return found, nil
}

func (r Repo) readLevel(level []visit, was, now walk) (next []visit, fresh []string) {
	for _, at := range level {
		full := filepath.Join(r.Tree, filepath.FromSlash(at.dir))
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		cached, known := was.Dirs[at.dir]
		changed := !known || cached.Changed != info.ModTime().UnixNano()
		stamp := ""
		if changed || cached.Ignore != "" {
			if ignore, err := os.Lstat(filepath.Join(full, ".gitignore")); err == nil {
				stamp = fmt.Sprint(ignore.ModTime().UnixNano(), ignore.Size())
			}
		}
		trusted := at.trusted && known && cached.Ignore == stamp
		if trusted && !changed {
			now.Dirs[at.dir] = cached
			for _, child := range cached.Children {
				next = append(next, visit{dir: path.Join(at.dir, child), trusted: true})
			}
			continue
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			continue
		}
		seen := walkedDir{Changed: info.ModTime().UnixNano(), Ignore: stamp}
		if at.dir != "." && slices.ContainsFunc(entries, func(one fs.DirEntry) bool { return one.Name() == ".git" }) {
			now.Dirs[at.dir] = seen
			continue
		}
		for _, one := range entries {
			name, child := one.Name(), path.Join(at.dir, one.Name())
			switch {
			case one.Type()&fs.ModeIrregular != 0:
				if _, err := os.Readlink(filepath.Join(full, name)); err == nil {
					seen.Links = append(seen.Links, name)
				}
			case !one.IsDir() || name == ".git" || at.dir != "." && (name == platformfs.StateDirName || name == platformfs.LegacyStateDirName):
			case trusted && slices.Contains(cached.Children, name):
				seen.Children = append(seen.Children, name)
				next = append(next, visit{dir: child, trusted: true})
			case trusted && slices.Contains(cached.Ignored, name):
				seen.Ignored = append(seen.Ignored, name)
			default:
				fresh = append(fresh, child)
			}
		}
		now.Dirs[at.dir] = seen
	}
	return next, fresh
}

type ignoreAsker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
}

func (r Repo) askIgnored(ctx context.Context, asker *ignoreAsker, dirs []string) (map[string]bool, error) {
	ignored := map[string]bool{}
	if len(dirs) == 0 {
		return ignored, nil
	}
	if asker.cmd == nil {
		cmd, stderr := r.command(ctx, "", "check-ignore", "-z", "-v", "-n", "--stdin")
		cmd.Stdin, cmd.Env = nil, append(cmd.Env, "GIT_FLUSH=1")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, failed("check-ignore", err, stderr)
		}
		*asker = ignoreAsker{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}
	}
	go func() { _, _ = io.WriteString(asker.stdin, strings.Join(dirs, "\x00")+"\x00") }()
	for range dirs {
		var record [4]string
		for i := range record {
			field, err := asker.stdout.ReadString(0)
			if err != nil {
				return nil, failed("check-ignore", cmp.Or(asker.cmd.Wait(), err), asker.stderr)
			}
			record[i] = strings.TrimSuffix(field, "\x00")
		}
		source, pattern, dir := record[0], record[2], record[3]
		ignored[dir] = source != "" && !strings.HasPrefix(pattern, "!")
	}
	return ignored, nil
}

func (asker *ignoreAsker) close() {
	if asker.cmd != nil {
		_ = asker.stdin.Close()
		_ = asker.cmd.Wait()
	}
}

func (r Repo) exclude(skipped []string) error {
	lines := []string{"/*/**/" + platformfs.StateDirName + "/", "/*/**/" + platformfs.LegacyStateDirName + "/"}
	if own, err := os.ReadFile(filepath.Join(r.Tree, ".git", "info", "exclude")); err == nil {
		lines = append(lines, string(own))
	}
	if inside, err := filepath.Rel(r.Tree, r.State); err == nil && filepath.IsLocal(inside) {
		lines = append(lines, "/"+filepath.ToSlash(inside)+"/")
	}
	escape := strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`)
	for _, path := range skipped {
		lines = append(lines, "/"+escape.Replace(path))
	}
	return os.WriteFile(r.path(excludeName), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func (r Repo) diff(ctx context.Context, shape, from, to string) ([]string, error) {
	out, err := r.git(ctx, "", "diff-tree", "-r", "-z", "--no-renames", shape, from, to)
	return nulSplit(out), err
}

func nulSplit(out []byte) []string {
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 })
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
