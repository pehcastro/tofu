package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	mu       sync.Mutex
	running  map[string]*live
}

func OpenAt(dir string) *Registry {
	return &Registry{dir: dir, running: map[string]*live{}}
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
	entry := Shell{Name: name, Command: command, Dir: root, Owner: owner, PID: cmd.Process.Pid, State: Running, Started: time.Now()}
	return entry, r.keep(entry, spawned, waited, logFile)
}

func (r *Registry) Yield(ctx context.Context, cmd *exec.Cmd, command, owner string, within time.Duration) (Shell, string, error) {
	name, logFile, err := r.claim()
	if err != nil {
		return Shell{}, "", err
	}
	spawned, waited, err := r.spawn(cmd, logFile)
	if err != nil {
		_ = os.Remove(r.logPath(name))
		return Shell{}, "", err
	}
	entry := Shell{Name: name, Command: command, Dir: cmd.Dir, Owner: owner, PID: cmd.Process.Pid, State: Running, Started: time.Now()}
	select {
	case waitErr := <-waited:
		_ = logFile.Close()
		spawned.release()
		output, readErr := os.ReadFile(r.logPath(name))
		_ = os.Remove(r.logPath(name))
		ended, code := time.Now(), exitCode(waitErr)
		entry.State, entry.Ended, entry.ExitCode = Exited, &ended, &code
		return entry, string(output), readErr
	case <-time.After(within):
	case <-ctx.Done():
	}
	err = r.keep(entry, spawned, waited, logFile)
	output, _ := os.ReadFile(r.logPath(name))
	return entry, string(output), err
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
	delete(r.running, started.Name)
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
	all := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if all[0] == "" {
		return "", nil
	}
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
