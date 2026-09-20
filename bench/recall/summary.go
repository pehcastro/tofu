package recall

import (
	"encoding/json"
	"fmt"
	"strings"

	rc "tofu/internal/recall"
	"tofu/internal/sys"
)

type Summary struct {
	Step         int     `json:"step"`
	Text         string  `json:"text"`
	Model        string  `json:"model"`
	Spend        string  `json:"spend"`
	LatencyMS    int64   `json:"latency_ms"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type Summaries struct {
	Session string    `json:"session"`
	Target  int       `json:"target"`
	Calls   []Summary `json:"calls"`
}

func ReadSummaries(path string) (Summaries, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Summaries{}, err
	}
	var recorded Summaries
	if err := json.Unmarshal(data, &recorded); err != nil {
		return Summaries{}, fmt.Errorf("recall: %s is not a recorded set of fork summaries: %w", path, err)
	}
	if len(recorded.Calls) == 0 {
		return Summaries{}, fmt.Errorf("recall: %s records no summary call", path)
	}
	return recorded, nil
}

func (s Summaries) Carry() CarryBuilder {
	used := 0
	return func(*rc.Store, rc.Config, rc.Conversation) (rc.Carry, error) {
		if used >= len(s.Calls) {
			return rc.Carry{}, fmt.Errorf("recall: %d summaries were recorded and the replay reached fork %d: record the missing one rather than reusing another", len(s.Calls), used+1)
		}
		used++
		return rc.Carry{Text: s.Calls[used-1].Text}, nil
	}
}

func (s Summaries) TotalCostUSD() float64 {
	total := 0.0
	for _, call := range s.Calls {
		total += call.CostUSD
	}
	return total
}

func Transcript(c rc.Conversation) string {
	var text strings.Builder
	text.WriteString(c.Instructions)
	for _, entry := range c.Entries {
		fmt.Fprintf(&text, "\nstep %d %s\n%s\n", entry.Step, entry.Tool, entry.Text)
	}
	return text.String()
}
