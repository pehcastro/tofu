package state

import "boji/internal/judge/ledger"

const ToolGatePoint = "tool_gate"

type ToolGateFlagged struct {
	Source  string `json:"source"`
	Excerpt string `json:"excerpt"`
}

type ToolGateContext struct {
	UserRecentMessages      []string         `json:"user_recent_messages"`
	FlaggedUntrustedContent *ToolGateFlagged `json:"flagged_untrusted_content"`
}

type ToolGateInput struct {
	Agent   string
	Tool    string
	Input   map[string]any
	Cwd     string
	Context ToolGateContext
}

type toolGateEnvelope struct {
	Agent   string          `json:"agent"`
	Tool    string          `json:"tool"`
	Input   map[string]any  `json:"input"`
	Cwd     string          `json:"cwd"`
	Context ToolGateContext `json:"context"`
}

func ToolGateVersion() string {
	return deriveVersion(ToolGatePoint, toolGateEnvelope{})
}

func BuildToolGate(in ToolGateInput) ([]byte, string, error) {
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	if in.Context.UserRecentMessages == nil {
		in.Context.UserRecentMessages = []string{}
	}
	canon, err := ledger.Canonical(toolGateEnvelope(in))
	if err != nil {
		return nil, "", err
	}
	return canon, ToolGateVersion(), nil
}
