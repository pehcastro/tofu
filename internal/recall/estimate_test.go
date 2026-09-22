package recall_test

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"

	"tofu/internal/recall"
)

const (
	compactionDecidesAboveTokens = 5000
	marginPercent                = 25
	underMarginPercent           = 10
	worstRowsListed              = 10
)

type recordedRequest struct {
	Session      string
	Day          string
	Step         int
	Wire         string
	Rebuilt      string
	Conversation recall.Conversation
	Reported     recall.Bill
	CachedBefore int
	Recorded     int
}

func filler(bytes int) string {
	if bytes <= 0 {
		return ""
	}
	return strings.Repeat("x", bytes)
}

func offBy(estimate, billed int) int {
	return (estimate - billed) * 100 / billed
}

type estimateRow struct {
	request  recordedRequest
	estimate int
	off      int
	was      int
}

func (r estimateRow) billed() int { return r.request.Reported.Total() }

type spread struct {
	rows   int
	median int
	worst  int
	at     estimateRow
	under  int
	lowest estimateRow
}

func spreadOf(rows []estimateRow) spread {
	var errors []int
	var found spread
	for _, r := range rows {
		if r.billed() < compactionDecidesAboveTokens {
			continue
		}
		errors = append(errors, abs(r.off))
		if abs(r.off) > found.worst {
			found.worst, found.at = abs(r.off), r
		}
		if -r.off > found.under {
			found.under, found.lowest = -r.off, r
		}
	}
	if len(errors) == 0 {
		return found
	}
	sort.Ints(errors)
	found.rows, found.median = len(errors), errors[len(errors)/2]
	return found
}

func groupsOf(rows []estimateRow, nameOf func(estimateRow) string) map[string][]estimateRow {
	grouped := map[string][]estimateRow{}
	for _, r := range rows {
		grouped[nameOf(r)] = append(grouped[nameOf(r)], r)
	}
	return grouped
}

func conversationOn(cfg, wired recall.Config, c recall.Conversation) recall.Conversation {
	c.Instructions = filler(cfg.Tokens(c.Instructions) * wired.BytesPerThousandTokens / 1000)
	return c
}

func measured(cfg recall.Config, requests []recordedRequest, wireOf func(recordedRequest) string) []estimateRow {
	bands := recall.ShippedBands()
	rows := make([]estimateRow, 0, len(requests))
	for _, request := range requests {
		wired := cfg.OnWire(wireOf(request))
		estimate := recall.Measure(wired, bands, conversationOn(cfg, wired, request.Conversation)).Total()
		billed := request.Reported.Total()
		rows = append(rows, estimateRow{request, estimate, offBy(estimate, billed), offBy(request.Recorded, billed)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].off < rows[j].off })
	return rows
}

func oneConstantForEveryWire(recordedRequest) string { return "" }

func theWireThatCarriedIt(r recordedRequest) string { return r.Wire }

func TestOurEstimateOfAContextAgainstWhatTheProviderBilledForTheSameOne(t *testing.T) {
	cfg, requests := recordedRequests(t)

	rows := measured(cfg, requests, theWireThatCarriedIt)
	for _, arm := range []struct {
		what string
		rows []estimateRow
	}{
		{"one constant for every wire", measured(cfg, requests, oneConstantForEveryWire)},
		{"a constant per wire", rows},
	} {
		byWire := groupsOf(arm.rows, wireOfRow)
		byWire["both wires"] = arm.rows
		for _, name := range slices.Sorted(maps.Keys(byWire)) {
			group := spreadOf(byWire[name])
			t.Logf("%-27s on %-10s: %3d rows above %d billed tokens, median %2d percent, worst %2d percent over on %-8s step %2d, worst %2d percent under on %-8s step %2d",
				arm.what, name, group.rows, compactionDecidesAboveTokens, group.median,
				group.worst, shortID(group.at), group.at.request.Step,
				group.under, shortID(group.lowest), group.lowest.request.Step)
		}
	}

	low, fromMessages := 0, 0
	for _, r := range rows {
		if r.request.Rebuilt == "messages" {
			fromMessages++
		}
		if r.estimate < r.billed() {
			low++
		}
	}

	listed := min(worstRowsListed, len(rows))
	for _, r := range append(append([]estimateRow{}, rows[:listed]...), rows[len(rows)-listed:]...) {
		t.Logf("%s step %2d, rebuilt from %-9s on %-9s: we estimate %6d, the provider billed %6d, %4d percent out, where the build recorded %6d, %4d percent out",
			shortID(r), r.request.Step, r.request.Rebuilt, r.request.Wire,
			r.estimate, r.billed(), r.off, r.request.Recorded, r.was)
	}

	asRecorded := make([]estimateRow, 0, len(rows))
	for _, r := range rows {
		r.off = r.was
		asRecorded = append(asRecorded, r)
	}
	whole, wasWorst := spreadOf(rows), spreadOf(asRecorded)
	t.Logf("the estimator that recorded these sessions was %d percent out at worst, %d against %d",
		wasWorst.worst, wasWorst.at.request.Recorded, wasWorst.at.billed())
	t.Logf("%d pinned requests, %d rebuilt from the messages the session kept and %d from the step rows; we read low on %d of them; above %d billed tokens the median error over %d rows is %d percent and the worst is %d percent, %d against %d",
		len(rows), fromMessages, len(rows)-fromMessages, low, compactionDecidesAboveTokens,
		whole.rows, whole.median, whole.worst, whole.at.estimate, whole.at.billed())

	byDay := groupsOf(rows, func(r estimateRow) string { return r.request.Day })
	for _, name := range slices.Sorted(maps.Keys(byDay)) {
		group := spreadOf(byDay[name])
		t.Logf("recorded on %s: %d rows above %d billed tokens, median %d percent, worst %d percent on %s step %d",
			name, group.rows, compactionDecidesAboveTokens, group.median, group.worst,
			shortID(group.at), group.at.request.Step)
	}

	if whole.worst > marginPercent {
		t.Fatalf("on a request of %d billed tokens we estimate %d, %d percent out, past the %d percent this bound allows: every threshold in context-budget.md is written in our units, so this is the size of the error in all of them",
			whole.at.billed(), whole.at.estimate, whole.worst, marginPercent)
	}
	if whole.under > underMarginPercent {
		t.Fatalf("on a request of %d billed tokens we estimate %d, %d percent under, past the %d percent this bound allows: an estimate under the provider's own number is the side that gets a request refused",
			whole.lowest.billed(), whole.lowest.estimate, whole.under, underMarginPercent)
	}
}

