package harness

type Tier string

const (
	TierAlwaysPasses  Tier = "always_passes"
	TierUsuallyPasses Tier = "usually_passes"
	TierUsuallyFails  Tier = "usually_fails"
)

const usuallyPassesFloor = 0.5

func Passed(row Row) bool {
	gatesOK, _ := evaluateGates(row)
	checklistOK, _ := checklistFull(row)
	return gatesOK && checklistOK
}

func PassCount(rows []Row) int {
	passed := 0
	for _, r := range rows {
		if Passed(r) {
			passed++
		}
	}
	return passed
}

func TierOf(rows []Row) Tier {
	passed := PassCount(rows)
	switch {
	case len(rows) > 0 && passed == len(rows):
		return TierAlwaysPasses
	case float64(passed) > usuallyPassesFloor*float64(len(rows)):
		return TierUsuallyPasses
	default:
		return TierUsuallyFails
	}
}
