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
	compacted, err := compactCarried(live)
	switch {
	case err != nil:
		return "nothing was compacted, and the next turn carries the history as it was: " + err.Error()
	case compacted.Into != "":
		return fmt.Sprintf("compacted %d old tool result(s) to an artifact handle each: the history went from about %d tokens to %d, and the next turn carries it as session %s",
			compacted.Results, compacted.TokensBefore, compacted.TokensAfter, compacted.Into)
	case compacted.TokensBefore > 0:
		return fmt.Sprintf("nothing to compact: no old tool result is left to shrink, and the history is about %d tokens", compacted.TokensBefore)
	}
	return compactNothingCarried
}

func compactCarried(live *host.Host) (host.Compaction, error) {
	id, carried := live.ID(), live.Carried()
	if id == "" || len(carried) == 0 {
		return host.Compaction{}, nil
	}
	store, err := sessionstore.Open()
	if err != nil {
		return host.Compaction{}, err
	}
	header, err := store.Header(id)
	if err != nil {
		return host.Compaction{}, err
	}
	state, err := sys.ProjectStateDir()
	if err != nil {
		return host.Compaction{}, err
	}
	compacted, err := turn.CompactCarried(filepath.Join(state, "artifacts"), header.Wire, carried)
	if err != nil {
		return host.Compaction{}, err
	}
	result := host.Compaction{Results: compacted.Results, TokensBefore: compacted.TokensBefore, TokensAfter: compacted.TokensAfter}
	if compacted.Results == 0 {
		return result, nil
	}
	into, err := turn.RecordCarried(store, id, compacted, time.Now())
	if err == nil {
		err = store.SetHead(into)
	}
	if err == nil {
		err = live.Compacted(into, compacted.Messages)
	}
	if err != nil {
		return host.Compaction{}, err
	}
	result.Into = into
	return result, nil
}
