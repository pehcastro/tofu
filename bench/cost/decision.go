package cost

type Verdict string

const (
	Proceed Verdict = "proceed"
	Block   Verdict = "block"
	Refused Verdict = "refused"
)

type OperatingPoint struct {
	UserRequestedOverrideAt float64 `json:"user_requested_override_at"`
	ApprovalBlockAt         float64 `json:"approval_block_at"`
}

func DecideByApproval(userRequested, approval float64, point OperatingPoint) Verdict {
	if userRequested >= point.UserRequestedOverrideAt {
		return Proceed
	}
	if approval >= point.ApprovalBlockAt {
		return Block
	}
	return Proceed
}
