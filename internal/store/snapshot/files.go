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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/platform/outside"
)

const (
	gitDirName           = "shadow"
	indexName            = "index"
	excludeName          = "exclude"
	walkName             = "walk"
	snapshotLockName     = "snapshot.lock"
	largestKeptFileBytes = 10 << 20
	packEveryBlob        = "core.bigFileThreshold=1"
	fastestCompression   = "core.compression=1"
	verbatimAttributes   = "* -text -filter -ident -working-tree-encoding\n"
	alwaysExcluded       = "node_modules/\n.venv/\n__pycache__/\n/*/**/" + platformfs.StateDirName + "/\n/*/**/" + platformfs.LegacyStateDirName + "/\n"
)

var ErrNoGit = errors.New("undo needs git on PATH, and there is none")

type Files struct {
	State string
	Tree  string
}

func (f Files) Dir() string { return filepath.Join(f.State, gitDirName) }

func (f Files) path(name string) string { return filepath.Join(f.Dir(), name) }

func (f Files) Snapshot(ctx context.Context) (string, error) {
	var tree string
	err := f.held(ctx, func() (err error) {
		tree, err = f.track(ctx)
		return err
	})
	return tree, err
}

func (f Files) held(ctx context.Context, work func() error) error {
	if err := os.MkdirAll(f.Dir(), 0o700); err != nil {
		return err
	}
	return hold(ctx, f.path(snapshotLockName), konst.UndoSnapshotTimeoutMillis*time.Millisecond, func() error {
		if err := f.ensure(ctx); err != nil {
			return err
		}
		return work()
	})
}

func hold(ctx context.Context, lock string, lifetime time.Duration, work func() error) error {
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
		if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > lifetime {
			_ = os.Remove(lock)
		} else if time.Now().After(deadline) {
			return LedgerHeld{Lock: lock}
		} else if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (f Files) ensure(ctx context.Context) error {
	if _, err := os.Stat(f.path("HEAD")); errors.Is(err, fs.ErrNotExist) {
		if _, err := f.Git(ctx, "", "init", "--quiet", "--bare"); err != nil {
			return err
		}
		if err := f.stampPruned(); err != nil {
			return err
		}
	}
	attributes := filepath.Join(f.Dir(), "info", "attributes")
	if _, err := os.Stat(attributes); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, writtenBeforeTheAttributes := range []string{f.path(indexName), filepath.Join(f.Dir(), "info", "exclude")} {
		if err := os.Remove(writtenBeforeTheAttributes); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(attributes), 0o700); err != nil {
		return err
	}
	return os.WriteFile(attributes, []byte(verbatimAttributes), 0o644)
}

func (f Files) prunedStamp() string {
	return filepath.Join(f.Dir(), "objects", "info", prunedStampName)
}

func (f Files) stampPruned() error {
	return os.WriteFile(f.prunedStamp(), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644)
}

func (f Files) Git(ctx context.Context, stdin string, args ...string) (string, error) {
	return f.gitIndexed(ctx, f.path(indexName), stdin, args...)
}

func (f Files) gitIndexed(ctx context.Context, index, stdin string, args ...string) (string, error) {
	cmd, stderr := f.command(ctx, index, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	return string(out), f.failed(args, err, stderr)
}

func (f Files) command(ctx context.Context, index string, args ...string) (*outside.Cmd, *bytes.Buffer) {
	flags := []string{"--git-dir", f.Dir(), "-c", "core.autocrlf=false", "-c", "core.safecrlf=false", "-c", "core.fsmonitor=false", "-c", "core.longpaths=true",
		"-c", "core.quotepath=false", "-c", "core.logAllRefUpdates=false", "-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "core.excludesFile=" + f.path(excludeName)}
	if args[0] != "init" {
		flags = append(flags, "--work-tree", f.Tree)
	}
	cmd := outside.Command(ctx, "git", append(flags, args...)...)
	cmd.Dir, cmd.WaitDelay = f.Tree, konst.BashWaitDelayMillis*time.Millisecond
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(strings.ToUpper(variable), "GIT_") }),
		"GIT_INDEX_FILE="+index, "GIT_AUTHOR_NAME=tofu", "GIT_AUTHOR_EMAIL=tofu@localhost", "GIT_COMMITTER_NAME=tofu", "GIT_COMMITTER_EMAIL=tofu@localhost")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd, &stderr
}

