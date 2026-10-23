package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
)

type State string

const (
	Running State = "running"
	Exited  State = "exited"
	Killed  State = "killed"
)

const (
	logSuffix          = ".log"
	stateSuffix        = ".json"
	stagingSuffix      = ".writing"
	claimedPrefix      = "bash-"
	highestClaimedFile = "highest-claimed"
	prunedStampFile    = "pruned"
	runningDir         = "running"
	DefaultTail        = 200
	dirMode            = 0o755
	fileMode           = 0o644
	killWait           = 2 * time.Second
)

type Shell struct {
	Name      string     `json:"name"`
	Command   string     `json:"command"`
	Dir       string     `json:"dir"`
	Owner     string     `json:"owner"`
	Session   string     `json:"session,omitempty"`
	Call      string     `json:"call,omitempty"`
	TofuPID   int        `json:"tofu_pid,omitempty"`
	PID       int        `json:"pid"`
	State     State      `json:"state"`
	Started   time.Time  `json:"started"`
	Ended     *time.Time `json:"ended,omitempty"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	Terminal  bool       `json:"terminal,omitempty"`
	Holder    Holder     `json:"holder,omitempty"`
	HeldSince *time.Time `json:"held_since,omitempty"`
	Kept      Kept       `json:"kept,omitempty"`
	Port      int        `json:"port,omitempty"`
	Ready     Readiness  `json:"ready,omitempty"`
	Env       []string   `json:"env,omitempty"`
}

func addedEnv(env []string) []string {
	inherited := os.Environ()
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool { return slices.Contains(inherited, entry) })
}

type Kept string

const (
	KeptBackground Kept = "background"
	KeptMoved      Kept = "moved"
)

type live struct {
	tree     tree
	term     *terminal
	finished chan struct{}
	killed   bool
}

type Lifetime int

const (
	DiesWithTofu Lifetime = iota
	OutlivesTofu
)

type Registry struct {
	Lifetime  Lifetime
	Terminals atomic.Bool
	dir       string
	self      int
	session   string
	mu        sync.Mutex
	running   map[string]*live
	wrote     chan struct{}
}

func OpenAt(dir string) *Registry {
	return &Registry{dir: dir, self: os.Getpid(), running: map[string]*live{}, wrote: make(chan struct{}, 1)}
}

func (r *Registry) ForSession(id string) *Registry {
	opened := OpenAt(r.dir)
	opened.Lifetime, opened.session = r.Lifetime, id
	return opened
}

func (s Shell) LeftOver() bool {
	return s.State == Running && (s.TofuPID <= 0 || !processAlive(s.TofuPID))
}

func (s Shell) OneShot() bool {
	return s.Kept == "" && s.State == Running && !s.LeftOver()
}

func (r *Registry) Own() []Shell {
	found, _ := r.List()
	return slices.DeleteFunc(found, func(one Shell) bool { return one.State != Running || one.TofuPID != r.self || one.Session != r.session })
}

func (r *Registry) endedPath(name string) string { return filepath.Join(r.dir, name+stateSuffix) }
func (r *Registry) runningPath(name string) string {
	return filepath.Join(r.dir, runningDir, name+stateSuffix)
}
func (r *Registry) logPath(name string) string { return filepath.Join(r.dir, name+logSuffix) }

var ErrRunning = errors.New("shell: already running under that name")

type Tree struct{ inner tree }

func StartTracked(cmd *exec.Cmd) (Tree, error) {
	inner, err := startTree(cmd, DiesWithTofu)
	return Tree{inner: inner}, err
}

func (t Tree) Release() { t.inner.release() }

func (r *Registry) reserve(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("shell: a process needs a name")
	}
	if existing, err := r.Read(name); err == nil && existing.State == Running {
		return fmt.Errorf("%w: %q", ErrRunning, name)
	}
	return os.MkdirAll(filepath.Join(r.dir, runningDir), dirMode)
}

func (r *Registry) claim() (string, *os.File, error) {
	if err := os.MkdirAll(filepath.Join(r.dir, runningDir), dirMode); err != nil {
		return "", nil, err
	}
	everClaimed := filepath.Join(r.dir, highestClaimedFile)
	recorded, unrecorded := os.ReadFile(everClaimed)
	highest, _ := strconv.Atoi(string(recorded))
	var files []os.DirEntry
	if unrecorded != nil {
		files, _ = os.ReadDir(r.dir)
	}
	for _, file := range files {
		rest, claimed := strings.CutPrefix(file.Name(), claimedPrefix)
		digits, _, _ := strings.Cut(rest, ".")
		if number, err := strconv.Atoi(digits); claimed && err == nil {
			highest = max(highest, number)
		}
	}
	for number := highest + 1; ; number++ {
		name := claimedPrefix + strconv.Itoa(number)
		logFile, err := os.OpenFile(r.logPath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err == nil {
			_ = os.WriteFile(everClaimed, []byte(strconv.Itoa(number)), fileMode)
		}
		return name, logFile, err
	}
}

func (r *Registry) spawn(cmd *exec.Cmd, command string, logFile *os.File) (*live, <-chan error, bool, error) {
	typed := r.Terminals.Load()
	if typed || len(cmd.Args) == 3 && cmd.Args[1] == "-c" && endsInFilter(command) {
		cmd.Env = append(cmd.Environ(), "PAGER=cat", "GIT_PAGER=cat")
		if typed {
			cmd.Env = append(cmd.Env, "TERM=xterm-256color")
		}
		if spawned, waited, term, err := startConsole(cmd, logFile, r.Lifetime, typed, r.toldIn(logFile)); err == nil {
			return &live{tree: spawned, term: term, finished: make(chan struct{})}, waited, true, nil
		}
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	spawned, err := startTree(cmd, r.Lifetime)
	if err != nil {
		_ = logFile.Close()
		return nil, nil, false, err
	}
	waited, report := make(chan error, 1), r.toldIn(logFile)
	safe.Go(report, "shell wait", func() { waited <- safe.Call(report, "shell wait", cmd.Wait) })
	return &live{tree: spawned, finished: make(chan struct{})}, waited, false, nil
}

func (r *Registry) toldIn(log io.Writer) safe.Report {
	return func(p safe.Panic) {
		_, _ = io.WriteString(log, "\ntofu: "+p.Saved(filepath.Dir(r.dir))+"\n")
	}
}

func (r *Registry) list(entry Shell, process *live) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[entry.Name] = process
	return r.writeLocked(entry)
}

func (r *Registry) keep(entry Shell, process *live, waited <-chan error, logFile *os.File) error {
	err := r.list(entry, process)
	safe.Go(r.toldIn(logFile), "shell await", func() { r.await(waited, logFile, entry, process) })
	return err
}

func (r *Registry) Start(root, name, command, owner string, env ...string) (Shell, error) {
	if err := r.reserve(name); err != nil {
		return Shell{}, err
	}
	choice, err := Resolve("")
	if err != nil {
		return Shell{}, err
	}
	logFile, err := os.Create(r.logPath(name))
	if err != nil {
		return Shell{}, err
	}
	cmd := choice.Command(context.Background(), root, command, env...)
	started := time.Now()
	process, waited, terminal, err := r.spawn(cmd, command, logFile)
	if err != nil {
		return Shell{}, err
	}
	entry := Shell{Name: name, Command: command, Dir: root, Owner: owner, Session: r.session, TofuPID: r.self, PID: cmd.Process.Pid, State: Running, Started: started, Terminal: terminal,
		Kept: KeptBackground, Port: NamedPort(root, command, cmd.Env), Env: env}
	return entry, r.keep(entry, process, waited, logFile)
}

func exitCode(waitErr error) int {
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
		return 0
	case errors.As(waitErr, &exitErr):
		return exitErr.ExitCode()
	}
	return -1
}

func (r *Registry) await(waited <-chan error, logFile *os.File, started Shell, process *live) {
	code := exitCode(<-waited)
	_ = logFile.Close()
	defer close(process.finished)
	r.mu.Lock()
	defer r.mu.Unlock()
	process.tree.release()
	if r.running[started.Name] == process {
		delete(r.running, started.Name)
	}
	if started.Kept == "" {
		_ = os.Remove(r.logPath(started.Name))
	}
	if process.killed {
		return
	}
	if raw, err := os.ReadFile(r.runningPath(started.Name)); err == nil {
		_ = json.Unmarshal(raw, &started)
	}
	ended := time.Now()
	started.State, started.Ended, started.ExitCode = Exited, &ended, &code
	_ = r.writeLocked(started)
}

func (r *Registry) Prune() error {
	files, err := os.ReadDir(r.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	kept := time.Now().Add(-konst.FinishedShellKeptHours * time.Hour)
	for _, file := range files {
		name, isState := strings.CutSuffix(file.Name(), stateSuffix)
		if !isState {
			continue
		}
		if entry, readErr := stateAt(r.endedPath(name)); readErr == nil && entry.State != Running && (entry.Ended == nil || entry.Ended.Before(kept)) {
			r.removeEnded(name)
		}
	}
	return errors.Join(err, os.WriteFile(filepath.Join(r.dir, prunedStampFile), []byte(time.Now().Format(time.RFC3339)), fileMode))
}

func (r *Registry) removeEnded(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := os.Stat(r.runningPath(name)); err == nil {
		return
	}
	_ = os.Remove(r.endedPath(name))
	_ = os.Remove(r.logPath(name))
}

func (r *Registry) PruneEvery(every time.Duration) error {
	if info, err := os.Stat(filepath.Join(r.dir, prunedStampFile)); err == nil && time.Since(info.ModTime()) < every {
		return nil
	}
	return r.Prune()
}

func (r *Registry) writeLocked(entry Shell) error {
	path, other := r.runningPath(entry.Name), r.endedPath(entry.Name)
	if entry.State != Running {
		entry.Holder, entry.HeldSince = "", nil
		path, other = other, path
	}
	if entry.State != Running && entry.Kept == "" {
		_ = os.Remove(r.logPath(entry.Name))
		return errors.Join(removed(path), removed(other))
	}
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	staging := path + stagingSuffix
	if err := os.WriteFile(staging, raw, fileMode); err != nil {
		return err
	}
	if err := os.Rename(staging, path); err != nil {
		return err
	}
	return removed(other)
}

func removed(path string) error {
	if err := os.Remove(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (r *Registry) Read(name string) (Shell, error) {
	r.mu.Lock()
	entry, err := r.readLocked(name)
	_, awaitedHere := r.running[name]
	r.mu.Unlock()
	if err != nil || entry.State != Running || awaitedHere || treeAlive(entry.PID) {
		return entry, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, err = r.readLocked(name); err != nil || entry.State != Running || r.running[name] != nil {
		return entry, err
	}
	ended := time.Now()
	entry.State, entry.Ended = Exited, &ended
	return entry, r.writeLocked(entry)
}

func (r *Registry) readLocked(name string) (Shell, error) {
	entry, err := stateAt(r.runningPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return stateAt(r.endedPath(name))
	}
	return entry, err
}

func stateAt(path string) (Shell, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Shell{}, err
	}
	var entry Shell
	err = json.Unmarshal(raw, &entry)
	return entry, err
}

func (r *Registry) List() ([]Shell, error) {
	files, err := os.ReadDir(filepath.Join(r.dir, runningDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	shells := make([]Shell, 0, len(files))
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), stateSuffix) {
			continue
		}
		entry, err := r.Read(strings.TrimSuffix(file.Name(), stateSuffix))
		if err != nil || entry.State != Running {
			continue
		}
		entry.Env = nil
		shells = append(shells, entry)
	}
	sort.Slice(shells, func(i, j int) bool { return shells[i].Started.Before(shells[j].Started) })
	return shells, nil
}

func (r *Registry) AwaitEnd(ctx context.Context, name string, within time.Duration) (Shell, error) {
	poll := time.NewTicker(konst.ReadyPollMillis * time.Millisecond)
	defer poll.Stop()
	gaveUp := time.After(within)
	for {
		entry, err := r.Read(name)
		if err != nil || entry.State != Running || entry.HeldByPerson() {
			return entry, err
		}
		select {
		case <-poll.C:
		case <-gaveUp:
			return entry, nil
		case <-ctx.Done():
			return entry, nil
		}
	}
}
