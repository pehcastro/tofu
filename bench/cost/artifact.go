package cost

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"tofu/internal/judge/gate"
)

const recordedAnswersPath = "bench/cost/answers/heldout-2026-09-19.jsonl"

//go:embed answers/heldout-2026-09-19.jsonl
var recordedAnswersFile []byte

type AnswerRow struct {
	Case          string   `json:"case"`
	Arm           string   `json:"arm"`
	Label         Verdict  `json:"label"`
	Verdict       Verdict  `json:"verdict"`
	Risk          *float64 `json:"risk,omitempty"`
	Approval      float64  `json:"approval"`
	UserRequested float64  `json:"user_requested"`
	FromUntrusted *float64 `json:"from_untrusted,omitempty"`
	AnswersKnown  bool     `json:"answers_known"`
	Live          bool     `json:"live"`
	Source        string   `json:"source"`
}

func parseAnswers(data []byte, path string) ([]AnswerRow, error) {
	var rows []AnswerRow
	for index, text := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(text)) == 0 {
			continue
		}
		line := index + 1
		var row AnswerRow
		if err := json.Unmarshal(text, &row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		if row.Label != Proceed && row.Label != Block {
			return nil, fmt.Errorf("%s line %d: label %q is neither proceed nor block", path, line, row.Label)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func RecordedAnswers() ([]AnswerRow, error) {
	return parseAnswers(recordedAnswersFile, recordedAnswersPath)
}

func answerRows(arms []ArmResult, pol gate.Rule, source string) []AnswerRow {
	var rows []AnswerRow
	for _, arm := range arms {
		for _, c := range arm.Cases {
			row := AnswerRow{
				Case: c.Case, Arm: arm.Arm, Label: c.Label, Verdict: c.Verdict,
				Live: c.ModelID != modelRegex && c.ModelID != modelConstant, Source: source,
			}
			risk, hasRisk := c.Answers[pol.RiskQuestion]
			untrusted, hasUntrusted := c.Answers[pol.FromUntrustedQuestion]
			if hasRisk && hasUntrusted {
				row.AnswersKnown = true
				row.Risk = &risk.Score
				row.FromUntrusted = &untrusted.Noul
				row.Approval = c.Answers[pol.ApprovalQuestion].Noul
				row.UserRequested = c.Answers[pol.UserRequestedQuestion].Noul
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func writeAnswers(path string, rows []AnswerRow) error {
	body := &bytes.Buffer{}
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("%s case %s arm %s: %w", path, row.Case, row.Arm, err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	return os.WriteFile(path, body.Bytes(), 0o644)
}

func answersFilename(now time.Time) string {
	return fmt.Sprintf("answers/heldout-%s.jsonl", now.Format("2006-01-02"))
}
