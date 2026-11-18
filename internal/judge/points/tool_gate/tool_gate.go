package toolgate

import (
	"maps"
	"slices"
	"unicode/utf8"

	"tofu/internal/judge/ledger"
	"tofu/internal/judge/points"
)

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
	return points.DeriveVersion(ToolGatePoint, toolGateV3Envelope{})
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

const elidedMark = " [elided to fit the judge] "

func ShortenLongest(in ToolGateInput, by int) (ToolGateInput, bool) {
	at, size := 0, 0
	for i, text := range in.Context.UserRecentMessages {
		if len(text) > size {
			at, size = i, len(text)
		}
	}
	if size > 0 {
		in.Context.UserRecentMessages = slices.Clone(in.Context.UserRecentMessages)
		in.Context.UserRecentMessages[at] = cutMiddle(in.Context.UserRecentMessages[at], by)
		return in, true
	}
	field := ""
	for key, value := range in.Input {
		if text, ok := value.(string); ok && len(text) > size {
			field, size = key, len(text)
		}
	}
	if size == 0 {
		return in, false
	}
	in.Input = maps.Clone(in.Input)
	in.Input[field] = cutMiddle(in.Input[field].(string), by)
	in.InputTruncated = true
	return in, true
}

func cutMiddle(text string, by int) string {
	keep := len(text) - by - len(elidedMark)
	if keep <= 0 {
		return ""
	}
	head, tail := keep/2, len(text)-(keep-keep/2)
	for head > 0 && !utf8.RuneStart(text[head]) {
		head--
	}
	for tail < len(text) && !utf8.RuneStart(text[tail]) {
		tail++
	}
	return text[:head] + elidedMark + text[tail:]
}
