package turn

import (
	"context"
	"encoding/json"
)

type GateRequest struct {
	TurnID string
	Task   string
	Tool   string
	Args   json.RawMessage
}

type GateDecision struct {
	ID      string
	Verdict string
}

type Gate interface {
	Decide(ctx context.Context, request GateRequest) (GateDecision, error)
}
