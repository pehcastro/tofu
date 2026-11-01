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
	compacted, into, err := compactInto(live)
	switch {
	case err != nil:
		return "nothing was compacted, and the next turn carries the history as it was: " + err.Error()
	case into != "":
		return fmt.Sprintf("compacted %d old tool result(s) after the first %d message(s), which stay as they were so the next request reads them from cache: the history went from about %d tokens to %d, and the next turn carries it as session %s",
			compacted.Results, compacted.Kept, compacted.TokensBefore, compacted.TokensAfter, into)
	case compacted.TokensBefore > 0:
		return fmt.Sprintf("nothing to compact: no old tool result is left to shrink after the first %d message(s), which stay as they were for the cache, and the history is about %d tokens", compacted.Kept, compacted.TokensBefore)
	}
	return compactNothingCarried
}

func compactCarried(live *host.Host) (host.Compaction, error) {
	compacted, into, err := compactInto(live)
	if err != nil {
		return host.Compaction{}, err
	}
	return host.Compaction{Into: into, Results: compacted.Results, TokensBefore: compacted.TokensBefore, TokensAfter: compacted.TokensAfter}, nil
}

func compactInto(live *host.Host) (turn.Compacted, string, error) {
	id, carried := live.ID(), live.Carried()
	if id == "" || len(carried) == 0 {
		return turn.Compacted{}, "", nil
	}
	store, err := sessionstore.Open()
	if err != nil {
		return turn.Compacted{}, "", err
	}
	header, err := store.Header(id)
	if err != nil {
		return turn.Compacted{}, "", err
	}
	state, err := sys.ProjectStateDir()
	if err != nil {
		return turn.Compacted{}, "", err
	}
	compacted, err := turn.CompactCarried(filepath.Join(state, "artifacts"), header.Wire, carried)
	if err != nil || compacted.Results == 0 {
		return compacted, "", err
	}
	into, err := turn.RecordCarried(store, id, compacted, time.Now())
	if err == nil {
		err = store.SetHead(into)
	}
	if err == nil {
		err = live.Compacted(into, compacted.Messages)
	}
	if err != nil {
		return turn.Compacted{}, "", err
	}
	return compacted, into, nil
}
