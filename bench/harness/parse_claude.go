package harness

import (
	"encoding/json"
	"fmt"
)

type ClaudeMeta = RunMeta

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type claudeTranscript struct {
	DurationMS       int64                      `json:"duration_ms"`
	TotalCostUSD     float64                    `json:"total_cost_usd"`
	NumTurns         int64                      `json:"num_turns"`
	Usage            claudeUsage                `json:"usage"`
	ModelUsage       map[string]json.RawMessage `json:"modelUsage"`
	Subtype          string                     `json:"subtype"`
	TerminalReason   string                     `json:"terminal_reason"`
	IsError          bool                       `json:"is_error"`
	PermissionDenied []json.RawMessage          `json:"permission_denials"`
}

func ParseClaude(data []byte, meta ClaudeMeta) (Row, []string, error) {
	var t claudeTranscript
	if err := json.Unmarshal(data, &t); err != nil {
		return Row{}, nil, err
	}

	row := Row{
		Arm:            meta.Arm,
		Task:           meta.Task,
		Version:        meta.Version,
		Run:            meta.Run,
		CLIVersion:     meta.CLIVersion,
		CredentialKind: meta.CredentialKind,
		Commit:         meta.Commit,
		Setup:          meta.Setup,
		WallClockMS:    t.DurationMS,
		BilledInput:    t.Usage.InputTokens + t.Usage.CacheCreationInputTokens + t.Usage.CacheReadInputTokens,
		BilledOutput:   t.Usage.OutputTokens,
		Turns:          t.NumTurns,
	}

	var gaps []string

	for model := range t.ModelUsage {
		row.Model = model
	}
	if row.Model == "" {
		gaps = append(gaps, "model: not present in this transcript")
	}
	if len(t.ModelUsage) > 1 {
		gaps = append(gaps, "model: transcript reports more than one model in modelUsage, only one was kept")
	}

	if meta.CredentialKind == CredentialKindKey {
		row.ModelDollars = &t.TotalCostUSD
	} else if t.TotalCostUSD > 0 {
		gaps = append(gaps, fmt.Sprintf(
			"model dollars: this run spent subscription quota, and the %.4f dollar total_cost_usd the transcript reports is the list price of the same tokens, not money that left the account, so it is not in the row",
			t.TotalCostUSD))
	}

	row.EndReason, gaps = claudeEndReason(t, gaps)

	gaps = append(gaps,
		fmt.Sprintf("billed input tokens: %d of the %d are cache reads and %d are cache creation, and the row adds all three, so this number is not comparable with an arm that reports fresh input only",
			t.Usage.CacheReadInputTokens, row.BilledInput, t.Usage.CacheCreationInputTokens),
		"tool calls: --output-format json carries no per-tool counts, so every tool call field on this row is zero because it was never measured, not because the arm used no tools. --output-format stream-json would carry them",
		"start/end timestamps: this transcript reports duration_ms only, so the runner supplies the start and derives the end from it",
		"commit: not present in this transcript, must come from the caller after the run",
	)
	if len(t.PermissionDenied) > 0 {
		gaps = append(gaps, fmt.Sprintf("permissions: the transcript records %d permission denials, and Row has no field for them", len(t.PermissionDenied)))
	}

	return row, gaps, nil
}

func claudeEndReason(t claudeTranscript, gaps []string) (EndReason, []string) {
	switch t.TerminalReason {
	case "completed":
		if t.IsError || t.Subtype != "success" {
			return EndReasonCrash, append(gaps, fmt.Sprintf(
				"end reason: terminal_reason says completed but is_error is %t and subtype is %q, so this row says crash", t.IsError, t.Subtype))
		}
		return EndReasonDone, gaps
	case "":
		return EndReasonCrash, append(gaps, "end reason: this transcript carries no terminal_reason, so this row says crash")
	}
	return EndReasonCrash, append(gaps, fmt.Sprintf(
		"end reason: terminal_reason is %q, and the only value mapped so far is completed, so this row says crash", t.TerminalReason))
}
