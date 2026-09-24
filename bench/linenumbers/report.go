package linenumbers

import (
	"fmt"
	"strings"

	"tofu/internal/konst"
)

const thresholdPercent = 0.5

func Render(machine, date string, result Result) string {
	b := &strings.Builder{}

	fmt.Fprintf(b, "# bench linenumbers: no, %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s. No live model call: this reads recorded turns under `.tofu/sessions` through `bench/corpus.WalkSessions` and the read tool's own source, nothing else.\n\n", machine)

	fmt.Fprintf(b, "## Threshold, stated before the number\n\n")
	fmt.Fprintf(b, "A saving under %.1f%% of read-tool bytes or of total cache-read tokens is a no, the floor the ticket names for a change to be worth making. The number below decides it.\n\n", thresholdPercent)

	fmt.Fprintf(b, "## Why the answer is no\n\n")
	fmt.Fprintf(b, "`internal/turn/tool_read.go` never prefixes a returned line with its number. A whole read (no `start_line`) returns the file's bytes verbatim; a ranged read prepends one header line naming the span, then the raw lines joined by newlines, still with no per-line number. `TestNoReadEverEmitsALineNumber` in this package reads a ten-line file both ways and finds no line beginning with a digit followed by a number-like separator. Every byte a sparse-numbering scheme would touch is already zero: nothing numbers every line today, so nothing would become every tenth.\n\n")

	fmt.Fprintf(b, "## The corpus\n\n")
	fmt.Fprintf(b, "`%s` through `bench/corpus.WalkSessions`: %d turns read, %d entries skipped.\n", result.SessionsDir, result.Turns, len(result.Skipped))
	for _, s := range result.Skipped {
		fmt.Fprintf(b, "  skipped: %s: %s\n", s.Path, s.Reason)
	}
	b.WriteString("\n")

	fmt.Fprintf(b, "## The numbers\n\n")
	fmt.Fprintf(b, "%d read-tool calls, %d bytes rendered to the model (about %d estimated tokens at %d bytes per token). %d of the recorded steps carried a cache-read token count, summing to %d cache-read tokens.\n\n",
		result.ReadCalls, result.ReadBytes, result.ReadEstTokens, konst.SearchBytesPerToken, result.CacheReadSteps, result.CacheReadTokens)

	fmt.Fprintf(b, "Line-number bytes: 0, because none are emitted. Share of read-tool bytes: 0.0%%. Share of total cache-read tokens: 0.0%%. Both are under the %.1f%% floor by construction, so the answer is no and nothing in `internal/turn` changes.\n",
		thresholdPercent)

	return b.String()
}
