package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
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
	DefaultTail        = 200
	dirMode            = 0o755
	fileMode           = 0o644
	killWait           = 2 * time.Second
)

type Shell struct {
	Name     string     `json:"name"`
	Command  string     `json:"command"`
	Dir      string     `json:"dir"`
	Owner    string     `json:"owner"`
	Call     string     `json:"call,omitempty"`
	TofuPID  int        `json:"tofu_pid,omitempty"`
	PID      int        `json:"pid"`
	State    State      `json:"state"`
	Started  time.Time  `json:"started"`
	Ended    *time.Time `json:"ended,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
	Terminal bool       `json:"terminal,omitempty"`
	Deadline int64      `json:"deadline_ms,omitempty"`
	Kept     Kept       `json:"kept,omitempty"`
	Port     int        `json:"port,omitempty"`
	Ready    Readiness  `json:"ready,omitempty"`
	Env      []string   `json:"env,omitempty"`
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

func (s Shell) OneShot() bool {
	return s.Kept == "" && s.State == Running && !s.LeftOver()
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
	files, err := os.ReadDir(r.dir)
	if err != nil {
		return "", nil, err
	}
	everClaimed := filepath.Join(r.dir, highestClaimedFile)
	recorded, _ := os.ReadFile(everClaimed)
	highest, _ := strconv.Atoi(string(recorded))
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

func (r *Registry) spawn(cmd *exec.Cmd, command string, logFile *os.File) (tree, <-chan error, bool, error) {
	if len(cmd.Args) == 3 && cmd.Args[1] == "-c" && endsInFilter(command) {
		cmd.Env = append(cmd.Environ(), "PAGER=cat", "GIT_PAGER=cat")
		if spawned, waited, err := startConsole(cmd, logFile, r.Lifetime); err == nil {
			return spawned, waited, true, nil
		}
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	spawned, err := startTree(cmd, r.Lifetime)
	if err != nil {
		_ = logFile.Close()
		return tree{}, nil, false, err
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	return spawned, waited, false, nil
}

func (r *Registry) list(entry Shell, process *live) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[entry.Name] = process
	return r.writeLocked(entry)
}

func (r *Registry) keep(entry Shell, process *live, waited <-chan error, logFile *os.File) error {
	err := r.list(entry, process)
	go r.await(waited, logFile, entry, process)
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
	spawned, waited, terminal, err := r.spawn(cmd, command, logFile)
	if err != nil {
		return Shell{}, err
	}
	entry := Shell{Name: name, Command: command, Dir: root, Owner: owner, TofuPID: r.self, PID: cmd.Process.Pid, State: Running, Started: started, Terminal: terminal,
		Kept: KeptBackground, Port: NamedPort(root, command, cmd.Env), Env: env}
	return entry, r.keep(entry, &live{tree: spawned, finished: make(chan struct{})}, waited, logFile)
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
	if raw, err := os.ReadFile(r.statePath(started.Name)); err == nil {
		_ = json.Unmarshal(raw, &started)
	}
	ended := time.Now()
	started.State, started.Ended, started.ExitCode = Exited, &ended, &code
	_ = r.writeLocked(started)
}

func (r *Registry) Prune() error {
	found, err := r.List()
	kept := time.Now().Add(-konst.FinishedShellKeptHours * time.Hour)
	for _, one := range found {
		if one.State != Running && (one.Ended == nil || one.Ended.Before(kept)) {
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

func (r *Registry) AwaitEnd(ctx context.Context, name string, within time.Duration) (Shell, error) {
	poll := time.NewTicker(konst.ReadyPollMillis * time.Millisecond)
	defer poll.Stop()
	gaveUp := time.After(within)
	for {
		entry, err := r.Read(name)
		if err != nil || entry.State != Running {
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
