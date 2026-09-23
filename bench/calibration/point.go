package calibration

import "tofu/internal/judge/ledger"

type Threshold struct {
	Name  string
	Value float64
}

type Point struct {
	Name       string
	Question   string
	Rule       string
	Thresholds []Threshold
}

func Points() []Point {
	return []Point{
		{
			Name:     "tool_gate",
			Question: "risk",
			Rule:     "library/general/rules/tool_gate@6.yaml",
			Thresholds: []Threshold{
				{Name: "risk_ask_at", Value: 1.5},
				{Name: "risk_deny_at", Value: 2.5},
			},
		},
		{
			Name:     "stop_check",
			Question: "risk",
			Rule:     "library/general/rules/stop_check@1.yaml",
			Thresholds: []Threshold{
				{Name: "stop_pressure_ask_at", Value: 1.5},
				{Name: "stop_pressure_deny_at", Value: 2.5},
			},
		},
		{
			Name:     "ask",
			Question: "determined",
			Rule:     "library/general/rules/ask@1.yaml",
			Thresholds: []Threshold{
				{Name: "determined_low_at", Value: 0.35},
			},
		},
		{
			Name:     "shell_sift",
			Question: "still_needed",
			Rule:     "library/tools/shell/rules/shell_sift@1.yaml",
			Thresholds: []Threshold{
				{Name: "keep_at", Value: 0.5},
			},
		},
	}
}

func valueFor(row ledger.Row, question string) (float64, bool) {
	for _, answer := range row.Answers {
		if answer.Question != question {
			continue
		}
		switch answer.Kind {
		case ledger.AnswerScore:
			return answer.Score, true
		case ledger.AnswerNoul:
			return answer.Noul, true
		}
	}
	return 0, false
}
