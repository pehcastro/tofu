package connector

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"boji/internal/konst"
	"boji/internal/transport"
)

type StopReason int

const (
	StopModelStopped StopReason = iota
	StopCapReached
	StopPermissionDenied
	StopQuotaClosed
	StopProcessFailed
)

func (s StopReason) String() string {
	switch s {
	case StopModelStopped:
		return "model_stopped"
	case StopCapReached:
		return "cap_reached"
	case StopPermissionDenied:
		return "permission_denied"
	case StopQuotaClosed:
		return "quota_closed"
	case StopProcessFailed:
		return "process_failed"
	}
	panic("connector: unknown stop reason")
}

type Transcript struct {
	Stop         StopReason
	SessionID    string
	CallID       string
	Model        string
	Text         string
	Structured   json.RawMessage
	ListCostUSD  float64
	InputTokens  int
	OutputTokens int
	Denied       []string
	Detail       string
}

type streamUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type streamRateLimit struct {
	Status string `json:"status"`
}

type streamDenial struct {
	ToolName string `json:"tool_name"`
}

type streamModelUsage struct {
	CanonicalModel string `json:"canonicalModel"`
}

type streamEvent struct {
	Type           string                      `json:"type"`
	Subtype        string                      `json:"subtype"`
	SessionID      string                      `json:"session_id"`
	UUID           string                      `json:"uuid"`
	Model          string                      `json:"model"`
	RateLimit      *streamRateLimit            `json:"rate_limit_info"`
	TerminalReason string                      `json:"terminal_reason"`
	IsError        bool                        `json:"is_error"`
	APIErrorStatus json.RawMessage             `json:"api_error_status"`
	Result         string                      `json:"result"`
	Structured     json.RawMessage             `json:"structured_output"`
	TotalCostUSD   float64                     `json:"total_cost_usd"`
	Usage          streamUsage                 `json:"usage"`
	ModelUsage     map[string]streamModelUsage `json:"modelUsage"`
	Denials        []streamDenial              `json:"permission_denials"`
	Errors         []string                    `json:"errors"`
}

func ReadTranscript(stream io.Reader) (Transcript, error) {
	var transcript Transcript
	var result *streamEvent
	throttled := false

	lines := bufio.NewScanner(stream)
	lines.Buffer(make([]byte, 0, konst.ConnectorLineBytes), konst.ConnectorLineBytes)
	for lines.Scan() {
		raw := strings.TrimSpace(lines.Text())
		if raw == "" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return Transcript{}, transport.Fail("connector.ReadTranscript", transport.KindInvalidAnswer, err,
				"the claude event stream carries a line that is not json")
		}
		switch {
		case event.Type == "system" && event.Subtype == "init":
			transcript.SessionID, transcript.Model = event.SessionID, event.Model
		case event.Type == "rate_limit_event" && event.RateLimit != nil && event.RateLimit.Status != "allowed":
			throttled = true
		case event.Type == "result":
			copied := event
			result = &copied
		}
	}
	if err := lines.Err(); err != nil {
		return Transcript{}, transport.Fail("connector.ReadTranscript", transport.KindProvider, err,
			"reading the claude event stream")
	}

	if result == nil {
		transcript.Stop, transcript.Detail = StopProcessFailed, "the claude subprocess ended without a result event"
		return transcript, nil
	}

	transcript.CallID = result.UUID
	transcript.Text = result.Result
	transcript.Structured = result.Structured
	transcript.ListCostUSD = result.TotalCostUSD
	transcript.InputTokens, transcript.OutputTokens = result.Usage.InputTokens, result.Usage.OutputTokens
	if result.SessionID != "" {
		transcript.SessionID = result.SessionID
	}
	for _, usage := range result.ModelUsage {
		if usage.CanonicalModel != "" {
			transcript.Model = usage.CanonicalModel
		}
	}
	for _, denial := range result.Denials {
		transcript.Denied = append(transcript.Denied, denial.ToolName)
	}
	transcript.Detail = strings.Join(result.Errors, "; ")
	if transcript.Detail == "" {
		transcript.Detail = result.Subtype
	}

	switch {
	case throttled || apiErrored(result.APIErrorStatus):
		transcript.Stop = StopQuotaClosed
	case result.TerminalReason == "budget_exhausted" || strings.HasPrefix(result.Subtype, "error_max"):
		transcript.Stop = StopCapReached
	case len(transcript.Denied) > 0:
		transcript.Stop = StopPermissionDenied
	case result.TerminalReason == "completed" && !result.IsError:
		transcript.Stop = StopModelStopped
	default:
		transcript.Stop = StopProcessFailed
	}
	return transcript, nil
}

func apiErrored(status json.RawMessage) bool {
	text := strings.TrimSpace(string(status))
	return text != "" && text != "null"
}
