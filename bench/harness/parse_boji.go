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

	var count int
	var start, end time.Time

	_, err := ledger.NewReader(dir).Each(filter, func(r ledger.Row) error {
		count++
		row.JudgeDollars += r.Cost
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

	var gaps []string
	if count == 0 {
		gaps = append(gaps, "no ledger rows matched the filter, so this row carries no jev decision, no jev cost and no jev timestamps")
		return row, gaps, nil
	}

	row.Start, row.End = start, end
	row.WallClockMS = end.Sub(start).Milliseconds()
	return row, gaps, nil
}
