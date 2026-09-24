package unit

const NeededQuestion = "still_needed"

type Mark struct {
	Keep   bool
	Reason string
}

func Position(index, total int) string {
	switch index {
	case 0:
		return "opening"
	case total - 1:
		return "closing"
	}
	return "middle"
}
