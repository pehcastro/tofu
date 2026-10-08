package shell

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrNotRunning = errors.New("shell: not running")

var ErrTreeGone = errors.New("shell: the process tree is already gone")

func HitDeadline(after time.Duration) string {
	return "hit the deadline after " + strconv.FormatFloat(after.Seconds(), 'f', -1, 64) + " s"
}

func (r *Registry) Kill(name string) error { return r.stop(name, 0) }

func (r *Registry) KillAtDeadline(name string, deadline time.Duration) error {
	return r.stop(name, deadline)
}

func (r *Registry) stop(name string, deadline time.Duration) error {
	process, err := r.terminate(name, deadline)
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

func processName(word string) string {
	name := filepath.Base(strings.Trim(word, `"'`))
	if strings.EqualFold(filepath.Ext(name), ".exe") {
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

func killed(fields []string) []int {
	if len(fields) < 2 {
		return nil
	}
	program := strings.ToLower(processName(fields[0]))
	if program == "powershell" || program == "pwsh" {
		at := slices.IndexFunc(fields, func(field string) bool { return slices.Contains([]string{"-command", "-c"}, strings.ToLower(field)) })
		if at < 0 {
			return nil
		}
		return killed(fields[at+1:])
	}
	if !slices.Contains([]string{"kill", "taskkill", "pkill", "stop-process"}, program) {
		return nil
	}
	var pids []int
	for at := 1; at < len(fields); at++ {
		field := strings.Trim(fields[at], `"'`)
		flag := strings.ToLower(strings.TrimLeft(field, "-/"))
		names := program == "pkill" && !strings.HasPrefix(field, "-")
		if (program == "taskkill" && flag == "im" || program == "stop-process" && flag == "name") && at+1 < len(fields) {
			at, field, names = at+1, strings.Trim(fields[at+1], `"'`), true
		}
		pid, err := strconv.Atoi(field)
		switch {
		case err == nil && pid > 0:
			pids = append(pids, pid)
		case names:
			for _, name := range strings.Split(field, ",") {
				named := processesNamed(name)
				if len(named) == 0 {
					return nil
				}
				pids = append(pids, named...)
			}
		case !strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "/"):
			return nil
		}
	}
	return pids
}

func (r *Registry) Owning(command string) []Shell {
	pids := killed(strings.Fields(command))
	if len(pids) == 0 {
		return nil
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

func (r *Registry) terminate(name string, deadline time.Duration) (*live, error) {
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
	entry.State, entry.Ended, entry.Deadline = Killed, &ended, deadline.Milliseconds()
	return process, r.writeLocked(entry)
}
