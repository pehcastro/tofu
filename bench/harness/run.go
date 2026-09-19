package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"boji/internal/judge/ledger"
	"boji/internal/turn"
)

type Execution struct {
	Plan      Plan
	Start     time.Time
	End       time.Time
	ExitCode  int
	Stdout    string
	Stderr    string
	EndReason EndReason
}

func (e Execution) Elapsed() time.Duration { return e.End.Sub(e.Start) }

func Execute(ctx context.Context, plan Plan) (Execution, error) {
	if plan.Arm != ArmBoji {
		return Execution{}, fmt.Errorf("the %s arm is not executed by this runner, it spends an account nobody approved: print its plan instead", plan.Arm)
	}
	capped, cancel := context.WithTimeout(ctx, plan.Caps.WallClock)
	defer cancel()

	cmd := exec.CommandContext(capped, plan.Command[0], plan.Command[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	execution := Execution{
		Plan:      plan,
		Start:     start,
		End:       time.Now(),
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		EndReason: EndReasonDone,
	}

	if errors.Is(capped.Err(), context.DeadlineExceeded) {
		execution.EndReason = EndReasonWallClock
		execution.ExitCode = -1
		return execution, nil
	}
	if err == nil {
		return execution, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		execution.ExitCode = exitErr.ExitCode()
		execution.EndReason = EndReasonCrash
		return execution, nil
	}
	return Execution{}, fmt.Errorf("%s never started: %w", Shell(plan.Command), err)
}

type Sources struct {
	LedgerDir   string
	ArmDir      string
	BunBin      string
	CheckerPath string
	StartCommit string
}

var testCasePattern = regexp.MustCompile(`\b(?:test|it)\s*\(`)

func V1Gates(armDir, startCommit string) Task {
	return Task{
		Dir:         armDir,
		StartCommit: startCommit,
		Build:       []string{"bun", "build", "src/index.ts", "--target=bun"},
		Lint:        []string{"bun", "x", "tsc", "--noEmit", "--skipLibCheck"},
		Test: TestGateCommand{
			Command:    []string{"bun", "test"},
			TestDirRel: "src",
			Pattern:    testCasePattern,
		},
	}
}

func LatestSession(dir string, after time.Time) (turn.Row, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return turn.Row{}, err
	}
	var newest turn.Row
	var found bool
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		written, err := sessionWrittenAt(path)
		if err != nil {
			return turn.Row{}, err
		}
		if written.Before(after) || (found && !written.After(newest.At)) {
			continue
		}
		row, err := LoadSession(path)
		if err != nil {
			return turn.Row{}, err
		}
		newest, found = row, true
	}
	if !found {
		return turn.Row{}, fmt.Errorf("no turn row in %s is dated at or after %s, so the run wrote nothing this bench can read", dir, after.Format(time.RFC3339))
	}
	return newest, nil
}

func sessionWrittenAt(path string) (time.Time, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	var dated struct {
		At time.Time `json:"at"`
	}
	if err := json.Unmarshal(body, &dated); err != nil {
		return time.Time{}, fmt.Errorf("%s carries no readable date: %w", path, err)
	}
	return dated.At, nil
}

func LoadSession(path string) (turn.Row, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return turn.Row{}, err
	}
	var row turn.Row
	if err := json.Unmarshal(body, &row); err != nil {
		return turn.Row{}, fmt.Errorf("%s is not a turn row: %w", path, err)
	}
	return row, nil
}

func MeasureBoji(session turn.Row, src Sources, meta RunMeta) (Row, []string) {
	row, gaps, err := ParseBoji(src.LedgerDir, ledger.Filter{TurnID: session.ID}, meta)
	if err != nil {
		row = Row{Arm: meta.Arm, Task: meta.Task, Version: meta.Version, Run: meta.Run, CLIVersion: meta.CLIVersion, CredentialKind: meta.CredentialKind, Commit: meta.Commit}
		gaps = append(gaps, "jev ledger: "+err.Error())
	}

	credential, credentialGap := credentialOfSpend(session.Spend)
	if credential == CredentialKindKey {
		modelDollars := session.TotalCostUSD
		row.ModelDollars = &modelDollars
	}
	row.CredentialKind = credential
	row.Model = session.Model
	row.Start = session.At
	row.End = session.At.Add(time.Duration(session.WallClockMS) * time.Millisecond)
	row.WallClockMS = session.WallClockMS
	row.Turns = int64(len(session.Steps))
	row.ToolCalls = countToolCalls(session)
	row.BilledInput, row.BilledOutput = countTokens(session)

	endReason, endGap := endReasonOf(session.Outcome)
	row.EndReason = endReason

	var gatesGap, checklistGap string
	row.Gates, gatesGap = measureGates(src)
	row.Checklist, checklistGap = measureChecklist(src)

	for _, gap := range []string{credentialGap, endGap, gatesGap, checklistGap} {
		if gap != "" {
			gaps = append(gaps, gap)
		}
	}
	return row, gaps
}

