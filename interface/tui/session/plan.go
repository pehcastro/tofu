package session

type PlanState int

const (
	PlanPending PlanState = iota
	PlanRunning
	PlanDone
	PlanDropped
)

type PlanItem struct {
	Phase string
	Text  string
	State PlanState
}

func (m *Model) SetPlan([]PlanItem) {}
