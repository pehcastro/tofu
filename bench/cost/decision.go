package cost

type Verdict string

const (
	Proceed Verdict = "proceed"
	Block   Verdict = "block"
	Refused Verdict = "refused"
)

const decisionThreshold = 0.5

func Decide(userRequested, approval float64) Verdict {
	if userRequested >= decisionThreshold {
		return Proceed
	}
	if approval >= decisionThreshold {
		return Block
	}
	return Proceed
}
