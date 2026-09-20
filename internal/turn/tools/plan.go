package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

const PlanToolName = "plan"

type PlanState string

const (
	PlanPending PlanState = "pending"
	PlanRunning PlanState = "running"
	PlanDone    PlanState = "done"
	PlanDropped PlanState = "dropped"
)

type PlanItem struct {
	Phase string    `json:"phase,omitempty"`
	Text  string    `json:"text"`
	State PlanState `json:"state"`
}

type Plan struct {
	mutex sync.Mutex
	items []PlanItem
}

func NewPlan() *Plan { return &Plan{} }

func (p *Plan) Name() string { return PlanToolName }

func (p *Plan) Definition() llm.Tool {
	return llm.Tool{
		Name: PlanToolName,
		Description: "states what you intend to do next, as an ordered list of short items the person watching can read. " +
			"set writes the whole list once, at the start, and after that you move one item at a time: " +
			"start marks the item you are working on, done marks it finished, drop marks it abandoned. " +
			"an item is named by its own words, never by a number, so pass the text back as it was written. " +
			"at most one item is running: starting a second one is refused and names the one already running. " +
			"use it for work of several steps, and do not use it for a single tool call",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op": map[string]any{"type": "string", "enum": []string{"set", "start", "done", "drop"}},
				"items": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"phase": map[string]any{"type": "string"},
							"text":  map[string]any{"type": "string"},
						},
						"required": []string{"text"},
					},
				},
				"item": map[string]any{"type": "string"},
			},
			"required": []string{"op"},
		},
	}
}

type planEntry struct {
	Phase string `json:"phase"`
	Text  string `json:"text"`
}

type planArgs struct {
	Op    string      `json:"op"`
	Items []planEntry `json:"items"`
	Item  string      `json:"item"`
}

func (p *Plan) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	args, err := parsePlanArgs(raw)
	if err != nil {
		return turn.Result{}, err
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	next, err := applyPlan(p.items, args)
	if err != nil {
		return turn.Result{}, err
	}
	p.items = next
	return turn.Result{Content: planText(p.items), Command: PlanToolName + " " + args.Op}, nil
}

func (p *Plan) Items() []PlanItem {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return slices.Clone(p.items)
}

func parsePlanArgs(raw json.RawMessage) (planArgs, error) {
	var args planArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return planArgs{}, fmt.Errorf("plan: arguments are not the expected shape: %w", err)
	}
	return args, nil
}

func applyPlan(items []PlanItem, args planArgs) ([]PlanItem, error) {
	if args.Op == "set" {
		return planSet(args.Items)
	}
	state, err := planStateFor(args.Op)
	if err != nil {
		return nil, err
	}
	at, err := planIndex(items, args.Item)
	if err != nil {
		return nil, err
	}
	running := slices.IndexFunc(items, func(item PlanItem) bool { return item.State == PlanRunning })
	if state == PlanRunning && running >= 0 && running != at {
		return nil, errors.New("plan: " + strconv.Quote(items[running].Text) + " is already running, and one item runs at a time. " +
			"finish it with done, or abandon it with drop, before starting another")
	}
	moved := slices.Clone(items)
	moved[at].State = state
	return moved, nil
}

func planStateFor(op string) (PlanState, error) {
	switch op {
	case "start":
		return PlanRunning, nil
	case "done":
		return PlanDone, nil
	case "drop":
		return PlanDropped, nil
	}
	return "", errors.New("plan: " + strconv.Quote(op) + " is no operation this tool has, which are set, start, done and drop")
}

func planSet(entries []planEntry) ([]PlanItem, error) {
	if len(entries) == 0 {
		return nil, errors.New("plan: set writes the whole list, so it needs at least one item")
	}
	items := make([]PlanItem, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry.Text)
		if text == "" {
			return nil, errors.New("plan: an item with no text cannot be named again, so it is refused")
		}
		if slices.ContainsFunc(items, func(held PlanItem) bool { return strings.EqualFold(held.Text, text) }) {
			return nil, errors.New("plan: two items read " + strconv.Quote(text) + ", and an item is named by its words, so neither could be started")
		}
		items = append(items, PlanItem{Phase: strings.TrimSpace(entry.Phase), Text: text, State: PlanPending})
	}
	return items, nil
}

func planIndex(items []PlanItem, text string) (int, error) {
	wanted := strings.ToLower(strings.TrimSpace(text))
	if wanted == "" {
		return -1, errors.New("plan: item is the text of the item to move, and it is missing")
	}
	for at, item := range items {
		if strings.ToLower(item.Text) == wanted {
			return at, nil
		}
	}
	found := -1
	for at, item := range items {
		if !strings.Contains(strings.ToLower(item.Text), wanted) {
			continue
		}
		if found >= 0 {
			return -1, errors.New("plan: " + strconv.Quote(text) + " reads onto more than one item, so pass the item's whole text. " + planWords(items))
		}
		found = at
	}
	if found < 0 {
		return -1, errors.New("plan: no item reads " + strconv.Quote(text) + ", and an item is named by its words rather than by a number. " + planWords(items))
	}
	return found, nil
}

func planWords(items []PlanItem) string {
	if len(items) == 0 {
		return "the plan is empty, so write it with set first"
	}
	written := make([]string, 0, len(items))
	for _, item := range items {
		written = append(written, strconv.Quote(item.Text))
	}
	return "the plan holds " + strings.Join(written, ", ")
}

func planText(items []PlanItem) string {
	lines := make([]string, 0, len(items)+1)
	lines = append(lines, "plan, "+strconv.Itoa(len(items))+" items")
	phase := ""
	for _, item := range items {
		if item.Phase != "" && item.Phase != phase {
			lines = append(lines, item.Phase)
		}
		phase = item.Phase
		lines = append(lines, "  "+string(item.State)+"  "+item.Text)
	}
	return strings.Join(lines, "\n")
}

func PlanOverSteps(steps []turn.StepRow) [][]PlanItem {
	over := make([][]PlanItem, 0, len(steps))
	var items []PlanItem
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Tool != PlanToolName || call.Error != "" {
				continue
			}
			args, err := parsePlanArgs(call.Args)
			if err != nil {
				continue
			}
			if next, err := applyPlan(items, args); err == nil {
				items = next
			}
		}
		over = append(over, slices.Clone(items))
	}
	return over
}
