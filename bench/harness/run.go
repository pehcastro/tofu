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
	"boji/internal/session"
	"boji/internal/turn"
)

const singleFileSuffix = ".json"

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
	capped, cancel := context.WithTimeout(ctx, plan.Caps.WallClock)
	defer cancel()

	cmd := exec.CommandContext(capped, plan.Command[0], plan.Command[1:]...)
	cmd.Dir = plan.WorkingDir
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

func TaskGates(armDir, startCommit string) Task {
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
	listing, err := session.NewStore(dir).Listing()
	if err != nil {
		return turn.Row{}, err
	}
	for _, header := range listing.Sessions {
		if header.At.Before(after) {
			continue
		}
		row, err := sessionRow(dir, header.ID)
		if err != nil {
			return turn.Row{}, err
		}
		return continuationOf(dir, row), nil
	}
	unread := ""
	for _, skip := range listing.Skipped {
		unread += " " + skip.ID
	}
	if unread != "" {
		unread = ", and these rows could not be read at all:" + unread
	}
	return turn.Row{}, fmt.Errorf("no turn row in %s is dated at or after %s, so the run wrote nothing this bench can read%s",
		dir, after.Format(time.RFC3339), unread)
}

func continuationOf(dir string, row turn.Row) turn.Row {
	walked := map[string]bool{row.ID: true}
	for row.ForkedInto != "" && !walked[row.ForkedInto] {
		next, err := sessionRow(dir, row.ForkedInto)
		if err != nil {
			return row
		}
		walked[next.ID] = true
		row = next
	}
	return row
}

func sessionRow(dir, id string) (turn.Row, error) {
	events, err := session.NewStore(dir).Body(id)
	if err != nil {
		return turn.Row{}, err
	}
	var row turn.Row
	var steps []turn.StepRow
	for i, event := range events {
		switch event.Kind {
		case session.EventStep:
			var step turn.StepRow
			if err := json.Unmarshal(event.Body, &step); err != nil {
				return turn.Row{}, fmt.Errorf("event %d of session %s does not read as a step: %w", i+1, id, err)
			}
			steps = append(steps, step)
		case session.EventOutcome:
			if err := json.Unmarshal(event.Body, &row); err != nil {
				return turn.Row{}, fmt.Errorf("the outcome event of session %s does not read as a turn row: %w", id, err)
			}
		case session.EventMessage, session.EventRead:
		default:
			return turn.Row{}, fmt.Errorf("event %d of session %s is of kind %q, which this bench has no reading for", i+1, id, event.Kind)
		}
	}
	if row.ID == "" {
		return turn.Row{}, fmt.Errorf("the body of session %s carries no outcome event, so there is no turn row in it", id)
	}
	row.Steps = steps
	return row, nil
}

func StoreTranscript(dir string, row turn.Row) error {
	body, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, row.ID+singleFileSuffix), body, 0o600)
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

	endReason, endGap := endReasonOf(session)
	row.EndReason = endReason

	for _, gap := range []string{credentialGap, endGap} {
		if gap != "" {
			gaps = append(gaps, gap)
		}
	}
	return scoreTree(row, src, gaps)
}

func MeasureClaude(execution Execution, src Sources, meta ClaudeMeta) (Row, []string, error) {
	row, gaps, err := ParseClaude([]byte(execution.Stdout), meta)
	if err != nil {
		return Row{}, nil, err
	}
	row.Start = execution.Start
	row.End = execution.Start.Add(time.Duration(row.WallClockMS) * time.Millisecond)
	row, gaps = scoreTree(row, src, gaps)
	return row, gaps, nil
}

func MeasureCodex(execution Execution, src Sources, meta CodexMeta) (Row, []string, error) {
	meta.Start, meta.End = execution.Start, execution.End
	row, gaps, err := ParseCodex([]byte(execution.Stdout), meta)
	if err != nil {
		return Row{}, nil, err
	}
	row, gaps = scoreTree(row, src, gaps)
	return row, gaps, nil
}

func scoreTree(row Row, src Sources, gaps []string) (Row, []string) {
	var gatesGap, checklistGap string
	row.Gates, gatesGap = measureGates(src)
	row.Checklist, checklistGap = measureChecklist(src)
	for _, gap := range []string{gatesGap, checklistGap} {
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
	outcomes = append(outcomes, RunGates(TaskGates(src.ArmDir, src.StartCommit))...)

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
		return []ChecklistResult{{Item: "the checker never ran, so this row carries no score"}},
			"checklist: " + err.Error()
	}
	return ChecklistResults(graded(checks)), ""
}

func graded(checks []ChecklistCheck) []ChecklistCheck {
	out := make([]ChecklistCheck, 0, len(checks))
	for _, c := range checks {
		if c.Status == ChecklistCheckRecorded {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out
}

func endReasonOf(session turn.Row) (EndReason, string) {
	switch session.Outcome {
	case turn.OutcomeStopped:
		return EndReasonDone, ""
	case turn.OutcomeStepCap:
		return EndReasonTurnCap, ""
	case turn.OutcomeRetiredCostCap:
		return EndReasonCrash, "end reason: this turn row was written when boji still had a cost cap, that cap is gone, and harness.EndReason has no variant for it, so this row says crash"
	case turn.OutcomeRetiredWallClockCap:
		return EndReasonWallClock, ""
	case turn.OutcomeDecisionCap:
		return EndReasonTurnCap, "end reason: the turn ended on the decision cap, and harness.EndReason has no variant for that, so this row says turn_cap"
	case turn.OutcomeTruncated:
		return EndReasonTruncated, ""
	case turn.OutcomeError:
		return EndReasonCrash, ""
	case turn.OutcomeUnset:
		return EndReasonCrash, "end reason: the turn row carries no outcome, so this row says crash"
	case turn.OutcomeForked:
		return EndReasonCrash, fmt.Sprintf(
			"end reason: session %s forked into %s and a forked session is not the end of the work, "+
				"so the session that finished it is the one to score and it was not on disk to be followed",
			session.ID, session.ForkedInto)
	}
	panic("harness: unknown turn outcome " + session.Outcome.String())
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

func Spend(rows []Row) string {
	b := &strings.Builder{}
	fmt.Fprint(b, "SPEND, each arm in its own unit\n")
	sorted := append([]Row(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Version != sorted[j].Version {
			return sorted[i].Version < sorted[j].Version
		}
		return sorted[i].Arm < sorted[j].Arm
	})
	for _, r := range sorted {
		fmt.Fprintf(b, "%s v%d: %s. tokens %d in, %d out. wall clock %d ms. turns %d\n",
			r.Arm, r.Version, spendUnitOf(r), r.BilledInput, r.BilledOutput, r.WallClockMS, r.Turns)
	}
	fmt.Fprint(b, "the units do not add: anthropic subscription quota, chatgpt subscription quota and openrouter dollars are three different currencies, "+
		"and a row that spends none of a currency is not cheaper than one that spends some of another. Compare within a column, never across.\n")
	return b.String()
}

func spendUnitOf(r Row) string {
	if r.ModelDollars != nil {
		return fmt.Sprintf("api key, %.4f dollars on the model, %.4f on jev", *r.ModelDollars, r.JudgeDollars)
	}
	if r.Arm == ArmBoji {
		return fmt.Sprintf("subscription quota for the model, %.4f openrouter dollars for jev", r.JudgeDollars)
	}
	return "subscription quota, no money left the account"
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
