package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Running State = "running"
	Exited  State = "exited"
	Killed  State = "killed"
)

const (
	logSuffix     = ".log"
	stateSuffix   = ".json"
	stagingSuffix = ".writing"
	claimedPrefix = "bash-"
	DefaultTail   = 200
	dirMode       = 0o755
	fileMode      = 0o644
	killWait      = 2 * time.Second
)

type Shell struct {
	Name     string     `json:"name"`
	Command  string     `json:"command"`
	Dir      string     `json:"dir"`
	Owner    string     `json:"owner"`
	TofuPID  int        `json:"tofu_pid,omitempty"`
	PID      int        `json:"pid"`
	State    State      `json:"state"`
	Started  time.Time  `json:"started"`
	Ended    *time.Time `json:"ended,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
}

type live struct {
	tree     tree
	finished chan struct{}
	killed   bool
}

type Lifetime int

const (
	DiesWithTofu Lifetime = iota
	OutlivesTofu
)

type Registry struct {
	Lifetime Lifetime
	dir      string
	self     int
	mu       sync.Mutex
	running  map[string]*live
}

func OpenAt(dir string) *Registry {
	return &Registry{dir: dir, self: os.Getpid(), running: map[string]*live{}}
}

func (s Shell) LeftOver() bool {
	return s.State == Running && (s.TofuPID <= 0 || !processAlive(s.TofuPID))
}

func (r *Registry) Own() []Shell {
	found, _ := r.List()
	return slices.DeleteFunc(found, func(one Shell) bool { return one.State != Running || one.TofuPID != r.self })
}

func (r *Registry) statePath(name string) string { return filepath.Join(r.dir, name+stateSuffix) }
func (r *Registry) logPath(name string) string   { return filepath.Join(r.dir, name+logSuffix) }

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
	return os.MkdirAll(r.dir, dirMode)
}

func (r *Registry) claim() (string, *os.File, error) {
	if err := os.MkdirAll(r.dir, dirMode); err != nil {
		return "", nil, err
	}
	for number := 1; ; number++ {
		name := claimedPrefix + strconv.Itoa(number)
		if exists(r.statePath(name)) {
			continue
		}
		logFile, err := os.OpenFile(r.logPath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return name, logFile, err
	}
}

func (r *Registry) spawn(cmd *exec.Cmd, logFile *os.File) (tree, <-chan error, error) {
	cmd.Stdout, cmd.Stderr = logFile, logFile
	spawned, err := startTree(cmd, r.Lifetime)
	if err != nil {
		_ = logFile.Close()
		return tree{}, nil, err
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	return spawned, waited, nil
}

func (r *Registry) keep(entry Shell, spawned tree, waited <-chan error, logFile *os.File) error {
	process := &live{tree: spawned, finished: make(chan struct{})}
	r.mu.Lock()
	r.running[entry.Name] = process
	err := r.writeLocked(entry)
	r.mu.Unlock()
	go r.await(waited, logFile, entry, process)
	return err
}

func (r *Registry) Start(root, name, command, owner string) (Shell, error) {
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
	cmd := exec.Command(choice.Path, "-c", command)
	cmd.Dir = root
	spawned, waited, err := r.spawn(cmd, logFile)
	if err != nil {
		return Shell{}, err
	}
	entry := Shell{Name: name, Command: command, Dir: root, Owner: owner, TofuPID: r.self, PID: cmd.Process.Pid, State: Running, Started: time.Now()}
	return entry, r.keep(entry, spawned, waited, logFile)
}

func (r *Registry) Yield(ctx context.Context, cmd *exec.Cmd, command, owner string, within time.Duration) (Shell, string, error) {
	got, err := r.YieldReady(ctx, cmd, command, owner, Wait{Within: within, Poll: within})
	return got.Shell, got.Output, err
}

type Readiness string

const (
	ReadyExited  Readiness = "exited"
	ReadyPort    Readiness = "its port opened"
	ReadyLine    Readiness = "it printed a ready line"
	ReadyWaited  Readiness = "the wait ran out"
	ReadyStopped Readiness = "the turn stopped"
)

type Wait struct {
	Within time.Duration
	Poll   time.Duration
	Port   int
}

type Yielded struct {
	Shell  Shell
	Output string
	Ready  Readiness
	Took   time.Duration
}

func (r *Registry) YieldReady(ctx context.Context, cmd *exec.Cmd, command, owner string, wait Wait) (Yielded, error) {
	if wait.Port > 0 {
		lookup, cancel := context.WithTimeout(ctx, wait.Within)
		defer cancel()
		if err := r.refuseHeld(lookup, wait.Port, wait.Poll); err != nil {
			return Yielded{}, err
		}
	}
	name, logFile, err := r.claim()
	if err != nil {
		return Yielded{}, err
	}
	spawned, waited, err := r.spawn(cmd, logFile)
	if err != nil {
		_ = os.Remove(r.logPath(name))
		return Yielded{}, err
	}
	got := Yielded{Shell: Shell{Name: name, Command: command, Dir: cmd.Dir, Owner: owner, TofuPID: r.self, PID: cmd.Process.Pid, State: Running, Started: time.Now()}}
	poll := time.NewTicker(wait.Poll)
	defer poll.Stop()
	gaveUp := time.After(wait.Within)
	for got.Ready == "" {
		select {
		case waitErr := <-waited:
			_ = logFile.Close()
			spawned.release()
			output, readErr := os.ReadFile(r.logPath(name))
			_ = os.Remove(r.logPath(name))
			ended, code := time.Now(), exitCode(waitErr)
			got.Shell.State, got.Shell.Ended, got.Shell.ExitCode = Exited, &ended, &code
			got.Output, got.Ready, got.Took = string(output), ReadyExited, time.Since(got.Shell.Started)
			return got, readErr
		case <-gaveUp:
			got.Ready = ReadyWaited
		case <-ctx.Done():
			got.Ready = ReadyStopped
		case <-poll.C:
			got.Ready = r.readiness(name, wait)
		}
	}
	got.Took = time.Since(got.Shell.Started)
	err = r.keep(got.Shell, spawned, waited, logFile)
	output, _ := os.ReadFile(r.logPath(name))
	got.Output = string(output)
	return got, err
}

func (r *Registry) readiness(name string, wait Wait) Readiness {
	output, _ := os.ReadFile(r.logPath(name))
	if regexp.MustCompile(`(?i)\b(?:listening|ready|started server)\b|http://`).Match(output) {
		return ReadyLine
	}
	if wait.Port > 0 && slices.ContainsFunc(dialLoopbacks(wait.Port, wait.Poll), func(one Address) bool { return one.Open }) {
		return ReadyPort
	}
	return ""
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
	if process.killed {
		return
	}
	ended := time.Now()
	started.State, started.Ended, started.ExitCode = Exited, &ended, &code
	_ = r.writeLocked(started)
}