const sessionTailBytes = 8

func shortID(r estimateRow) string {
	if len(r.request.Session) < sessionTailBytes {
		return "no row"
	}
	return r.request.Session[len(r.request.Session)-sessionTailBytes:]
}

func wireOfRow(r estimateRow) string { return r.request.Wire }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestTheEstimateCountsTheToolSchemasEveryRequestCarriesAndNotOnlyTheInstructions(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000}
	bands := recall.ShippedBands()
	instructions := recall.Conversation{Instructions: filler(4000)}
	both := recall.Conversation{Instructions: filler(4000), ToolSchemas: filler(9000)}

	if measured := recall.Measure(cfg, bands, instructions).Identity; measured != 4000 {
		t.Fatalf("identity = %d tokens for a 4000 byte instruction block at a byte a token, want 4000", measured)
	}
	measured := recall.Measure(cfg, bands, both).Identity
	if measured != 13000 {
		t.Fatalf("identity = %d tokens for the same instructions plus a 9000 byte tool schema block, want 13000: the schemas are on every request and the provider bills them", measured)
	}
}

const wireWithExplicitCacheWrites = "anthropic"

const (
	cacheSplitSkipSession = "turn-18d7430fd0c0c304"
	cacheSplitSkipStep    = 2
	cacheSplitSkipReason  = "step 2 reads all 5,956 tokens step 1 should have written to cache, at zero write cost: " +
		"the write landed on a client attempt that was never recorded, and the attempt that followed hit it for free. " +
		"an event carries its attempt since TOFU-303, and this turn was recorded under schema 2, before the field " +
		"existed, so the attempt behind the number cannot be read back and this step stays out of the comparison"
)

func TestWhatTheProviderReadsFromItsCacheAndWhatItChargesFresh(t *testing.T) {
	_, requests := recordedRequests(t)
	compared, reset, otherWire, named := 0, 0, 0, 0
	for _, request := range requests {
		if request.Wire != wireWithExplicitCacheWrites {
			otherWire++
			continue
		}
		if request.Session == cacheSplitSkipSession && request.Step == cacheSplitSkipStep {
			named++
			t.Logf("%s step %d skipped: %s", request.Session, request.Step, cacheSplitSkipReason)
			continue
		}
		billed := request.Reported.Total()
		if request.CachedBefore == 0 || request.CachedBefore > billed {
			reset++
			continue
		}
		split := recall.Billed(request.CachedBefore, billed)
		compared++
		if split != request.Reported {
			t.Fatalf("%s step %d: from a %d token cached prefix and a %d token request we predict %d read and %d fresh, and the provider reported %d read and %d fresh",
				request.Session, request.Step, request.CachedBefore, billed,
				split.CacheRead, split.Fresh, request.Reported.CacheRead, request.Reported.Fresh)
		}
	}
	t.Logf("%d requests skipped, wired to something other than %s: that wire bills no separate cache-write figure, so a cumulative prefix cannot be tracked for it", otherWire, wireWithExplicitCacheWrites)
	modeled := len(requests) - otherWire - named
	if compared < modeled/2 {
		t.Fatalf("only %d of %d recorded requests on %s could be compared against the cache split", compared, modeled, wireWithExplicitCacheWrites)
	}
	t.Logf("the split holds on %d recorded requests; %d are skipped because the request was smaller than the cached prefix, which is a new turn rather than a step; %d are skipped by name", compared, reset, named)
}
