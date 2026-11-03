package main

import (
	"cmp"
	"fmt"
	"strconv"

	"tofu/internal/host"
	"tofu/internal/turn"
)

const (
	compactNothingCarried = "nothing to compact: this session carries no history yet"
	noLineOpened          = "no new line was opened, and the next turn carries the history as it was: "
)

func compactSession(live *host.Host) string { return forkNote(live, turn.ForkCompact, "") }

func branchLine(live *host.Host, message string) (string, string) {
	number, err := strconv.Atoi(cmp.Or(message, "0"))
	if err != nil {
		return "/branch takes the number of one of your messages, not " + strconv.Quote(message), ""
	}
	forked, err := live.Fork(host.HandFork{Kind: turn.ForkBranch, Message: number, By: "the person, /branch in the terminal interface"})
	if err != nil {
		return err.Error(), ""
	}
	return fmt.Sprintf("restarted from message %d as session %s, carrying the %d message(s) before it, about %d tokens of %d. the old line is kept as it was, and message %d is back in the composer to send or change",
		number, forked.Into, forked.Kept, forked.TokensAfter, forked.TokensBefore, number), forked.Words
}

func forkNote(live *host.Host, kind turn.ForkKind, focus string) string {
	forked, err := live.Fork(host.HandFork{Kind: kind, Focus: focus, By: "the person, /" + string(kind) + " in the terminal interface"})
	focused := ""
	if focus != "" {
		focused = fmt.Sprintf(", and %d result(s) naming %s stay whole", forked.Focused, strconv.Quote(focus))
	}
	switch {
	case err != nil && kind == turn.ForkCompact:
		return "nothing was compacted, and the next turn carries the history as it was: " + err.Error()
	case err != nil:
		return noLineOpened + err.Error()
	case kind == turn.ForkByHand:
		return fmt.Sprintf("forked into session %s, carrying what /compact carries: %d old tool result(s) shrunk after the first %d message(s), which stay as they were so the next request reads them from cache, about %d tokens to %d",
			forked.Into, forked.Results, forked.Kept, forked.TokensBefore, forked.TokensAfter)
	case forked.Into != "":
		return fmt.Sprintf("compacted %d old tool result(s) after the first %d message(s), which stay as they were so the next request reads them from cache%s: the history went from about %d tokens to %d, and the next turn carries it as session %s",
			forked.Results, forked.Kept, focused, forked.TokensBefore, forked.TokensAfter, forked.Into)
	case forked.TokensBefore > 0:
		return fmt.Sprintf("nothing to compact: no old tool result is left to shrink after the first %d message(s), which stay as they were for the cache%s, and the history is about %d tokens", forked.Kept, focused, forked.TokensBefore)
	}
	return compactNothingCarried
}
