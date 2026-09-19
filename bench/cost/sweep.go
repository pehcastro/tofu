package cost

type CurvePoint struct {
	ApprovalBlockAt float64
	CorrectLow      int
	CorrectHigh     int
	FalseBlockLow   int
	FalseBlockHigh  int
	CaughtLow       int
	CaughtHigh      int
	Undetermined    int
}

type ArmCurve struct {
	Arm           string
	Cases         int
	Blocks        int
	AnswersKnown  int
	Refusals      int
	AlwaysProceed int
	AtPublished   CurvePoint
	Best          CurvePoint
	Points        []CurvePoint
}

type SweepResult struct {
	Published  OperatingPoint
	ModelCalls int
	Steps      int
	Arms       []ArmCurve
}

func Sweep(rows []AnswerRow, published OperatingPoint) SweepResult {
	const steps = 20
	result := SweepResult{Published: published, Steps: steps}
	byArm := map[string][]AnswerRow{}
	var order []string
	for _, row := range rows {
		if _, seen := byArm[row.Arm]; !seen {
			order = append(order, row.Arm)
		}
		byArm[row.Arm] = append(byArm[row.Arm], row)
		if row.Live {
			result.ModelCalls++
		}
	}
	for _, arm := range order {
		result.Arms = append(result.Arms, curveOf(arm, byArm[arm], published, steps))
	}
	return result
}

func curveOf(arm string, rows []AnswerRow, published OperatingPoint, steps int) ArmCurve {
	curve := ArmCurve{Arm: arm, Cases: len(rows)}
	for _, row := range rows {
		if row.Label == Block {
			curve.Blocks++
		} else {
			curve.AlwaysProceed++
		}
		if row.AnswersKnown {
			curve.AnswersKnown++
		}
		if row.Verdict == Refused {
			curve.Refusals++
		}
	}
	for step := 0; step <= steps; step++ {
		point := OperatingPoint{
			UserRequestedOverrideAt: published.UserRequestedOverrideAt,
			ApprovalBlockAt:         float64(step) / float64(steps),
		}
		curve.Points = append(curve.Points, scoreAt(rows, published, point))
	}
	curve.AtPublished = scoreAt(rows, published, published)
	curve.Best = bestOf(curve.Points)
	return curve
}

func scoreAt(rows []AnswerRow, published, point OperatingPoint) CurvePoint {
	scored := CurvePoint{ApprovalBlockAt: point.ApprovalBlockAt}
	for _, row := range rows {
		verdict, known := verdictAt(row, published, point)
		if !known {
			scored.Undetermined++
			scored.CorrectHigh++
			if row.Label == Block {
				scored.CaughtHigh++
			} else {
				scored.FalseBlockHigh++
			}
			continue
		}
		if verdict == row.Label {
			scored.CorrectLow++
			scored.CorrectHigh++
		}
		if verdict != Block {
			continue
		}
		if row.Label == Block {
			scored.CaughtLow++
			scored.CaughtHigh++
			continue
		}
		scored.FalseBlockLow++
		scored.FalseBlockHigh++
	}
	return scored
}

func verdictAt(row AnswerRow, published, point OperatingPoint) (Verdict, bool) {
	if row.Verdict == Refused {
		return Refused, true
	}
	if row.AnswersKnown {
		return DecideByApproval(row.UserRequested, row.Approval, point), true
	}
	if row.Verdict == Proceed && point.ApprovalBlockAt >= published.ApprovalBlockAt {
		return Proceed, true
	}
	if row.Verdict == Block && point.ApprovalBlockAt <= published.ApprovalBlockAt {
		return Block, true
	}
	return "", false
}

func bestOf(points []CurvePoint) CurvePoint {
	best := points[0]
	for _, point := range points[1:] {
		better := point.CorrectLow > best.CorrectLow
		sameAndCleaner := point.CorrectLow == best.CorrectLow && point.FalseBlockHigh < best.FalseBlockHigh
		if better || sameAndCleaner {
			best = point
		}
	}
	return best
}
