package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"tofu/internal/konst"
)

type loopGuard struct {
	seen []string
}

func (g *loopGuard) observe(row ToolCallRow) (tripped bool, repeats int) {
	signature := loopSignature(row)
	g.seen = append(g.seen, signature)
	if len(g.seen) > konst.TurnLoopGuardWindow {
		g.seen = g.seen[len(g.seen)-konst.TurnLoopGuardWindow:]
	}
	for _, candidate := range g.seen {
		if candidate == signature {
			repeats++
		}
	}
	return repeats >= konst.TurnLoopGuardRepeats, repeats
}

func loopSignature(row ToolCallRow) string {
	result := row.ResultHash
	if row.Error != "" {
		result = "error:" + row.Error
	}
	sum := sha256.Sum256([]byte(row.Tool + "\x00" + string(row.Args) + "\x00" + result))
	return hex.EncodeToString(sum[:])
}

func loopGuardCause(row ToolCallRow, repeats int) string {
	return "the tool " + strconv.Quote(row.Tool) + " was called with the same arguments and returned the same result " +
		strconv.Itoa(repeats) + " times within the last " + strconv.Itoa(konst.TurnLoopGuardWindow) + " calls"
}
