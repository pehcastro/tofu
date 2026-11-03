package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

type loopGuard struct {
	seen    []string
	repeats int
	window  int
}

func newLoopGuard(caps Caps) *loopGuard {
	return &loopGuard{repeats: caps.LoopGuardRepeats, window: caps.LoopGuardWindow}
}

func (g *loopGuard) observe(row ToolCallRow) (tripped bool, repeats int) {
	if g.repeats <= 0 || g.window <= 0 {
		return false, 0
	}
	signature := loopSignature(row)
	g.seen = append(g.seen, signature)
	if len(g.seen) > g.window {
		g.seen = g.seen[len(g.seen)-g.window:]
	}
	for _, candidate := range g.seen {
		if candidate == signature {
			repeats++
		}
	}
	return repeats >= g.repeats, repeats
}

func loopSignature(row ToolCallRow) string {
	result := row.ResultHash
	if row.Error != "" {
		result = "error:" + row.Error
	}
	sum := sha256.Sum256([]byte(row.Tool + "\x00" + string(row.Args) + "\x00" + result))
	return hex.EncodeToString(sum[:])
}

func loopGuardCause(row ToolCallRow, repeats, window int) string {
	return "the tool " + strconv.Quote(row.Tool) + " was called with the same arguments and returned the same result " +
		strconv.Itoa(repeats) + " times within the last " + strconv.Itoa(window) + " calls"
}
