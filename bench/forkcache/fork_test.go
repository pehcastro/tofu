package forkcache_test

import (
	"os"
	"reflect"
	"testing"

	"tofu/bench/forkcache"
	"tofu/internal/session"
	"tofu/internal/sys"
)

const (
	pinnedPairsPath = "testdata/forks.jsonl"
	pinWriter       = "TOFU_PIN_FORK_CACHE"
	pinnedPairsMin  = 2
)

func pinned(t *testing.T) []forkcache.Pair {
	t.Helper()
	pairs, err := forkcache.ReadPairs(pinnedPairsPath)
	if err != nil {
		t.Fatalf("the pinned fork pairs at %s do not open, and nothing below is measured without them: %v", pinnedPairsPath, err)
	}
	if len(pairs) < pinnedPairsMin {
		t.Fatalf("the pin holds %d fork pairs and the report was written on at least %d", len(pairs), pinnedPairsMin)
	}
	return pairs
}

func measurable(t *testing.T) []forkcache.Pair {
	t.Helper()
	var kept []forkcache.Pair
	skipped := 0
	for _, pair := range pinned(t) {
		if !pair.ReportsCacheWrite() {
			skipped++
			continue
		}
		kept = append(kept, pair)
	}
	t.Logf("%d fork pairs skipped, recorded on a wire other than %s, which is the only one that bills a separate cache write, so a write of zero on those rows is a missing field and not a measurement",
		skipped, forkcache.WireReportingCacheWrite)
	if len(kept) == 0 {
		t.Skipf("no recorded fork pair is on %s, so nothing here reads a cache write field", forkcache.WireReportingCacheWrite)
	}
	return kept
}

func TestTheFirstRequestAfterAForkReadsCacheAndWritesNone(t *testing.T) {
	pairs := measurable(t)
	for _, pair := range pairs {
		if fate := pair.Fate(); fate != forkcache.FateRead {
			t.Errorf("%s, forked from %s, first billed a prefix %s: read %d, write %d",
				pair.SubAgent, pair.Parent, fate, pair.SubAgentFirst.CacheRead, pair.SubAgentFirst.CacheWrite)
		}
	}
	t.Logf("\n%s", forkcache.Table(pairs))
}

func TestAForkKeepsLessCachedPrefixThanCarryingOnAndPaysNoRewriteForIt(t *testing.T) {
	for _, pair := range measurable(t) {
		if pair.SubAgentFirst.CacheRead >= pair.PrefixIfContinued() {
			t.Errorf("%s read %d cached tokens where carrying on would have presented %d: a fork that loses nothing is not the mechanism this measures",
				pair.SubAgent, pair.SubAgentFirst.CacheRead, pair.PrefixIfContinued())
		}
		if pair.SubAgentFirst.CacheWrite != 0 {
			t.Errorf("%s wrote %d tokens of cache on its first request, so the lost prefix was re-paid rather than dropped", pair.SubAgent, pair.SubAgentFirst.CacheWrite)
		}
	}
}

func TestAnAccountForkIsNotInTheRecordedCorpus(t *testing.T) {
	pairs := pinned(t)
	kinds := map[string]int{}
	for _, pair := range pairs {
		kinds[pair.ForkKind]++
	}
	t.Skipf("%d recorded forks carry the kinds %v, and a session header holds no credential identity, so an account fork cannot be told from a context fork in any recorded row",
		len(pairs), kinds)
}

func TestThePinnedForkPairsStillMatchTheLiveSessions(t *testing.T) {
	store := session.NewStore(sys.RecordedStateDir("sessions"))
	live, skipped, err := forkcache.PairsIn(store)
	if err != nil {
		t.Skipf("the live sessions are not readable from here, so the pin cannot be checked against them: %v", err)
	}
	if path := os.Getenv(pinWriter); path != "" {
		if err := forkcache.WritePairs(path, live); err != nil {
			t.Fatalf("writing the pin to %s: %v", path, err)
		}
		t.Logf("wrote %d fork pairs to %s", len(live), path)
	}
	held := pinned(t)
	if !reflect.DeepEqual(live, held) {
		t.Fatalf("the live store holds %d fork pairs and the pin holds %d, or one of them reads differently:\nlive\n%s\npinned\n%s",
			len(live), len(held), forkcache.Table(live), forkcache.Table(held))
	}
	for _, skip := range skipped {
		t.Logf("skipped %s: %v", skip.ID, skip.Reason)
	}
	t.Logf("%d fork pairs read from the live store, %d sessions skipped", len(live), len(skipped))
}
