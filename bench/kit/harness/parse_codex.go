package harness

import (
	"bytes"
	"encoding/json"
	"time"
)

type CodexMeta struct {
	RunMeta
	Model string
	Start time.Time
	End   time.Time
}

type codexUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

type codexError struct {
	Message string `json:"message"`
}

type codexEvent struct {
	Type    string      `json:"type"`
	Message string      `json:"message"`
	Error   *codexError `json:"error"`
	Usage   *codexUsage `json:"usage"`
}

func ParseCodex(data []byte, meta CodexMeta) (Row, []string, error) {
	row := Row{
		Arm:            meta.Arm,
		Task:           meta.Task,
		Version:        meta.Version,
		Run:            meta.Run,
		CLIVersion:     meta.CLIVersion,
		CredentialKind: meta.CredentialKind,
		Commit:         meta.Commit,
		Setup:          meta.Setup,
		Seed:           meta.Seed,
		Effort:         meta.Effort,
		Model:          meta.Model,
		Start:          meta.Start,
		End:            meta.End,
	}
	if !meta.Start.IsZero() && !meta.End.IsZero() {
		row.WallClockMS = meta.End.Sub(meta.Start).Milliseconds()
	}

	var gaps []string
	var turnsStarted int64
	var completed bool
	var sawUsage bool
	var lastErr string

	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return Row{}, nil, err
		}
		switch ev.Type {
		case "turn.started":
			turnsStarted++
		case "turn.completed":
			completed = true
			if ev.Usage != nil {
				sawUsage = true
				row.BilledInput = ev.Usage.InputTokens + ev.Usage.CachedInputTokens
				row.BilledOutput = ev.Usage.OutputTokens
			}
		case "turn.failed", "error":
			if ev.Error != nil {
				lastErr = ev.Error.Message
			} else if ev.Message != "" {
				lastErr = ev.Message
			}
		}
	}
	row.Turns = turnsStarted

	switch {
	case completed:
		row.EndReason = EndReasonDone
	case lastErr != "":
		row.EndReason = EndReasonCrash
		gaps = append(gaps, "end reason: codex reported an error before any turn completed: "+lastErr)
	default:
		row.EndReason = EndReasonCrash
	}

	if !sawUsage {
		gaps = append(gaps, "billed input/output tokens: no turn.completed event with a usage block in this transcript")
	}
	if row.Model == "" {
		gaps = append(gaps, "model: not present in this transcript, must come from the caller")
	}
	gaps = append(gaps, "dollars: codex exec --json emits no cost figure for either credential kind in this transcript")

	return row, gaps, nil
}
