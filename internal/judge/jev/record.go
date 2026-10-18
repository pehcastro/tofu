package jev

import (
	"cmp"
	"maps"
	"slices"

	"tofu/internal/judge/ledger"
)

func (d Decision) Record(row *ledger.Row, asked []Question, wording int) {
	row.Build, row.Model, row.RequestID = d.Build, d.Alias, cmp.Or(d.RequestID, d.TransportID)
	row.LatencyMS, row.Cost = d.Latency.Milliseconds(), d.Usage.Cost
	for _, q := range asked {
		answer := d.Answers[q.ID]
		recorded := ledger.Answer{Question: q.ID, Wording: wording, Kind: ledger.AnswerNoul, Noul: answer.Noul}
		if q.Kind == QuestionChoice {
			recorded = ledger.Answer{Question: q.ID, Wording: wording, Kind: ledger.AnswerChoice, Choice: answer.Choice}
			for _, option := range slices.Sorted(maps.Keys(answer.Probabilities)) {
				recorded.Dist = append(recorded.Dist, ledger.Slice{Option: option, P: answer.Probabilities[option]})
			}
		}
		row.Answers = append(row.Answers, recorded)
	}
}
