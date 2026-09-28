package jevloop

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/browser"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
)

const operationQuestion = "operation"

const stateBuilder = "jevloop.stepState@1"

type Choice struct {
	Action   browser.Action
	Decision jev.Decision
}

type Chooser func(ctx context.Context, goal string, page browser.Page, steps []Step) (Choice, error)

type Jev struct {
	Client *jev.Client
	Set    question.Set
	Ledger *ledger.Writer
}

type pageState struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type recentAction struct {
	Action      string `json:"action"`
	Op          string `json:"op"`
	Value       string `json:"value,omitempty"`
	PageChanged bool   `json:"page_changed"`
	Stale       bool   `json:"stale,omitempty"`
}

type stepState struct {
	Goal          string            `json:"goal"`
	Page          pageState         `json:"page"`
	Elements      []browser.Element `json:"elements"`
	RecentActions []recentAction    `json:"recent_actions"`
}

type targetCriteria struct {
	Element      string       `json:"element"`
	CurrentValue string       `json:"current_value"`
	Role         browser.Role `json:"role"`
	Checked      string       `json:"checked,omitempty"`
	Selected     string       `json:"selected,omitempty"`
	Expanded     string       `json:"expanded,omitempty"`
}

func targetQuestion(op browser.Op) string {
	return strings.ToLower(op.String()) + "_target"
}

func (j Jev) Choose(ctx context.Context, goal string, page browser.Page, steps []Step) (Choice, error) {
	operation, hasOperation := j.Set.Question(operationQuestion)
	target, hasTarget := j.Set.Question("target")
	if !hasOperation || !hasTarget {
		return Choice{}, fmt.Errorf("%s asks no operation or no target question", j.Set.Name)
	}

	recent := steps[max(0, len(steps)-konst.BrowserRecentSteps):]
	state := stepState{
		Goal:          goal,
		Page:          pageState{URL: page.URL, Title: page.Title, Text: page.Text},
		Elements:      page.Elements,
		RecentActions: make([]recentAction, 0, len(recent)),
	}
	for _, step := range recent {
		state.RecentActions = append(state.RecentActions, recentAction{
			Action: step.Label, Op: step.Action.Op.String(), Value: step.Action.Value, PageChanged: step.Changed, Stale: step.Stale,
		})
	}

	ops := operation.ToJev()
	request := jev.Request{State: state, Questions: []jev.Question{ops}}
	targets := map[browser.Op]map[string]browser.Action{}
	var offered []jev.Option
	for _, option := range ops.Options {
		op, err := browser.ParseOp(option.Name)
		if err != nil {
			return Choice{}, err
		}
		switch op {
		case browser.OpScrollUp:
			if !page.Scroll.Up {
				continue
			}
		case browser.OpScrollDown:
			if !page.Scroll.Down {
				continue
			}
		case browser.OpClick, browser.OpTypeText, browser.OpSelect:
			options, actions := candidates(op, page)
			if len(options) == 0 {
				continue
			}
			if len(options) > konst.ChoiceCeiling {
				return Choice{}, fmt.Errorf("%s offers %d targets and a question holds at most %d", op, len(options), konst.ChoiceCeiling)
			}
			targets[op] = actions
			request.Questions = append(request.Questions, jev.Question{
				ID:           targetQuestion(op),
				Kind:         jev.QuestionChoice,
				Instructions: operation.Instructions + "\n\n" + target.Instructions + "\n\nOperation: " + op.String(),
				Options:      options,
			})
		case browser.OpWait, browser.OpDone, browser.OpBlocked:
		}
		offered = append(offered, option)
	}
	request.Questions[0].Options = offered

	decision, err := j.Client.Ask(ctx, request)
	if err != nil {
		return Choice{}, err
	}
	if err := j.record(state, decision); err != nil {
		return Choice{}, fmt.Errorf("the decision was not logged, so it does not run: %w", err)
	}
	op, err := browser.ParseOp(decision.Answers[operationQuestion].Choice)
	if err != nil {
		return Choice{}, err
	}
	action := browser.Action{Op: op}
	if actions, targeted := targets[op]; targeted {
		action = actions[decision.Answers[targetQuestion(op)].Choice]
	}
	return Choice{Action: action, Decision: decision}, nil
}

func (j Jev) record(state stepState, decision jev.Decision) error {
	if j.Ledger == nil {
		return nil
	}
	body, err := ledger.Canonical(state)
	if err != nil {
		return err
	}
	answers := make([]ledger.Answer, 0, len(decision.Answers))
	for _, id := range slices.Sorted(maps.Keys(decision.Answers)) {
		answer := decision.Answers[id]
		dist := make([]ledger.Slice, 0, len(answer.Probabilities))
		for _, option := range slices.Sorted(maps.Keys(answer.Probabilities)) {
			dist = append(dist, ledger.Slice{Option: option, P: answer.Probabilities[option]})
		}
		answers = append(answers, ledger.Answer{Question: id, Wording: j.Set.QuestionsVersion, Kind: ledger.AnswerChoice, Choice: answer.Choice, Dist: dist})
	}
	_, err = j.Ledger.Append(ledger.Row{
		Point: j.Set.Name, Questions: j.Set.Name, Version: j.Set.QuestionsVersion,
		Build: decision.Build, Model: decision.Alias, RequestID: decision.RequestID,
		StateHash: ledger.HashOf(body), StateBuilder: stateBuilder, State: body, Answers: answers,
		LatencyMS: decision.Latency.Milliseconds(), Cost: decision.Usage.Cost,
	})
	return err
}

func candidates(op browser.Op, page browser.Page) ([]jev.Option, map[string]browser.Action) {
	var options []jev.Option
	actions := map[string]browser.Action{}
	offer := func(name, label, value string, element browser.Element) {
		options = append(options, jev.Option{Name: name, Criteria: targetCriteria{
			Element: "[" + name + "] " + label, CurrentValue: element.Value, Role: element.Role,
			Checked: element.Checked, Selected: element.Selected, Expanded: element.Expanded,
		}})
		actions[name] = browser.Action{Op: op, Element: element.Index, Value: value}
	}
	for _, element := range page.Elements {
		if !op.Accepts(element.Role) || op == browser.OpTypeText && element.ReadOnly {
			continue
		}
		if op != browser.OpSelect {
			offer(strconv.Itoa(element.Index), element.Label, "", element)
			continue
		}
		for position, option := range element.Options {
			offer(fmt.Sprintf("%d:%d", element.Index, position+1), element.Label+": "+option.Label, option.Value, element)
		}
	}
	return options, actions
}