func (r *Registry) Prune() error {
	found, err := r.List()
	for _, one := range found {
		if one.State != Running {
			_ = os.Remove(r.statePath(one.Name))
			_ = os.Remove(r.logPath(one.Name))
		}
	}
	return err
}

func (r *Registry) writeLocked(entry Shell) error {
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	path := r.statePath(entry.Name)
	staging := path + stagingSuffix
	if err := os.WriteFile(staging, raw, fileMode); err != nil {
		return err
	}
	return os.Rename(staging, path)
}

func (r *Registry) Read(name string) (Shell, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readLocked(name)
}

func (r *Registry) readLocked(name string) (Shell, error) {
	raw, err := os.ReadFile(r.statePath(name))
	if err != nil {
		return Shell{}, err
	}
	var entry Shell
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Shell{}, err
	}
	if entry.State != Running {
		return entry, nil
	}
	if _, awaitedHere := r.running[entry.Name]; awaitedHere || treeAlive(entry.PID) {
		return entry, nil
	}
	ended := time.Now()
	entry.State, entry.Ended = Exited, &ended
	if err := r.writeLocked(entry); err != nil {
		return Shell{}, err
	}
	return entry, nil
}

func (r *Registry) List() ([]Shell, error) {
	files, err := os.ReadDir(r.dir)
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
		if err != nil {
			continue
		}
		shells = append(shells, entry)
	}
	sort.Slice(shells, func(i, j int) bool { return shells[i].Started.Before(shells[j].Started) })
	return shells, nil
}

func (r *Registry) Tail(name string, lines int) (string, error) {
	if _, err := r.Read(name); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(r.logPath(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", nil
	}
	all := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n"), nil
}

var ErrNotRunning = errors.New("shell: not running")

var ErrTreeGone = errors.New("shell: the process tree is already gone")

func (r *Registry) Kill(name string) error {
	process, err := r.terminate(name)
	if err != nil {
		return err
	}
	if process == nil {
		return nil
	}
	select {
	case <-process.finished:
	case <-time.After(killWait):
	}
	return nil
}

func (r *Registry) Restart(name string) (Shell, error) {
	entry, err := r.Read(name)
	if err != nil {
		return Shell{}, err
	}
	if err := r.Kill(name); err != nil && !errors.Is(err, ErrNotRunning) {
		return Shell{}, err
	}
	return r.Start(entry.Dir, name, entry.Command, entry.Owner)
}

func (r *Registry) Owning(command string) []Shell {
	fields := strings.Fields(command)
	if len(fields) < 2 || !slices.Contains([]string{"kill", "taskkill", "pkill"}, strings.TrimSuffix(filepath.Base(fields[0]), ".exe")) {
		return nil
	}
	var pids []int
	for _, field := range fields[1:] {
		pid, err := strconv.Atoi(field)
		switch {
		case err == nil && pid > 0:
			pids = append(pids, pid)
		case !strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "/"):
			return nil
		}
	}
	listed, _ := r.List()
	var owned []Shell
	for _, pid := range pids {
		at := slices.IndexFunc(listed, func(one Shell) bool { return one.State == Running && treeHas(one.PID, pid) })
		if at < 0 {
			return nil
		}
		if !slices.ContainsFunc(owned, func(one Shell) bool { return one.Name == listed[at].Name }) {
			owned = append(owned, listed[at])
		}
	}
	return owned
}

func (r *Registry) terminate(name string) (*live, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, err := r.readLocked(name)
	if err != nil {
		return nil, err
	}
	if entry.State != Running {
		return nil, fmt.Errorf("%w: %q", ErrNotRunning, name)
	}
	process := r.running[name]
	if process != nil {
		process.killed = true
	}
	if err := killTree(entry.PID); err != nil {
		return nil, err
	}
	ended := time.Now()
	entry.State, entry.Ended = Killed, &ended
	return process, r.writeLocked(entry)
}
