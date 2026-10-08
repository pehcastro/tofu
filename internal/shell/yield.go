package shell

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"time"
)

func (r *Registry) Yield(ctx context.Context, cmd *exec.Cmd, command, owner string, within time.Duration) (Shell, string, error) {
	got, err := r.YieldReady(ctx, cmd, command, owner, Wait{Within: within, Poll: within})
	return got.Shell, got.Output, err
}

type Readiness string

const (
	ReadyExited  Readiness = "exited"
	ReadyPort    Readiness = "port"
	ReadyLine    Readiness = "line"
	ReadyWaited  Readiness = "waited"
	ReadyStopped Readiness = "stopped"
)

func (r Readiness) Words() string {
	switch r {
	case ReadyExited:
		return "it exited"
	case ReadyPort:
		return "its port opened"
	case ReadyLine:
		return "it printed a ready line"
	case ReadyWaited:
		return "the wait ran out"
	case ReadyStopped:
		return "the turn stopped"
	}
	panic("shell: unknown readiness " + string(r))
}

type Wait struct {
	Within time.Duration
	Poll   time.Duration
	Port   int
	Kept   Kept
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
	started := time.Now()
	spawned, waited, terminal, err := r.spawn(cmd, command, logFile)
	if err != nil {
		_ = os.Remove(r.logPath(name))
		return Yielded{}, err
	}
	got := Yielded{Shell: Shell{Name: name, Command: command, Dir: cmd.Dir, Owner: owner, TofuPID: r.self, PID: cmd.Process.Pid, State: Running, Started: started, Terminal: terminal, Port: wait.Port}}
	process := &live{tree: spawned, finished: make(chan struct{})}
	if err := r.list(got.Shell, process); err != nil {
		_ = killTree(cmd.Process.Pid)
		spawned.release()
		_ = logFile.Close()
		return Yielded{}, err
	}
	exited := func(waitErr error) (Yielded, error) {
		_ = logFile.Close()
		spawned.release()
		output, readErr := os.ReadFile(r.logPath(name))
		r.mu.Lock()
		delete(r.running, name)
		_ = os.Remove(r.statePath(name))
		_ = os.Remove(r.logPath(name))
		r.mu.Unlock()
		close(process.finished)
		ended, code := time.Now(), exitCode(waitErr)
		got.Shell.State, got.Shell.Ended, got.Shell.ExitCode = Exited, &ended, &code
		got.Output, got.Ready, got.Took = Decode(output), ReadyExited, time.Since(got.Shell.Started)
		return got, readErr
	}
	var polled <-chan time.Time
	if wait.Poll > 0 {
		poll := time.NewTicker(wait.Poll)
		defer poll.Stop()
		polled = poll.C
	}
	gaveUp := time.After(wait.Within)
	for got.Ready == "" {
		select {
		case waitErr := <-waited:
			return exited(waitErr)
		case <-gaveUp:
			got.Ready = ReadyWaited
		case <-ctx.Done():
			got.Ready = ReadyStopped
		case <-polled:
			got.Ready = r.readiness(name, wait)
		}
	}
	select {
	case waitErr := <-waited:
		return exited(waitErr)
	default:
	}
	got.Took, got.Shell.Ready = time.Since(got.Shell.Started), got.Ready
	if got.Ready != ReadyStopped || wait.Kept == KeptBackground {
		got.Shell.Kept = wait.Kept
	}
	if err := r.keep(got.Shell, process, waited, logFile); err != nil {
		return got, err
	}
	got.Output, err = r.Tail(name, DefaultTail)
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
