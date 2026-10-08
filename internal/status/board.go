package status

import (
	"maps"
	"slices"
	"strings"
)

type Board struct {
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
	b.clock++
	b.held[r.ID] = stamped{record: r, updated: b.clock}
	return !held || !same(was.record, r)
}

func (b *Board) List() []Record {
	listed := make([]Record, 0, len(b.held))
	for _, id := range slices.Sorted(maps.Keys(b.held)) {
		listed = append(listed, b.held[id].record)
	}
	return listed
}
