package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"boji/internal/crew"
	"boji/internal/konst"
	"boji/internal/llm"
)

type DepthLimitError struct {
	Depth int
	Limit int
}

func (e DepthLimitError) Error() string {
	return fmt.Sprintf("spawn refused: a child at depth %d would pass the crew depth limit of %d", e.Depth, e.Limit)
}

type BreadthLimitError struct {
	Spawned int
	Limit   int
}

func (e BreadthLimitError) Error() string {
	return fmt.Sprintf("spawn refused: this turn has already spawned %d children and the crew breadth limit is %d", e.Spawned, e.Limit)
}

type DoneVerdict string

const (
	DoneAccepted DoneVerdict = "accepted"
	DoneReopen   DoneVerdict = "reopen"
)

type DoneDecision struct {
	ID      string
	Verdict DoneVerdict
	Reason  string
}

type DoneReview interface {
	Review(ctx context.Context, child Row) (DoneDecision, error)
}

type CheapDoneReview struct{}

func (CheapDoneReview) Review(_ context.Context, child Row) (DoneDecision, error) {
	for _, step := range child.Steps {
		for _, call := range step.ToolCalls {
			if call.Error == "" && (call.ExitCode == nil || *call.ExitCode == 0) {
				return DoneDecision{Verdict: DoneAccepted, Reason: "the child ran " + call.Tool + " and it did not fail"}, nil
			}
		}
	}
	return DoneDecision{Verdict: DoneReopen, Reason: "nothing in the child's row is evidence the work happened: not one tool call ran without failing"}, nil
}

type SpawnTool struct {
	Review   DoneReview
	parentID string
	depth    int
	spawned  int
	base     Config
	roster   *crew.Roster
	children []Row
}

func NewSpawnTool(parentID string, base Config, roster *crew.Roster) *SpawnTool {
	return &SpawnTool{parentID: parentID, base: base, roster: roster}
}

func (t *SpawnTool) Name() string { return "spawn" }

func (t *SpawnTool) Children() []Row { return t.children }

func (t *SpawnTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "spawn",
		Description: "hands one piece of work to a child with its own context and its own conversation, and returns the child's report rather than its transcript. " +
			"owns lists the paths the child may write, every other path is refused at the write, and no two children may hold overlapping paths. " +
			"At most " + strconv.Itoa(konst.CrewMaxBreadth) + " children per turn, nested at most " + strconv.Itoa(konst.CrewMaxDepth) + " deep.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task":    map[string]any{"type": "string"},
				"mission": map[string]any{"type": "string", "description": "the work in a handful of words, as a board entry reads: work on BOJI-395. the task is the brief and is kept whole"},
				"owns":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"task", "owns"},
		},
	}
}

type spawnArgs struct {
	Task    string   `json:"task"`
	Mission string   `json:"mission,omitempty"`
	Owns    []string `json:"owns"`
}

func (a spawnArgs) mission() string {
	if given := strings.TrimSpace(a.Mission); given != "" {
		return given
	}
	first, _, _ := strings.Cut(strings.TrimSpace(a.Task), "\n")
	if runes := []rune(first); len(runes) > konst.SubAgentMissionChars {
		return strings.TrimSpace(string(runes[:konst.SubAgentMissionChars])) + "..."
	}
	return first
}

