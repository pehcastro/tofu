package main

import (
	"fmt"

	"tofu/internal/host"
)

const compactNothingCarried = "nothing to compact: this session carries no history yet"

func compactSession(live *host.Host) string {
	compacted, err := live.Compact("the person, /compact in the terminal interface")
	switch {
	case err != nil:
		return "nothing was compacted, and the next turn carries the history as it was: " + err.Error()
	case compacted.Into != "":
		return fmt.Sprintf("compacted %d old tool result(s) after the first %d message(s), which stay as they were so the next request reads them from cache: the history went from about %d tokens to %d, and the next turn carries it as session %s",
			compacted.Results, compacted.Kept, compacted.TokensBefore, compacted.TokensAfter, compacted.Into)
	case compacted.TokensBefore > 0:
		return fmt.Sprintf("nothing to compact: no old tool result is left to shrink after the first %d message(s), which stay as they were for the cache, and the history is about %d tokens", compacted.Kept, compacted.TokensBefore)
	}
	return compactNothingCarried
}

func compactCarried(live *host.Host, by string) (host.Compaction, error) {
	compacted, err := live.Compact(by)
	return compacted.Compaction, err
}
