package main

import (
	"fmt"
	"path/filepath"
	"time"

	"tofu/internal/host"
	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const compactNothingCarried = "nothing to compact: this session carries no history yet"

func compactSession(live *host.Host) string {
	id, carried := live.ID(), live.Carried()
	if id == "" || len(carried) == 0 {
		return compactNothingCarried
	}
	unchanged := "nothing was compacted, and the next turn carries the history as it was: "
	store, err := sessionstore.Open()
	if err != nil {
		return unchanged + err.Error()
	}
	header, err := store.Header(id)
	if err != nil {
		return unchanged + err.Error()
	}
	state, err := sys.ProjectStateDir()
	if err != nil {
		return unchanged + err.Error()
	}
	compacted, err := turn.CompactCarried(filepath.Join(state, "artifacts"), header.Wire, carried)
	if err != nil {
		return unchanged + err.Error()
	}
	if compacted.Results == 0 {
		return fmt.Sprintf("nothing to compact: no old tool result is left to shrink, and the history is about %d tokens", compacted.TokensBefore)
	}
	into, err := turn.RecordCarried(store, id, compacted, time.Now())
	if err == nil {
		err = store.SetHead(into)
	}
	if err == nil {
		err = live.Compacted(into, compacted.Messages)
	}
	if err != nil {
		return unchanged + err.Error()
	}
	return fmt.Sprintf("compacted %d old tool result(s) to an artifact handle each: the history went from about %d tokens to %d, and the next turn carries it as session %s",
		compacted.Results, compacted.TokensBefore, compacted.TokensAfter, into)
}
