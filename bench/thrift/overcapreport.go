package thrift

import (
	"fmt"
	"strings"
)

const firstRunCostUSD = 0.040968
const firstRunCalls = 393
const mechanicalFloorPercent = 1.5

func RenderOverCap(targets []OverCapTarget, idSkips []JudgedSkip, judged OverCapJudged) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "\n7. the 9 over-cap read and search artifacts, judged against fixed truncation on the part each one drops\n")
	fmt.Fprintf(b, "%d distinct over-cap read or search artifacts identified from the corpus, %d skipped before judging\n", len(targets)+len(idSkips), len(idSkips))
	for _, skip := range idSkips {
		fmt.Fprintf(b, "  identification skip: %s: %s\n", skip.Turn, skip.Reason)
	}
	fmt.Fprintf(b, "call cap %d, %d calls made, $%.6f spent, %d of %d rows judged\n",
		judged.CallCap, judged.CallsMade, judged.CostUSD, len(judged.Rows), len(targets))
	fmt.Fprintf(b, "%-24s %-8s %10s %6s %11s %11s %13s %11s %11s %13s %8s\n",
		"turn", "tool", "raw bytes", "paras", "fixed byt", "fixed drop", "fixed tokens", "thrift byt", "thrift drop", "thrift tokens", "overlap")
	var rawTotal, fixedTotal, fixedTokTotal, thriftTotal, thriftTokTotal, fixedDroppedTotal, thriftDroppedTotal, overlapTotal int64
	var worked *OverCapRow
	for i := range judged.Rows {
		row := &judged.Rows[i]
		fmt.Fprintf(b, "%-24s %-8s %10d %6d %11d %11d %13d %11d %11d %13d %8d\n",
			row.Turn, row.Tool, row.RawBytes, row.Paragraphs, row.FixedBytes, row.FixedDroppedBytes, row.FixedTokens,
			row.ThriftBytes, row.ThriftDroppedBytes, row.ThriftTokens, row.OverlapBytes)
		rawTotal += row.RawBytes
		fixedTotal += row.FixedBytes
		fixedTokTotal += row.FixedTokens
		thriftTotal += row.ThriftBytes
		thriftTokTotal += row.ThriftTokens
		fixedDroppedTotal += row.FixedDroppedBytes
		thriftDroppedTotal += row.ThriftDroppedBytes
		overlapTotal += row.OverlapBytes
		if row.KeptInFixedDropBytes > 0 && worked == nil {
			worked = row
		}
	}
	fmt.Fprintf(b, "totals: raw %d bytes, fixed truncation %d bytes (%d tokens, dropped %d), thrift %d bytes (%d tokens, dropped %d), overlap between the two drops %d bytes\n",
		rawTotal, fixedTotal, fixedTokTotal, fixedDroppedTotal, thriftTotal, thriftTokTotal, thriftDroppedTotal, overlapTotal)
	if fixedTokTotal > 0 {
		percent := share(fixedTokTotal-thriftTokTotal, fixedTokTotal)
		clears := "clears"
		if percent < mechanicalFloorPercent {
			clears = "does not clear"
		}
		fmt.Fprintf(b, "thrift cuts a further %d of %d fixed-truncation tokens, %.1f%%, against the 1.5%% mechanical floor from TOFU-338: this %s it\n",
			fixedTokTotal-thriftTokTotal, fixedTokTotal, percent, clears)
	}
	if worked != nil {
		fmt.Fprintf(b, "worked example: %s, turn %s: fixed truncation drops %d bytes from the middle and thrift would have kept %d of those bytes across %d paragraph(s), so on this result truncation by position drops a part the judgment says still matters\n",
			worked.Command, worked.Turn, worked.FixedDroppedBytes, worked.KeptInFixedDropBytes, worked.KeptInFixedDropParagraphs)
	} else if len(judged.Rows) > 0 {
		fmt.Fprintf(b, "no worked example: across %d judged rows, every paragraph thrift kept already fell inside what fixed truncation itself keeps\n", len(judged.Rows))
	}
	fmt.Fprintf(b, "cost against the first run: %d calls here against %d there, $%.6f against $%.6f\n",
		judged.CallsMade, firstRunCalls, judged.CostUSD, firstRunCostUSD)
	for _, skip := range judged.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Turn, skip.Reason)
	}
	return b.String()
}
