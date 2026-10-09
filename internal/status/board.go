package status

import (
	"maps"
	"slices"
	"strings"
	"time"
)

type Board struct {
	Now   func() time.Time
	held  map[string]stamped
	clock int
}

type stamped struct {
	record  Record
	updated int
}

func (b *Board) Apply(r Record) bool {
	if r.State == Clear {
		before := len(b.held)
		maps.DeleteFunc(b.held, func(id string, _ stamped) bool {
			return r.ID == "" || id == r.ID || strings.HasPrefix(id, r.ID+"/")
		})
		return len(b.held) != before
	}
	if b.held == nil {
		b.held = map[string]stamped{}
	}
	was, held := b.held[r.ID]
	if !held && len(b.held) >= maxRecords {
		oldest := slices.MinFunc(slices.Collect(maps.Keys(b.held)), func(x, y string) int { return b.held[x].updated - b.held[y].updated })
		delete(b.held, oldest)
	}
	switch {
	case held && was.record.State == r.State:
		r.At = was.record.At
	case b.Now != nil:
		r.At = b.Now()
	}
	b.clock++
	b.held[r.ID] = stamped{record: r, updated: b.clock}
	return !held || !same(was.record, r)
}

func (b *Board) Sync(wanted []Record) []Record {
	wanted = wanted[:min(len(wanted), maxRecords)]
	var sent []Record
	for _, held := range b.List() {
		gone := Record{ID: held.ID, State: Clear}
		if !slices.ContainsFunc(wanted, func(record Record) bool { return record.ID == held.ID }) && b.Apply(gone) {
			sent = append(sent, gone)
		}
	}
	for _, record := range wanted {
		if b.Apply(record) {
			sent = append(sent, b.held[record.ID].record)
		}
	}
	return sent
}

func (b *Board) List() []Record {
	listed := make([]Record, 0, len(b.held))
	for _, id := range slices.Sorted(maps.Keys(b.held)) {
		listed = append(listed, b.held[id].record)
	}
	return listed
}
