package harness

import (
	"time"

	"boji/internal/judge/ledger"
)

type BojiMeta = RunMeta

func ParseBoji(dir string, filter ledger.Filter, meta BojiMeta) (Row, []string, error) {
	row := Row{
		Arm:            meta.Arm,
		Task:           meta.Task,
		Version:        meta.Version,
		Run:            meta.Run,
		CLIVersion:     meta.CLIVersion,
		CredentialKind: meta.CredentialKind,
		Commit:         meta.Commit,
	}

	var dollars float64
	var count int
	var start, end time.Time

	_, err := ledger.NewReader(dir).Each(filter, func(r ledger.Row) error {
		count++
		dollars += r.Cost
		if row.Model == "" {
			row.Model = r.Model
		}
		if start.IsZero() || r.At.Before(start) {
			start = r.At
		}
		if end.IsZero() || r.At.After(end) {
			end = r.At
		}
		return nil
	})
	if err != nil {
		return Row{}, nil, err
	}

	gaps := []string{
		"turns: internal/judge/ledger has no notion of turns yet, there is no loop until E4",
		"tool calls: internal/judge/ledger has no notion of tool calls yet, there is no loop until E4",
		"billed input/output tokens: ledger.Row carries Cost but not token counts",
	}
	if count == 0 {
		gaps = append(gaps, "no ledger rows matched the filter, the row below has nothing else to report")
		return row, gaps, nil
	}

	row.Start, row.End = start, end
	row.WallClockMS = end.Sub(start).Milliseconds()
	if meta.CredentialKind == CredentialKindKey {
		row.Dollars = &dollars
	}
	return row, gaps, nil
}
