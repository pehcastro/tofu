package turn

import (
	"context"
	"encoding/json"
)

type GateDecision struct {
	ID      string
	Verdict string
}

type Gate interface {
	Decide(ctx context.Context, tool string, args json.RawMessage, cwd string) (GateDecision, error)
}
