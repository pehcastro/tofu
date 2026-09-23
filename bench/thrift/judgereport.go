package thrift

import (
	"fmt"
	"strings"

	"tofu/internal/konst"
)

func RenderJudged(result Result, judged Judged) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "\n5. the judged thrift cut against fixed truncation, over read and search results carrying a stored artifact\n")
	fmt.Fprintf(b, "arm: internal/judge/thrift, the still_needed question at library/questions/thrift@1.yaml, asked once per paragraph, kept at or above %.2f\n", thriftKeepAt)
	fmt.Fprintf(b, "live call cap %d, %d calls made, $%.6f spent, %d rows measured, %d calls skipped\n",
		judged.CallCap, judged.CallsMade, judged.CostUSD, len(judged.Rows), len(judged.Skips))
	fmt.Fprintf(b, "%-24s %-8s %10s %14s %14s %14s %14s\n", "turn", "tool", "raw bytes", "fixed bytes", "fixed tokens", "thrift bytes", "thrift tokens")
	var rawTotal, fixedTotal, thriftTotal, fixedTokTotal, thriftTokTotal, largestRaw int64
	for _, row := range judged.Rows {
		fmt.Fprintf(b, "%-24s %-8s %10d %14d %14d %14d %14d\n",
			row.Turn, row.Tool, row.RawBytes, row.FixedBytes, row.FixedTokens, row.ThriftBytes, row.ThriftTokens)
		rawTotal += row.RawBytes
		fixedTotal += row.FixedBytes
		thriftTotal += row.ThriftBytes
		fixedTokTotal += row.FixedTokens
		thriftTokTotal += row.ThriftTokens
		largestRaw = max(largestRaw, row.RawBytes)
	}
	fmt.Fprintf(b, "totals: raw %d bytes, fixed truncation %d bytes (%d tokens), thrift %d bytes (%d tokens)\n",
		rawTotal, fixedTotal, fixedTokTotal, thriftTotal, thriftTokTotal)
	if fixedTotal == rawTotal {
		overCap, files := overCapReadAndSearch(result)
		fmt.Fprintf(b, "the fixed-truncation arm did not cut a single byte on this sample: the largest row is %d bytes against a %d byte cap\n", largestRaw, konst.TurnResultBytesCap)
		fmt.Fprintf(b, "thrift removed %d of %d tokens anyway, %.1f%%, over read and search results the arm would have passed through whole\n",
			fixedTokTotal-thriftTokTotal, fixedTokTotal, share(fixedTokTotal-thriftTokTotal, fixedTokTotal))
		fmt.Fprintf(b, "the arm-firing comparison this ticket asks for cannot be made on this sample: %d of %d stored read and search artifacts in the whole corpus exceed the cap, from section 4, and none of them landed inside this sample's live call budget\n", overCap, files)
	} else {
		switch {
		case thriftTokTotal < fixedTokTotal:
			fmt.Fprintf(b, "thrift beat the fixed-truncation arm over this sample: %d tokens against %d, task outcome not measured\n", thriftTokTotal, fixedTokTotal)
		case thriftTokTotal == fixedTokTotal:
			fmt.Fprintf(b, "thrift tied the fixed-truncation arm over this sample at %d tokens\n", thriftTokTotal)
		default:
			fmt.Fprintf(b, "thrift lost to the fixed-truncation arm over this sample: %d tokens against %d\n", thriftTokTotal, fixedTokTotal)
		}
	}
	for _, skip := range judged.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Turn, skip.Reason)
	}
	return b.String()
}

func overCapReadAndSearch(result Result) (int, int) {
	overCap, files := 0, 0
	for _, row := range result.Prose.Rows {
		if row.Tool != "read" && row.Tool != "search" {
			continue
		}
		overCap += row.OverCap
		files += row.Artifacts
	}
	return overCap, files
}

func RenderThreeArms(result Result, judged Judged) string {
	b := &strings.Builder{}
	baseline := result.RenderedTokensPerSession.Sum
	rtkAlone := baseline - result.Rtk.CappedPerSession.Sum
	var thriftDelta int64
	for _, row := range judged.Rows {
		thriftDelta += row.FixedTokens - row.ThriftTokens
	}
	rtkAndThrift := rtkAlone - thriftDelta
	fmt.Fprintf(b, "\n6. three arms over the corpus total, rendered tokens\n")
	fmt.Fprintf(b, "neither:      %d tokens, today's baseline, fixed truncation already applied by internal/turn/artifact.go\n", baseline)
	fmt.Fprintf(b, "rtk alone:    %d tokens, baseline minus the capped rtk saving on bash calls, %d\n", rtkAlone, result.Rtk.CappedPerSession.Sum)
	fmt.Fprintf(b, "rtk + thrift: %d tokens, rtk alone minus the thrift saving measured over %d sampled read and search calls, %d\n", rtkAndThrift, len(judged.Rows), thriftDelta)
	fmt.Fprintf(b, "the thrift term is not extrapolated past what was actually judged: %d of the corpus's read and search calls fell inside the %d live call cap\n", len(judged.Rows), judged.CallCap)
	return b.String()
}
