package audit

import (
	"encoding/csv"
	"io"
	"time"
)

type MemorySink struct {
	Events []Event
}

func (m *MemorySink) Append(batch []Event) error {
	m.Events = append(m.Events, batch...)
	return nil
}

func Export(w io.Writer, events []Event) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"at", "actor", "action", "target"}); err != nil {
		return err
	}
	for _, e := range events {
		if err := out.Write([]string{e.At.UTC().Format(time.RFC3339), e.Actor, e.Action, e.Target}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
