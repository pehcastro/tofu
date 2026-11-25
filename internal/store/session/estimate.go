package session

import (
	"encoding/json"
	"fmt"
)

type EstimateScope string

const (
	EstimateTurn  EstimateScope = "turn"
	EstimateAgent EstimateScope = "agent"
	EstimateCall  EstimateScope = "call"
)

type EstimateBasis string

const (
	EstimateFromHistory EstimateBasis = "history"
	EstimateFirstRun    EstimateBasis = "first_run"
	EstimateNoHistory   EstimateBasis = "no_history"
)

type EstimateMatch string

const (
	MatchAgentModel EstimateMatch = "agent_model"
	MatchAgent      EstimateMatch = "agent"
	MatchModel      EstimateMatch = "model"
)

type EstimateOutcome string

const (
	EstimateDone    EstimateOutcome = "done"
	EstimateKilled  EstimateOutcome = "killed"
	EstimateStopped EstimateOutcome = "stopped"
)

type EstimateBody struct {
	Scope       EstimateScope `json:"scope"`
	Agent       string        `json:"agent,omitempty"`
	Definition  string        `json:"definition,omitempty"`
	Basis       EstimateBasis `json:"basis"`
	Match       EstimateMatch `json:"match,omitempty"`
	SaidMS      int64         `json:"said_ms"`
	DoneByMS    int64         `json:"done_by_ms"`
	AgentMS     int64         `json:"agent_ms"`
	TestMS      int64         `json:"test_ms"`
	BuildMS     int64         `json:"build_ms"`
	VerifyMS    int64         `json:"verify_ms"`
	ElapsedMS   int64         `json:"elapsed_ms,omitempty"`
	Model       string        `json:"model"`
	Project     string        `json:"project"`
	PromptChars int           `json:"prompt_chars"`
	Runs        int           `json:"runs"`
	SizedRuns   int           `json:"sized_runs,omitempty"`
	TestRuns    int           `json:"test_runs"`
	BuildRuns   int           `json:"build_runs"`
	VerifyRuns  int           `json:"verify_runs"`
	LastedRuns  int           `json:"lasted_runs,omitempty"`
	LongestMS   int64         `json:"longest_ms,omitempty"`
}

type EstimateSplit struct {
	ModelMS      int64 `json:"model_ms"`
	ToolsMS      int64 `json:"tools_ms"`
	TestMS       int64 `json:"test_ms"`
	BuildMS      int64 `json:"build_ms"`
	VerifyMS     int64 `json:"verify_ms"`
	CheckCalls   int   `json:"test_calls"`
	PermissionMS int64 `json:"permission_ms"`
	QueueMS      int64 `json:"queue_ms"`
}

type EstimateSettledBody struct {
	Estimate string          `json:"estimate"`
	Outcome  EstimateOutcome `json:"outcome"`
	TookMS   int64           `json:"took_ms"`
	EstimateSplit
}

type Estimated struct {
	Event Event
	EstimateBody
	Settled *EstimateSettledBody
}

type Promise int

const (
	PromiseOpen Promise = iota
	PromiseNone
	PromiseKept
	PromiseLate
)

func (e Estimated) Promise() (Promise, int64) {
	switch {
	case e.Settled == nil:
		return PromiseOpen, 0
	case e.Basis == EstimateNoHistory:
		return PromiseNone, 0
	}
	late := e.Settled.TookMS - e.Settled.PermissionMS - e.DoneByMS
	switch {
	case late > 0:
		return PromiseLate, late
	case e.Settled.Outcome != EstimateDone:
		return PromiseNone, 0
	}
	return PromiseKept, 0
}

func Estimates(events []Event) ([]Estimated, error) {
	var estimated []Estimated
	at := map[string]int{}
	for _, event := range events {
		switch event.Kind {
		case EventEstimate:
			one := Estimated{Event: event}
			if err := json.Unmarshal(event.Body, &one.EstimateBody); err != nil {
				return nil, fmt.Errorf("estimate %s does not parse: %w", event.ID, err)
			}
			at[event.ID] = len(estimated)
			estimated = append(estimated, one)
		case EventEstimateSettled:
			var settled EstimateSettledBody
			if err := json.Unmarshal(event.Body, &settled); err != nil {
				return nil, fmt.Errorf("settled estimate %s does not parse: %w", event.ID, err)
			}
			if i, found := at[settled.Estimate]; found {
				estimated[i].Settled = &settled
			}
		}
	}
	return estimated, nil
}
