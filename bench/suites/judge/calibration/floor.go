package calibration

import "math"

const BudgetFivePercent = 0.05

const BudgetOnePercent = 0.01

func RuleOfThree(falsePositiveBudget float64) int {
	return int(math.Ceil(3 / falsePositiveBudget))
}

func Further(have, floor int) int {
	if have >= floor {
		return 0
	}
	return floor - have
}
