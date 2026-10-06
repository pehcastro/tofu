package session

import "tofu/internal/host"

type PlanState = host.PlanState

const (
	PlanPending = host.PlanPending
	PlanRunning = host.PlanRunning
	PlanDone    = host.PlanDone
	PlanDropped = host.PlanDropped
)

type PlanItem = host.PlanItem

func (m *Model) SetPlan([]PlanItem) {}
