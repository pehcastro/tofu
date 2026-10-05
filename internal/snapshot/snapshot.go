package snapshot

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	largestUntrackedBytes = 10 << 20
	gitDirName            = "undo.git"
	ledgerName            = "undo.jsonl"
	verbatimAttributes    = "* -text -filter -ident -working-tree-encoding\n"
	driftedWhy            = "changed after the turn ended, so it was left as it is: --force puts it back anyway"
)

var ErrNoGit = errors.New("undo needs git on PATH, and there is none")

type Repo struct {
	Dir  string
	Tree string
}

type Ask struct {
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

type entry struct {
	Turn  string `json:"turn,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func (r Repo) Begin(ctx context.Context, turn string) error {
	tree, err := r.track(ctx)
	if err != nil {
		return err
	}
	return r.append(entry{Turn: turn, Start: tree})
}

func (r Repo) End(ctx context.Context) error {
	tree, err := r.track(ctx)
	if err != nil {
		return err
	}
	return r.append(entry{End: tree})
}

func (r Repo) Undo(ctx context.Context, ask Ask) (Report, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Report{}, ErrNoGit
	}
	entries, err := r.entries()
	if err != nil {
		return Report{}, err
	}
	if ask.Turns > len(entries) {
		return Report{}, NothingRecorded{Asked: ask.Turns, Recorded: len(entries)}
	}
	kept, undone := entries[:len(entries)-ask.Turns], entries[len(entries)-ask.Turns:]
	last := undone[len(undone)-1]
	if last.End == "" && !ask.Force {
		return Report{}, StillRunning{Turn: last.Turn}
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

func (r Repo) gitDir() string { return filepath.Join(r.Dir, gitDirName) }

func (r Repo) git(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.autocrlf=false", "-c", "core.fsmonitor=false", "-c", "core.longpaths=true",
		"-c", "core.quotepath=false", "--git-dir", r.gitDir(), "--work-tree", r.Tree}, args...)...)
	cmd.Dir, cmd.Stdin = r.Tree, strings.NewReader(stdin)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(strings.ToUpper(variable), "GIT_") })
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, ErrNoGit
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (r Repo) track(ctx context.Context) (string, error) {
	if _, err := os.Stat(r.gitDir()); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(r.gitDir(), 0o755); err != nil {
			return "", err
		}
		if _, err := r.git(ctx, "", "init", "--quiet"); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(r.gitDir(), "info", "attributes"), []byte(verbatimAttributes), 0o644); err != nil {
			return "", err
		}
	}
	if err := r.exclude(nil); err != nil {
		return "", err
	}
	untracked, err := r.git(ctx, "", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	var large []string
	for _, path := range nulSplit(untracked) {
		if info, err := os.Lstat(filepath.Join(r.Tree, filepath.FromSlash(path))); err == nil && info.Size() > largestUntrackedBytes {
			large = append(large, path)
		}
	}
	if err := r.exclude(large); err != nil {
		return "", err
	}
	if _, err := r.git(ctx, "", "add", "--all"); err != nil {
		return "", err
	}
	tree, err := r.git(ctx, "", "write-tree")
	return strings.TrimSpace(string(tree)), err
}

func (r Repo) exclude(large []string) error {
	var lines []string
	if own, err := os.ReadFile(filepath.Join(r.Tree, ".git", "info", "exclude")); err == nil {
		lines = append(lines, string(own))
	}
	if inside, err := filepath.Rel(r.Tree, filepath.Dir(r.Dir)); err == nil && filepath.IsLocal(inside) {
		lines = append(lines, "/"+filepath.ToSlash(inside)+"/")
	}
	escape := strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`)
	for _, path := range large {
		lines = append(lines, "/"+escape.Replace(path))
	}
	return os.WriteFile(filepath.Join(r.gitDir(), "info", "exclude"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
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
	file, err := os.OpenFile(filepath.Join(r.Dir, ledgerName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	return errors.Join(json.NewEncoder(file).Encode(line), file.Close())
}

func (r Repo) entries() ([]entry, error) {
	raw, err := os.ReadFile(filepath.Join(r.Dir, ledgerName))
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
	path := filepath.Join(r.Dir, ledgerName)
	if err := os.WriteFile(path+".tmp", lines.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
