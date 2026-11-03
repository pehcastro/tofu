package shell

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/sys"
)

var ErrNotRunning = errors.New("shell: not running")

var ErrTreeGone = errors.New("shell: the process tree is already gone")

func HitDeadline(after time.Duration) string {
	return "hit the deadline after " + strconv.FormatFloat(after.Seconds(), 'f', -1, 64) + " s"
}

func (r *Registry) Kill(name string) error { return r.stop(name, HolderPerson) }

func (r *Registry) AgentKill(name string) error { return r.stop(name, HolderAgent) }

func (r *Registry) KillAtDeadline(name string, _ time.Duration) error {
	return r.stop(name, HolderAgent)
}

func (r *Registry) stop(name string, by Holder) error {
	process, err := r.terminate(name, by)
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

func (r *Registry) StopUnder(dir string) []string {
	listed, _ := r.List()
	folded := strings.ToLower(filepath.ToSlash(dir))
	var stopped []string
	for _, one := range listed {
		rel, err := filepath.Rel(dir, one.Dir)
		inside := err == nil && !strings.HasPrefix(rel, "..")
		named := strings.Contains(strings.ToLower(filepath.ToSlash(one.Command)), folded)
		if one.State == Running && (inside || named) && r.Kill(one.Name) == nil {
			stopped = append(stopped, one.Name)
		}
	}
	return stopped
}

func (r *Registry) Restart(name string) (Shell, error) { return r.restart(name, HolderPerson) }

func (r *Registry) AgentRestart(name string) (Shell, error) { return r.restart(name, HolderAgent) }

func (r *Registry) restart(name string, by Holder) (Shell, error) {
	entry, err := r.Read(name)
	if err != nil {
		return Shell{}, err
	}
	if err := r.stop(name, by); err != nil && !errors.Is(err, ErrNotRunning) {
		return Shell{}, err
	}
	return r.Start(entry.Dir, name, entry.Command, entry.Owner, entry.Env...)
}

func processName(word string) string {
	name := filepath.Base(strings.Trim(word, `"'`))
	if strings.EqualFold(filepath.Ext(name), ".exe") {
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

var (
	flagShape  = sys.LazyRegexp(`^(-{1,2}|/{1,2})([A-Za-z0-9]+)(:\$?[A-Za-z]+)?$`)
	actionWord = sys.LazyRegexp(`^[A-Za-z]+$`)
)

func killed(fields []string) []int {
	if len(fields) < 2 {
		return nil
	}
	program := strings.ToLower(processName(fields[0]))
	switch program {
	case "powershell", "pwsh", "bash", "sh", "cmd":
		return wrappedKill(program, fields[1:])
	case "kill", "taskkill", "pkill", "stop-process":
	default:
		return nil
	}
	var pids []int
	for at := 1; at < len(fields); at++ {
		field := strings.Trim(fields[at], `"'`)
		shape := flagShape().FindStringSubmatch(field)
		if shape != nil && (program == "taskkill" || shape[1][0] == '-') {
			switch flag := strings.ToLower(shape[2]); {
			case program == "kill" && flag == "l", program == "stop-process" && flag == "whatif",
				program == "taskkill" && slices.Contains([]string{"s", "u", "p", "fi"}, flag):
				return nil
			case program == "stop-process" && slices.Contains([]string{"erroraction", "ea", "warningaction", "wa", "informationaction", "infa"}, flag):
				if at+1 >= len(fields) || !actionWord().MatchString(strings.Trim(fields[at+1], `"'`)) {
					return nil
				}
				at++
			case (program == "taskkill" && flag == "im" || program == "stop-process" && (flag == "name" || flag == "processname")) && at+1 < len(fields):
				at++
				named := pidsIn(strings.Trim(fields[at], `"'`), processesNamed)
				if named == nil {
					return nil
				}
				pids = append(pids, named...)
			}
			continue
		}
		listed := pidsIn(field, asPid)
		if listed == nil && program == "pkill" {
			listed = pidsIn(field, processesNamed)
		}
		if listed == nil {
			return nil
		}
		pids = append(pids, listed...)
	}
	return pids
}

func wrappedKill(program string, rest []string) []int {
	for at, field := range rest {
		switch flag := strings.ToLower(field); {
		case flag == "-c" || flag == "-command" && (program == "powershell" || program == "pwsh") || flag == "/c" && program == "cmd":
			return killed(rest[at+1:])
		case at > 0 && slices.Contains([]string{"-executionpolicy", "-ep"}, strings.ToLower(rest[at-1])):
		case !flagShape().MatchString(field):
			return nil
		}
	}
	return nil
}

func pidsIn(field string, lookup func(string) []int) []int {
	var pids []int
	for _, one := range strings.Split(field, ",") {
		found := lookup(one)
		if len(found) == 0 {
			return nil
		}
		pids = append(pids, found...)
	}
	return pids
}

func asPid(word string) []int {
	if pid, err := strconv.Atoi(word); err == nil && pid > 0 {
		return []int{pid}
	}
	return nil
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

func (r *Registry) terminate(name string, by Holder) (*live, error) {
	entry, process, err := r.markKilled(name, by)
	if err != nil {
		return nil, err
	}
	killErr := killTree(entry.PID)
	if killErr != nil && !errors.Is(killErr, ErrTreeGone) {
		return nil, killErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ended := time.Now()
	entry.Ended = &ended
	if killErr != nil {
		entry.State = Exited
		return nil, errors.Join(fmt.Errorf("%w: %q", ErrNotRunning, name), r.writeLocked(entry))
	}
	entry.State = Killed
	return process, r.writeLocked(entry)
}

func (r *Registry) markKilled(name string, by Holder) (Shell, *live, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, err := r.readLocked(name)
	if err != nil {
		return entry, nil, err
	}
	if entry.State != Running {
		return entry, nil, fmt.Errorf("%w: %q", ErrNotRunning, name)
	}
	if err := entry.refusedToAgent(); by == HolderAgent && err != nil {
		return entry, nil, err
	}
	process := r.running[name]
	if process != nil {
		process.killed = true
	}
	return entry, process, nil
}
