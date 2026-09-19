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
	"boji/internal/llm"
)

const (
	CrewMaxDepth   = 2
	CrewMaxBreadth = 4
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

type SpawnTool struct {
	parentID string
	depth    int
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
			"At most " + strconv.Itoa(CrewMaxBreadth) + " children per turn, nested at most " + strconv.Itoa(CrewMaxDepth) + " deep.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{"type": "string"},
				"owns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"task", "owns"},
		},
	}
}

type spawnArgs struct {
	Task string   `json:"task"`
	Owns []string `json:"owns"`
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
	if t.depth+1 > CrewMaxDepth {
		return Result{}, DepthLimitError{Depth: t.depth + 1, Limit: CrewMaxDepth}
	}
	if len(t.children) >= CrewMaxBreadth {
		return Result{}, BreadthLimitError{Spawned: len(t.children), Limit: CrewMaxBreadth}
	}

	childID := t.parentID + "-c" + strconv.Itoa(len(t.children)+1)
	if err := t.roster.Hold(childID, args.Owns); err != nil {
		return Result{}, fmt.Errorf("spawn: %w", err)
	}

	owned := slices.Clone(t.base.Tools.tools)
	for i, tool := range owned {
		if tool.Name() == "write" || tool.Name() == "edit" {
			owned[i] = ownedTool{tool: tool, owns: args.Owns}
		}
	}
	nested := &SpawnTool{parentID: childID, depth: t.depth + 1, base: t.base, roster: t.roster}
	child := t.base
	child.Task = args.Task
	child.Tools = NewRegistry(append(owned, nested)...)
	child.NewID = func() string { return childID }

	row, err := Run(ctx, child)
	t.children = append(t.children, row)
	t.children = append(t.children, nested.children...)
	if err != nil {
		return Result{}, fmt.Errorf("spawn: child %s: %w", childID, err)
	}
	return Result{Content: childReport(row), Command: "spawn " + childID}, nil
}

func childReport(row Row) string {
	calls := 0
	for _, step := range row.Steps {
		calls += len(step.ToolCalls)
	}
	report := &strings.Builder{}
	fmt.Fprintf(report, "child %s finished %s after %d steps and %d tool calls, costing $%.4f\n",
		row.ID, row.Outcome, len(row.Steps), calls, row.TotalCostUSD)
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