func credentialOfSpend(spend turn.Spend) (CredentialKind, string) {
	switch spend {
	case turn.SpendSubscription:
		return CredentialKindSubscription, ""
	case turn.SpendAPIKey:
		return CredentialKindKey, ""
	}
	return CredentialKindKey, fmt.Sprintf("credential kind: the turn row names its spend %q, which is neither subscription nor api_key, so this row says key and its dollars cannot be trusted", spend)
}

func measureGates(src Sources) ([]GateResult, string) {
	outcomes := []GateOutcome{runCommandGate("install", src.ArmDir, []string{"bun", "install"})}
	outcomes = append(outcomes, RunGates(V1Gates(src.ArmDir, src.StartCommit))...)

	results := make([]GateResult, 0, len(outcomes))
	for _, o := range outcomes {
		results = append(results, GateResult(o))
	}
	if src.StartCommit == "" {
		return results, "gates: " + src.ArmDir + " is not a git repository of its own, so the test-count and diff-cost gates had no baseline commit to measure against"
	}
	return results, ""
}

func measureChecklist(src Sources) ([]ChecklistResult, string) {
	checks, err := RunChecklist(src.BunBin, src.CheckerPath, src.ArmDir)
	if err != nil {
		return nil, "checklist: " + err.Error()
	}
	return ChecklistResults(graded(checks)), ""
}

const recordedChecklistItem = 11

func graded(checks []ChecklistCheck) []ChecklistCheck {
	out := make([]ChecklistCheck, 0, len(checks))
	for _, c := range checks {
		if c.Item == recordedChecklistItem {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out
}

func endReasonOf(outcome turn.Outcome) (EndReason, string) {
	switch outcome {
	case turn.OutcomeStopped:
		return EndReasonDone, ""
	case turn.OutcomeStepCap:
		return EndReasonTurnCap, ""
	case turn.OutcomeRetiredCostCap:
		return EndReasonCrash, "end reason: this turn row was written when boji still had a cost cap, that cap is gone, and harness.EndReason has no variant for it, so this row says crash"
	case turn.OutcomeWallClockCap:
		return EndReasonWallClock, ""
	case turn.OutcomeDecisionCap:
		return EndReasonTurnCap, "end reason: the turn ended on the decision cap, and harness.EndReason has no variant for that, so this row says turn_cap"
	case turn.OutcomeError:
		return EndReasonCrash, ""
	case turn.OutcomeUnset:
		return EndReasonCrash, "end reason: the turn row carries no outcome, so this row says crash"
	}
	panic("harness: unknown turn outcome " + outcome.String())
}

func countToolCalls(session turn.Row) ToolCalls {
	var calls ToolCalls
	for _, step := range session.Steps {
		for _, call := range step.ToolCalls {
			switch call.Tool {
			case "read":
				calls.Read++
			case "write":
				calls.Write++
			case "bash":
				calls.Shell++
			default:
				calls.Other++
			}
			if call.Error != "" || (call.ExitCode != nil && *call.ExitCode != 0) {
				calls.Failed++
			}
		}
	}
	return calls
}

func countTokens(session turn.Row) (input, output int64) {
	for _, step := range session.Steps {
		input += int64(step.PromptTokens)
		output += int64(step.CompletionTokens)
	}
	return input, output
}

func ChecklistScore(row Row) (passed, total int) {
	for _, c := range row.Checklist {
		total++
		if c.Passed {
			passed++
		}
	}
	return passed, total
}

func OwnStartCommit(dir string) string {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	root, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	if !strings.EqualFold(filepath.Clean(filepath.FromSlash(strings.TrimSpace(string(root)))), filepath.Clean(absolute)) {
		return ""
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(head))
}
