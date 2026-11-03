package jev

import (
	"cmp"
	"maps"
	"slices"

	"tofu/internal/judge/ledger"
)

func (d Decision) Record(row *ledger.Row, asked []Question, wording int) {
	row.Model, row.LatencyMS = d.Alias, d.Latency.Milliseconds()
	d.Spent(row)
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

func (d Decision) Spent(row *ledger.Row) {
	charge := d.Charge
	row.Build, row.RequestID, row.Cost = d.Build, cmp.Or(d.RequestID, d.TransportID), d.Usage.Cost
	row.Provider, row.Key, row.Charge = d.Wire, d.Key, &charge
	row.Usage = &ledger.Usage{InputTokens: d.Usage.InputTokens, OutputTokens: d.Usage.OutputTokens}
}
