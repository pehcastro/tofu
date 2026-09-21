package thrift

import (
	"fmt"
	"strings"
)

func Render(result Result) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "corpus: %s, %d entries, %d read as sessions, %d skipped\n", result.SessionsDir, result.Entries, result.Sessions, len(result.Skips))
	for _, skip := range result.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Path, skip.Reason)
	}
	fmt.Fprintf(b, "result bytes cap: %d, bytes per token: %d\n\n", result.ResultBytesCap, result.BytesPerToken)

	b.WriteString("1. calls per session\n")
	writeSpread(b, "tool calls", result.CallsPerSession)
	b.WriteString("\nshape, sessions per call count band\n")
	for _, bucket := range result.CallShape {
		label := fmt.Sprintf("%d to %d", bucket.Low, bucket.High)
		if bucket.High < 0 {
			label = fmt.Sprintf("%d and up", bucket.Low)
		}
		fmt.Fprintf(b, "%-14s %4d  %s\n", label, bucket.Count, strings.Repeat("#", bucket.Count))
	}

	b.WriteString("\n2. tool result tokens per session\n")
	writeSpread(b, "rendered tokens", result.RenderedTokensPerSession)
	writeSpread(b, "raw tokens", result.RawTokensPerSession)
	fmt.Fprintf(b, "\n%-16s %7s %9s %14s %14s %10s %10s %10s\n", "tool", "calls", "sessions", "raw tokens", "rendered", "median", "p90", "worst")
	for _, tool := range result.Tools {
		fmt.Fprintf(b, "%-16s %7d %9d %14d %14d %10d %10d %10d\n",
			tool.Tool, tool.Calls, tool.SessionsUsing, tool.RawTokens, tool.RenderedTokens,
			tool.PerSession.Median, tool.PerSession.P90, tool.PerSession.Worst)
	}

	fmt.Fprintf(b, "\n3. rtk over a whole session, margin %d tokens per bash call from the spec\n", result.Rtk.MarginTokensPerCall)
	writeSpread(b, "bash calls", result.Rtk.BashCallsPerSession)
	writeSpread(b, "claimed saved", result.Rtk.ClaimedPerSession)
	writeSpread(b, "capped saved", result.Rtk.CappedPerSession)
	fmt.Fprintf(b, "claimed saving against rendered tokens: %.1f%% of the corpus total\n",
		share(result.Rtk.ClaimedPerSession.Sum, result.RenderedTokensPerSession.Sum))
	fmt.Fprintf(b, "capped saving against rendered tokens: %.1f%% of the corpus total\n",
		share(result.Rtk.CappedPerSession.Sum, result.RenderedTokensPerSession.Sum))

	writeProse(b, result)
	return b.String()
}

func writeProse(b *strings.Builder, result Result) {
	prose := result.Prose
	fmt.Fprintf(b, "\n4. redundancy in a recorded tool result, %d handles under %s, %d skipped\n", prose.Handles, prose.Dir, len(prose.Skips))
	for _, skip := range prose.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Handle, skip.Reason)
	}
	fmt.Fprintf(b, "arm: keep the first %d and the last %d bytes and drop the middle, which is what internal/turn/artifact.go does today at a %d byte cap\n",
		prose.ArmHead, prose.ArmTail, prose.ArmBytes)
	fmt.Fprintf(b, "%-16s %6s %14s %10s %10s %10s %8s %8s %10s %10s %8s %9s\n",
		"tool", "files", "bytes", "blank", "duplicate", "trailing", "lossless", "gzip", "uniq lines", "arm keeps", "over cap", "stripped")
	for _, row := range append(append([]Redundancy(nil), prose.Rows...), prose.Whole) {
		fmt.Fprintf(b, "%-16s %6d %14d %10d %10d %10d %7.1f%% %7.1f%% %10d %9.1f%% %8d %9d\n",
			row.Tool, row.Artifacts, row.Bytes, row.BlankBytes, row.DuplicateBytes, row.TrailingBytes,
			share(row.RemovableBytes(), row.Bytes), share(row.Bytes-row.GzipBytes, row.Bytes),
			row.UniqueLines, share(int64(row.ArmKeptUniqueLines), int64(row.UniqueLines)),
			row.OverCap, row.UnderCapIfStripped)
	}
	fmt.Fprintf(b, "arm drops %d of %d bytes, %.1f%%, and keeps %d of %d unique lines, %.1f%%\n",
		prose.Whole.Bytes-prose.Whole.ArmKeptBytes, prose.Whole.Bytes,
		share(prose.Whole.Bytes-prose.Whole.ArmKeptBytes, prose.Whole.Bytes),
		prose.Whole.ArmKeptUniqueLines, prose.Whole.UniqueLines,
		share(int64(prose.Whole.ArmKeptUniqueLines), int64(prose.Whole.UniqueLines)))
}

func writeSpread(b *strings.Builder, label string, spread Spread) {
	fmt.Fprintf(b, "%-16s n=%d sum=%d zeros=%d min=%d p25=%d median=%d p75=%d p90=%d worst=%d\n",
		label, spread.Count, spread.Sum, spread.Zeros, spread.Min, spread.P25, spread.Median, spread.P75, spread.P90, spread.Worst)
}

func share(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}