func (t *SpawnTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args spawnArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("spawn: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Task) == "" {
		return Result{}, errors.New("spawn: task is required")
	}
	if len(args.Owns) == 0 {
		return Result{}, errors.New("spawn: owns is required, and a child holding no paths could write nothing")
	}
	if t.depth+1 > konst.CrewMaxDepth {
		return Result{}, DepthLimitError{Depth: t.depth + 1, Limit: konst.CrewMaxDepth}
	}
	if t.spawned >= konst.CrewMaxBreadth {
		return Result{}, BreadthLimitError{Spawned: t.spawned, Limit: konst.CrewMaxBreadth}
	}

	childID := t.parentID + "-c" + strconv.Itoa(t.spawned+1)
	mission := args.mission()
	if err := t.roster.Hold(crew.SubAgent{ID: childID, Mission: mission, Brief: args.Task, Owns: args.Owns}); err != nil {
		var collision crew.CollisionError
		if errors.As(err, &collision) && collision.HolderReport != "" {
			return Result{Command: "handback " + collision.Holder, Content: fmt.Sprintf(
				"no child was started: %s already holds %q, and %q overlaps it. Send this work to %s rather than starting a rival.\n\n%s has reported:\n%s",
				collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder, collision.Holder, collision.HolderReport)}, nil
		}
		return Result{}, fmt.Errorf("spawn: %w", err)
	}

	owned := slices.Clone(t.base.Tools.tools)
	for i, tool := range owned {
		switch tool.Name() {
		case "write", "edit":
			owned[i] = ownedTool{tool: tool, owns: args.Owns}
		case "bash":
			owned[i] = ownedShell{tool: tool, owns: args.Owns}
		}
	}
	nested := &SpawnTool{Review: t.Review, parentID: childID, depth: t.depth + 1, base: t.base, roster: t.roster}
	child := t.base
	child.Task = args.Task
	child.Tools = NewRegistry(append(owned, nested)...)
	child.NewID = func() string { return childID }

	childCtx, release := context.WithCancel(ctx)
	defer release()
	t.spawned++
	row, runErr := Run(childCtx, child)
	claims, state := []Row{row}, crew.InReview
	switch {
	case ctx.Err() != nil:
		state = crew.Parked
	case runErr != nil:
		state = crew.Errored
	default:
		claims, state = t.reviewed(ctx, child, row)
	}
	t.retain(append(claims, nested.children...))

	report := childReport(claims[len(claims)-1], state)
	t.roster.Reached(childID, state, report)
	if runErr != nil {
		return Result{}, fmt.Errorf("spawn: child %s is %s: %w", childID, state, runErr)
	}
	return Result{Content: report, Command: "spawn " + childID + " " + state.String() + ": " + mission}, nil
}

func (t *SpawnTool) retain(rows []Row) {
	t.children = append(t.children, rows...)
	for i := range len(t.children) - konst.SubAgentRetainedRows {
		released := t.children[i].Summary()
		released.Conversation = nil
		t.children[i] = released
	}
}

func (t *SpawnTool) reviewed(ctx context.Context, child Config, first Row) ([]Row, crew.State) {
	if t.Review == nil {
		return []Row{first}, crew.InReview
	}
	decision, err := t.Review.Review(ctx, first)
	if err != nil {
		first.Warnings = append(first.Warnings, "the done review did not run, so the child's own claim stands: "+err.Error())
		return []Row{first}, crew.InReview
	}
	if decision.ID != "" {
		first.DecisionIDs = append(first.DecisionIDs, decision.ID)
	}
	switch decision.Verdict {
	case DoneAccepted:
		return []Row{first}, crew.Finished
	case DoneReopen:
		child.Task = first.Task + "\n\nYou reported this finished and the done review did not believe you: " + decision.Reason
		child.NewID = func() string { return first.ID + "-r" }
		second, err := Run(ctx, child)
		if err != nil {
			first.Warnings = append(first.Warnings, "the child was re-opened and did not run again, so its first claim stands: "+err.Error())
			return []Row{first}, crew.Errored
		}
		second.DecisionIDs = append(second.DecisionIDs, decision.ID)
		return []Row{first, second}, crew.InReview
	}
	panic("turn: unknown done verdict " + string(decision.Verdict))
}

func childReport(row Row, state crew.State) string {
	calls := 0
	for _, step := range row.Steps {
		calls += len(step.ToolCalls)
	}
	report := &strings.Builder{}
	fmt.Fprintf(report, "sub-agent %s is %s, %s after %d steps and %d tool calls, costing $%.4f\n",
		row.ID, state, row.Outcome, len(row.Steps), calls, row.TotalCostUSD)
	if len(row.Steps) > 0 {
		report.WriteString(row.Steps[len(row.Steps)-1].AssistantText)
	}
	for _, step := range row.Steps {
		for _, call := range step.ToolCalls {
			if call.Error != "" {
				report.WriteString("\ncould not: " + call.Tool + ": " + call.Error)
			}
		}
	}
	return report.String()
}

type ownedTool struct {
	tool Tool
	owns []string
}

func (t ownedTool) Name() string { return t.tool.Name() }

func (t ownedTool) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and only inside the paths this agent holds: " + strings.Join(t.owns, ", ")
	return definition
}

func (t ownedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	if err := crew.Allow(args.Path, t.owns); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}

type ownedShell struct {
	tool Tool
	owns []string
}

func (t ownedShell) Name() string { return t.tool.Name() }

func (t ownedShell) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and every path the command names has to be inside the paths this agent holds: " +
		strings.Join(t.owns, ", ") +
		". A command naming any path outside them is refused before it runs, and that work goes back to the parent."
	return definition
}

func (t ownedShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	if err := crew.AllowCommand(args.Command, t.owns); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}
