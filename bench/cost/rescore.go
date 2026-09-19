package cost

import (
	"fmt"

	"boji/internal/judge/policy"
)

type RescoreCount struct {
	Arm             string
	Rows            int
	Rescored        int
	PartialAnswers  int
	VerdictOnly     int
	Refusals        int
	Changed         int
	CorrectRecorded int
	CorrectRescored int
}

type RescoreResult struct {
	Policy     policy.Policy
	Resolution policy.Resolution
	ModelCalls int
	Arms       []RescoreCount
}

func Rescore(rows []AnswerRow, pol policy.Policy, resolution policy.Resolution) (RescoreResult, error) {
	result := RescoreResult{Policy: pol, Resolution: resolution}
	counts := map[string]*RescoreCount{}
	var order []string
	for _, row := range rows {
		if row.Live {
			result.ModelCalls++
		}
		count, seen := counts[row.Arm]
		if !seen {
			count = &RescoreCount{Arm: row.Arm}
			counts[row.Arm] = count
			order = append(order, row.Arm)
		}
		count.Rows++
		if row.Verdict == row.Label {
			count.CorrectRecorded++
		}
		answers, complete := rowAnswers(pol, row)
		switch {
		case complete:
			verdict, _, err := decide(answers, pol)
			if err != nil {
				return RescoreResult{}, fmt.Errorf("%s case %s: %w", row.Arm, row.Case, err)
			}
			count.Rescored++
			if verdict != row.Verdict {
				count.Changed++
			}
			if verdict == row.Label {
				count.CorrectRescored++
			}
		case row.Verdict == Refused:
			count.Refusals++
		case row.AnswersKnown:
			count.PartialAnswers++
		default:
			count.VerdictOnly++
		}
	}
	for _, arm := range order {
		result.Arms = append(result.Arms, *counts[arm])
	}
	return result, nil
}
