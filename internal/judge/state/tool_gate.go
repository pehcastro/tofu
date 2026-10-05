package state

import "tofu/internal/judge/ledger"

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
	Agent          string
	Tool           string
	Input          map[string]any
	Cwd            string
	ProjectDir     string
	ScratchDir     string
	InputTruncated bool
	Context        ToolGateContext
}

type toolGateV3Context struct {
	UserRecentMessages      []string         `json:"user_recent_messages"`
	FlaggedUntrustedContent *ToolGateFlagged `json:"flagged_untrusted_content"`
	WriteTargets            WriteTargets     `json:"write_targets"`
}

type toolGateV3Envelope struct {
	Agent   string            `json:"agent"`
	Tool    string            `json:"tool"`
	Input   map[string]any    `json:"input"`
	Cwd     string            `json:"cwd"`
	Context toolGateV3Context `json:"context"`
}

func ToolGateV3Version() string {
	return deriveVersion(ToolGatePoint, toolGateV3Envelope{})
}

func BuildToolGateV3(in ToolGateInput) ([]byte, string, error) {
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	messages := in.Context.UserRecentMessages
	if messages == nil {
		messages = []string{}
	}
	canon, err := ledger.Canonical(toolGateV3Envelope{
		Agent: in.Agent, Tool: in.Tool, Input: in.Input, Cwd: in.Cwd,
		Context: toolGateV3Context{
			UserRecentMessages:      messages,
			FlaggedUntrustedContent: in.Context.FlaggedUntrustedContent,
			WriteTargets:            TargetsOf(in),
		},
	})
	if err != nil {
		return nil, "", err
	}
	return canon, ToolGateV3Version(), nil
}
