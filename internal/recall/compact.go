package recall

import (
	"fmt"
	"slices"
)

type DropReason string

const (
	DroppedSuperseded DropReason = "superseded"
	DroppedAged       DropReason = "aged"
)

type Drop struct {
	Step        int
	Tool        string
	Handle      string
	Bytes       int
	TokensFreed int
	Reason      DropReason
}

func Compact(store *Store, cfg Config, bands Bands, c Conversation) (Conversation, []Drop, error) {
	total := Measure(cfg, bands, c).Total()
	if total <= bands.Target() {
		return c, nil, nil
	}
	entries := slices.Clone(c.Entries)
	newest := make(map[string]int)
	for i, entry := range entries {
		if entry.SupersedeKey != "" {
			newest[entry.SupersedeKey] = i
		}
	}

	type victim struct {
		index  int
		reason DropReason
	}
	var superseded, aged []victim
	for i, entry := range entries[:recentFrom(cfg, bands, entries)] {
		if entry.Tool == "" || entry.Handle != "" || len(entry.Text) < cfg.CompactFloorBytes {
			continue
		}
		if entry.SupersedeKey != "" && newest[entry.SupersedeKey] != i {
			superseded = append(superseded, victim{i, DroppedSuperseded})
			continue
		}
		aged = append(aged, victim{i, DroppedAged})
	}

	var drops []Drop
	for _, chosen := range slices.Concat(superseded, aged) {
		if total <= bands.Target() {
			break
		}
		entry := entries[chosen.index]
		handle, err := store.put([]byte(entry.Text))
		if err != nil {
			return Conversation{}, nil, fmt.Errorf("recall: compaction could not store the %s result from step %d: %w", entry.Tool, entry.Step, err)
		}
		bytes := len(entry.Text)
		notice := droppedNotice(entry.Tool, bytes, handle)
		freed := cfg.Tokens(entry.Text) - cfg.Tokens(notice)
		total -= freed
		entry.Text, entry.Handle = notice, handle
		entries[chosen.index] = entry
		drops = append(drops, Drop{
			Step:        entry.Step,
			Tool:        entry.Tool,
			Handle:      handle,
			Bytes:       bytes,
			TokensFreed: freed,
			Reason:      chosen.reason,
		})
	}
	c.Entries = entries
	return c, drops, nil
}

func droppedNotice(tool string, bytes int, handle string) string {
	return fmt.Sprintf(
		"the %s result that stood here is held whole in artifact %s: %d bytes, dropped from this conversation to stay inside the context budget. "+
			"call artifact_fetch with that handle, an offset and a length to read any range of it.",
		tool, handle, bytes)
}
