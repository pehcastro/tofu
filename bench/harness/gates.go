package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type GateStatus string

const (
	GateStatusPassed           GateStatus = "passed"
	GateStatusFailed           GateStatus = "failed"
	GateStatusCouldNotEvaluate GateStatus = "could_not_evaluate"
)

type GateOutcome struct {
	Name   string
	Status GateStatus
	Reason string
}

type DiffCost struct {
	Files     int
	Additions int
	Deletions int
}

type ChecklistItem struct {
	Item    string
	Command []string
}

type TestGateCommand struct {
	Command    []string
	TestDirRel string
	Pattern    *regexp.Regexp
}

type Task struct {
	Dir         string
	StartCommit string
	Build       []string
	Lint        []string
	Test        TestGateCommand
	Checklist   []ChecklistItem
}

func RunGates(t Task) []GateOutcome {
	outcomes := []GateOutcome{
		runCommandGate("build", t.Dir, t.Build),
		runTestGate("test", t.Dir, t.StartCommit, t.Test),
		runCommandGate("lint", t.Dir, t.Lint),
	}
	for _, item := range t.Checklist {
		outcomes = append(outcomes, runCommandGate("checklist: "+item.Item, t.Dir, item.Command))
	}
	diffOutcome, _ := RunDiffCostGate(t.Dir, t.StartCommit)
	return append(outcomes, diffOutcome)
}

func runCommandGate(name, dir string, command []string) GateOutcome {
	if len(command) == 0 {
		return GateOutcome{Name: name, Status: GateStatusCouldNotEvaluate, Reason: "no command declared"}
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	reason := strings.TrimSpace(string(out))
	if err == nil {
		return GateOutcome{Name: name, Status: GateStatusPassed, Reason: reason}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return GateOutcome{Name: name, Status: GateStatusCouldNotEvaluate, Reason: "command not found: " + strings.Join(command, " ")}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return GateOutcome{Name: name, Status: GateStatusFailed, Reason: reason}
	}
	return GateOutcome{Name: name, Status: GateStatusCouldNotEvaluate, Reason: err.Error()}
}

func runTestGate(name, dir, startCommit string, spec TestGateCommand) GateOutcome {
	before, err := countTestsAtCommit(dir, startCommit, spec.TestDirRel, spec.Pattern)
	if err != nil {
		return GateOutcome{Name: name, Status: GateStatusCouldNotEvaluate, Reason: "could not count baseline tests: " + err.Error()}
	}
	after, err := countTestsInTree(dir, spec.TestDirRel, spec.Pattern)
	if err != nil {
		return GateOutcome{Name: name, Status: GateStatusCouldNotEvaluate, Reason: "could not count current tests: " + err.Error()}
	}
	if after < before {
		return GateOutcome{Name: name, Status: GateStatusFailed, Reason: fmt.Sprintf("tests deleted: %d before, %d after", before, after)}
	}
	outcome := runCommandGate(name, dir, spec.Command)
	if outcome.Status == GateStatusPassed {
		outcome.Reason = fmt.Sprintf("%d tests, before %d after %d", after, before, after)
	}
	return outcome
}

func countTestsInTree(dir, rel string, pattern *regexp.Regexp) (int, error) {
	entries, err := os.ReadDir(filepath.Join(dir, rel))
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, rel, entry.Name()))
		if err != nil {
			return 0, err
		}
		count += len(pattern.FindAllIndex(data, -1))
	}
	return count, nil
}

func countTestsAtCommit(dir, commit, rel string, pattern *regexp.Regexp) (int, error) {
	listOut, err := exec.Command("git", "-C", dir, "ls-tree", "-r", "--name-only", commit, "--", rel).Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(listOut), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		data, err := exec.Command("git", "-C", dir, "show", commit+":"+path).Output()
		if err != nil {
			return 0, err
		}
		count += len(pattern.FindAllIndex(data, -1))
	}
	return count, nil
}

func RunDiffCostGate(dir, startCommit string) (GateOutcome, DiffCost) {
	out, err := exec.Command("git", "-C", dir, "diff", "--numstat", startCommit).Output()
	if err != nil {
		return GateOutcome{Name: "diff_cost", Status: GateStatusCouldNotEvaluate, Reason: err.Error()}, DiffCost{}
	}
	cost := DiffCost{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		cost.Files++
		if a, err := strconv.Atoi(fields[0]); err == nil {
			cost.Additions += a
		}
		if d, err := strconv.Atoi(fields[1]); err == nil {
			cost.Deletions += d
		}
	}
	reason := fmt.Sprintf("files %d, additions %d, deletions %d", cost.Files, cost.Additions, cost.Deletions)
	return GateOutcome{Name: "diff_cost", Status: GateStatusPassed, Reason: reason}, cost
}
