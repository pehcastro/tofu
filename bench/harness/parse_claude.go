package harness

import "encoding/json"

type ClaudeMeta = RunMeta

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type claudeTranscript struct {
	DurationMS   int64                      `json:"duration_ms"`
	TotalCostUSD float64                    `json:"total_cost_usd"`
	NumTurns     int64                      `json:"num_turns"`
	Usage        claudeUsage                `json:"usage"`
	ModelUsage   map[string]json.RawMessage `json:"modelUsage"`
	Subtype      string                     `json:"subtype"`
	IsError      bool                       `json:"is_error"`
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
	}

	row.EndReason = EndReasonDone
	if t.IsError || t.Subtype != "success" {
		row.EndReason = EndReasonCrash
		gaps = append(gaps, "end reason inferred from subtype \""+t.Subtype+"\", the mapping only covers success, the only subtype seen so far")
	}

	gaps = append(gaps,
		"start/end timestamps: this transcript reports duration_ms only, not an absolute start or end time",
		"commit: not present in this transcript, must come from the caller after the run",
	)

	return row, gaps, nil
}
