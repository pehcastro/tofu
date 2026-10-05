package main

import (
	"fmt"
	"path/filepath"

	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const compactNothingCarried = "nothing to compact: this session carries no history yet"

func (s *appSession) compact() string {
	if s.id == "" || len(s.carried) == 0 {
		return compactNothingCarried
	}
	unchanged := "nothing was compacted, and the next turn carries the history as it was: "
	store, err := sessionstore.Open()
	if err != nil {
		return unchanged + err.Error()
	}
	header, err := store.Header(s.id)
	if err != nil {
		return unchanged + err.Error()
	}
	state, err := sys.ProjectStateDir()
	if err != nil {
		return unchanged + err.Error()
	}
	compacted, err := turn.CompactCarried(filepath.Join(state, "artifacts"), header.Wire, s.carried)
	if err != nil {
		return unchanged + err.Error()
	}
	if compacted.Results == 0 {
		return fmt.Sprintf("nothing to compact: no old tool result is left to shrink, and the history is about %d tokens", compacted.TokensBefore)
	}
	into, err := turn.RecordCarried(store, s.id, compacted, s.now())
	if err == nil {
		err = store.SetHead(into)
	}
	if err != nil {
		return unchanged + err.Error()
	}
	s.id, s.carried = into, compacted.Messages
	return fmt.Sprintf("compacted %d old tool result(s) to an artifact handle each: the history went from about %d tokens to %d, and the next turn carries it as session %s",
		compacted.Results, compacted.TokensBefore, compacted.TokensAfter, into)
}