func (f Files) failed(args []string, err error, stderr *bytes.Buffer) error {
	if errors.Is(err, exec.ErrNotFound) {
		return ErrNoGit
	}
	if err != nil {
		verb := args[slices.IndexFunc(args, func(arg string) bool { return !strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") })]
		return fmt.Errorf("git %s in the file store %s: %w: %s", verb, f.Dir(), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (f Files) track(ctx context.Context) (string, error) {
	index := f.path(indexName)
	if err := os.Remove(index + ".lock"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := f.exclude(nil); err != nil {
		return "", err
	}
	walked, err := f.walk(ctx)
	if err != nil {
		return "", err
	}
	if err := f.exclude(walked.skipped()); err != nil {
		return "", err
	}
	if _, missing := os.Stat(index); errors.Is(missing, fs.ErrNotExist) {
		err = f.addInParts(ctx, walked.Dirs)
	} else {
		_, err = f.Git(ctx, "", "add", "--all")
	}
	if err != nil {
		return "", err
	}
	return f.writeTree(ctx)
}

func (f Files) addInParts(ctx context.Context, dirs map[string]walkedDir) error {
	rules, err := os.ReadFile(f.path(excludeName))
	if err != nil {
		return err
	}
	shares := shareOut(dirs, runtime.NumCPU())
	parts := len(shares)
	indexes, failures := make([][]byte, parts), make([]error, parts)
	var running sync.WaitGroup
	for part := range parts {
		var hiddenFiles, hiddenDirs []string
		for other, share := range shares {
			if other == part {
				continue
			}
			for _, one := range share {
				if !one.filesOnly {
					hiddenDirs = append(hiddenDirs, anchored(one.dir)+"/")
					continue
				}
				inside := ""
				if one.dir != "." {
					inside = anchored(one.dir)
				}
				hiddenFiles = append(hiddenFiles, inside+"/*")
				for _, child := range dirs[one.dir].Children {
					hiddenFiles = append(hiddenFiles, "!"+anchored(path.Join(one.dir, child))+"/")
				}
			}
		}
		hidden := append(slices.Clone(rules), strings.Join(slices.Concat(hiddenFiles, hiddenDirs, []string{""}), "\n")...)
		excludes, index := f.path(excludeName+"-"+strconv.Itoa(part)), f.path(indexName+"-"+strconv.Itoa(part))
		running.Go(func() {
			defer func() { _, _ = os.Remove(excludes), os.Remove(index) }()
			if failures[part] = os.WriteFile(excludes, hidden, 0o644); failures[part] != nil {
				return
			}
			if _, failures[part] = f.gitIndexed(ctx, index, "", "-c", "core.excludesFile="+excludes, "-c", packEveryBlob, "-c", fastestCompression, "-c", "index.version=2", "add", "--all"); failures[part] != nil {
				return
			}
			if raw, err := os.ReadFile(index); !errors.Is(err, fs.ErrNotExist) {
				indexes[part], failures[part] = raw, err
			}
		})
	}
	running.Wait()
	if err := errors.Join(failures...); err != nil {
		return err
	}
	joined, ok := joinIndexes(indexes)
	if !ok {
		_, err := f.Git(ctx, "", "-c", packEveryBlob, "add", "--all")
		return err
	}
	if err := os.WriteFile(f.path(indexName+".joined"), joined, 0o644); err != nil {
		return err
	}
	return os.Rename(f.path(indexName+".joined"), f.path(indexName))
}

type addShare struct {
	dir       string
	filesOnly bool
	weight    int
}

func shareOut(dirs map[string]walkedDir, parts int) [][]addShare {
	weights := map[string]int{}
	var weigh func(dir string) int
	weigh = func(dir string) int {
		weights[dir] = dirs[dir].Files
		for _, child := range dirs[dir].Children {
			weights[dir] += weigh(path.Join(dir, child))
		}
		return weights[dir]
	}
	most := max(1, weigh(".")/parts)
	var units []addShare
	var split func(dir string)
	split = func(dir string) {
		units = append(units, addShare{dir: dir, filesOnly: true, weight: dirs[dir].Files})
		for _, child := range dirs[dir].Children {
			if inside := path.Join(dir, child); weights[inside] > most {
				split(inside)
			} else {
				units = append(units, addShare{dir: inside, weight: weights[inside]})
			}
		}
	}
	split(".")
	slices.SortStableFunc(units, func(a, b addShare) int { return cmp.Compare(b.weight, a.weight) })
	shares, loads := make([][]addShare, min(parts, len(units))), make([]int, min(parts, len(units)))
	for _, one := range units {
		least := 0
		for i := range shares {
			if loads[i] < loads[least] || loads[i] == loads[least] && len(shares[i]) < len(shares[least]) {
				least = i
			}
		}
		shares[least], loads[least] = append(shares[least], one), loads[least]+one.weight
	}
	return shares
}

func anchored(path string) string {
	return "/" + strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`).Replace(path)
}

func (f Files) exclude(skipped []string) error {
	lines := []string{alwaysExcluded}
	if own, err := os.ReadFile(filepath.Join(f.Tree, ".git", "info", "exclude")); err == nil {
		lines = append(lines, string(own))
	}
	if inside, err := filepath.Rel(f.Tree, f.State); err == nil && filepath.IsLocal(inside) {
		lines = append(lines, "/"+filepath.ToSlash(inside)+"/")
	}
	for _, path := range skipped {
		lines = append(lines, anchored(path))
	}
	return os.WriteFile(f.path(excludeName), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

type walkedDir struct {
	Changed  int64    `json:"changed"`
	Files    int      `json:"files,omitempty"`
	Ignore   string   `json:"ignore,omitempty"`
	Children []string `json:"children,omitempty"`
	Ignored  []string `json:"ignored,omitempty"`
	Links    []string `json:"links,omitempty"`
	Large    []string `json:"large,omitempty"`
}

type walk struct {
	Rules string               `json:"rules"`
	Dirs  map[string]walkedDir `json:"dirs"`
}

type visit struct {
	dir     string
	trusted bool
}

func (f Files) walk(ctx context.Context) (walk, error) {
	rules, err := os.ReadFile(f.path(excludeName))
	if err != nil {
		return walk{}, err
	}
	var was walk
	if raw, err := os.ReadFile(f.path(walkName)); err == nil {
		_ = json.Unmarshal(raw, &was)
	}
	now := walk{Rules: string(rules), Dirs: map[string]walkedDir{}}
	var asker ignoreAsker
	defer asker.close()
	for level := []visit{{dir: ".", trusted: was.Rules == now.Rules}}; len(level) > 0; {
		next, fresh := f.readLevel(level, was, now)
		ignored, err := f.askIgnored(ctx, &asker, fresh)
		if err != nil {
			return walk{}, err
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
	if raw, err := json.Marshal(now); err == nil {
		_ = os.WriteFile(f.path(walkName), raw, 0o644)
	}
	return now, nil
}

func (now walk) skipped() []string {
	var found []string
	for dir, seen := range now.Dirs {
		for _, skipped := range slices.Concat(seen.Links, seen.Large) {
			found = append(found, path.Join(dir, skipped))
		}
	}
	slices.Sort(found)
	return found
}

func (f Files) readLevel(level []visit, was, now walk) (next []visit, fresh []string) {
	for _, at := range level {
		full := filepath.Join(f.Tree, filepath.FromSlash(at.dir))
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
			case !one.IsDir():
				seen.Files++
				if info, err := one.Info(); err == nil && info.Size() > largestKeptFileBytes {
					seen.Large = append(seen.Large, name)
				}
			case name == ".git" || at.dir != "." && (name == platformfs.StateDirName || name == platformfs.LegacyStateDirName):
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
	cmd    *outside.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
}

func (f Files) askIgnored(ctx context.Context, asker *ignoreAsker, dirs []string) (map[string]bool, error) {
	ignored := map[string]bool{}
	if len(dirs) == 0 {
		return ignored, nil
	}
	defer outside.Wait(ctx, outside.Process)()
	args := []string{"check-ignore", "-z", "-v", "-n", "--stdin"}
	if asker.cmd == nil {
		cmd, stderr := f.command(ctx, f.path(indexName), args...)
		cmd.Env = append(cmd.Env, "GIT_FLUSH=1")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, f.failed(args, err, stderr)
		}
		*asker = ignoreAsker{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}
	}
	go func() { _, _ = io.WriteString(asker.stdin, strings.Join(dirs, "\x00")+"\x00") }()
	for range dirs {
		var record [4]string
		for i := range record {
			field, err := asker.stdout.ReadString(0)
			if err != nil {
				return nil, f.failed(args, cmp.Or(asker.cmd.Wait(), err), asker.stderr)
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

func nulSplit(out string) []string {
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 })
}
