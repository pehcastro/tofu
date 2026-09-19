package state

import "boji/internal/judge/ledger"

const StopCheckPoint = "stop_check"

type StopCheckCall struct {
	Tool    string `json:"tool"`
	Command string `json:"command"`
	Failed  bool   `json:"failed"`
}

type StopCheckStep struct {
	Index              int             `json:"index"`
	AssistantText      string          `json:"assistant_text"`
	StopReason         string          `json:"stop_reason"`
	ToolCalls          []StopCheckCall `json:"tool_calls"`
	RepeatsEarlierStep bool            `json:"repeats_an_earlier_step"`
}

type StopCheckBudget struct {
	AtStepCap      bool `json:"at_step_cap"`
	AtDecisionCap  bool `json:"at_decision_cap"`
	AtWallClockCap bool `json:"at_wall_clock_cap"`
}

type StopCheckState struct {
	Task        string          `json:"task"`
	RecentSteps []StopCheckStep `json:"recent_steps"`
	Budget      StopCheckBudget `json:"budget"`
}

func StopCheckVersion() string {
	return deriveVersion(StopCheckPoint, StopCheckState{})
}

func BuildStopCheck(in StopCheckState) ([]byte, string, error) {
	steps := make([]StopCheckStep, len(in.RecentSteps))
	copy(steps, in.RecentSteps)
	for i := range steps {
		if steps[i].ToolCalls == nil {
			steps[i].ToolCalls = []StopCheckCall{}
		}
	}
	in.RecentSteps = steps
	canon, err := ledger.Canonical(in)
	if err != nil {
		return nil, "", err
	}
	return canon, StopCheckVersion(), nil
}
