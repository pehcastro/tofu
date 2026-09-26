package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

func posixShell() (string, error) {
	for _, name := range []string{"bash", "sh"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("shell: no sh or bash on PATH")
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

func (r *Registry) Start(root, name, command, owner string) (Shell, error) {
	if err := r.reserve(name); err != nil {
		return Shell{}, err
	}
	shell, err := posixShell()
	if err != nil {
		return Shell{}, err
	}
	logFile, err := os.Create(r.logPath(name))
	if err != nil {
		return Shell{}, err
	}
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = logFile, logFile
	spawned, err := startTree(cmd, r.Lifetime)
	if err != nil {
		_ = logFile.Close()
		return Shell{}, err
	}
	entry := Shell{Name: name, Command: command, Dir: root, Owner: owner, PID: cmd.Process.Pid, State: Running, Started: time.Now()}
	process := &live{tree: spawned, finished: make(chan struct{})}
	r.mu.Lock()
	writeErr := r.writeLocked(entry)
	if writeErr == nil {
		r.running[name] = process
	}
	r.mu.Unlock()
	if writeErr != nil {
		_ = logFile.Close()
		return Shell{}, writeErr
	}
	go r.await(cmd, logFile, entry, process)
	return entry, nil
}

func (r *Registry) await(cmd *exec.Cmd, logFile *os.File, started Shell, process *live) {
	waitErr := cmd.Wait()
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
	code := 0
	if waitErr != nil {
		code = -1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	started.State, started.Ended, started.ExitCode = Exited, &ended, &code
	_ = r.writeLocked(started)
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

type Watch struct {
	r    *Registry
	name string
	log  *os.File
}

func (r *Registry) Watch(name, command, dir, owner string) (*Watch, error) {
	if err := r.reserve(name); err != nil {
		return nil, err
	}
	logFile, err := os.Create(r.logPath(name))
	if err != nil {
		return nil, err
	}
	entry := Shell{Name: name, Command: command, Dir: dir, Owner: owner, State: Running, Started: time.Now()}
	r.mu.Lock()
	writeErr := r.writeLocked(entry)
	if writeErr == nil {
		r.running[name] = &live{finished: make(chan struct{})}
	}
	r.mu.Unlock()
	if writeErr != nil {
		_ = logFile.Close()
		return nil, writeErr
	}
	return &Watch{r: r, name: name, log: logFile}, nil
}

func (w *Watch) Writer() *os.File { return w.log }

func (w *Watch) SetPID(pid int) error {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	entry, err := w.r.readLocked(w.name)
	if err != nil {
		return err
	}
	entry.PID = pid
	return w.r.writeLocked(entry)
}

func (w *Watch) Finish(code int) error { return w.conclude(Exited, &code) }

func (w *Watch) Killed() error { return w.conclude(Killed, nil) }

func (w *Watch) conclude(state State, code *int) error {
	_ = w.log.Close()
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	entry, err := w.r.readLocked(w.name)
	if process, ok := w.r.running[w.name]; ok {
		close(process.finished)
		delete(w.r.running, w.name)
	}
	if err != nil {
		return err
	}
	if entry.State != Running {
		return nil
	}
	ended := time.Now()
	entry.State, entry.Ended, entry.ExitCode = state, &ended, code
	return w.r.writeLocked(entry)
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
