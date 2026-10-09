package quota

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/transport"
)

type usageStub struct {
	asked  atomic.Int64
	status atomic.Int64
	after  string
	delay  time.Duration
	server *httptest.Server
}

func newUsageStub(t *testing.T) *usageStub {
	stub := &usageStub{}
	stub.status.Store(http.StatusOK)
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		stub.asked.Add(1)
		time.Sleep(stub.delay)
		if status := int(stub.status.Load()); status != http.StatusOK {
			if stub.after != "" {
				w.Header().Set("Retry-After", stub.after)
			}
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, `{"five_hour":{"utilization":17,"resets_at":"2026-06-03T12:30:00Z"},
			"seven_day":{"utilization":40,"resets_at":"2026-06-06T00:00:00Z"},
			"limits":[{"kind":"weekly_scoped","percent":20,"resets_at":"2026-06-06T00:00:00Z","scope":{"model":{"display_name":"Opus"}}}]}`)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) pass(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

func sharedPoller(t *testing.T, dir string, stub *usageStub, now func() time.Time) *Poller {
	t.Helper()
	poller, err := NewPoller(nil, now, map[Provider]string{ClaudeSub: stub.server.URL, CodexSub: stub.server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	poller.dir, poller.spread = dir, func() float64 { return 0.5 }
	return poller
}

func claudeRow(row int64) Account {
	return Account{Provider: ClaudeSub, AccountID: "acct-" + strconv.FormatInt(row, 10), Row: row, Credential: madeUpAccess{}}
}

type madeUpAccess struct{}

func (madeUpAccess) Access(context.Context) (string, error) { return "made-up", nil }

func windowUsed(t *testing.T, report Report, id string) float64 {
	t.Helper()
	for _, window := range report.Windows {
		if window.ID == id {
			return window.Used.Fraction
		}
	}
	t.Fatalf("no %s window in %+v", id, report.Windows)
	return 0
}

func TestA429KeepsTheLastReadingStaleAndASecondProcessDoesNotAsk(t *testing.T) {
	dir, stub, clock := t.TempDir(), newUsageStub(t), &testClock{at: recordedNow}
	if _, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1)); err != nil {
		t.Fatal(err)
	}
	clock.pass(10 * time.Minute)
	stub.status.Store(http.StatusTooManyRequests)
	stub.after = "120"
	report, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1))
	if transport.KindOf(err) != transport.KindRateLimit {
		t.Fatalf("the 429 read as %v", err)
	}
	if !report.Stale || windowUsed(t, report, fiveHourWindow) != 0.17 || !report.FetchedAt.Equal(recordedNow) {
		t.Fatalf("the 429 served %+v, want the reading from %s marked stale", report, recordedNow)
	}
	if want := clock.now().Add(2 * time.Minute); !report.RetryAt.Equal(want) {
		t.Fatalf("retry at %s, want %s from Retry-After 120", report.RetryAt, want)
	}
	clock.pass(90 * time.Second)
	again, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1))
	if asked := stub.asked.Load(); asked != 2 {
		t.Fatalf("the stub was asked %d times, want 2: the cooldown did not cross processes", asked)
	}
	if transport.KindOf(err) != transport.KindRateLimit || !again.Stale || len(again.Windows) == 0 {
		t.Fatalf("inside the cooldown the poll answered %+v, %v", again, err)
	}
	clock.pass(time.Minute)
	stub.status.Store(http.StatusOK)
	fresh, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1))
	if err != nil || fresh.Stale || stub.asked.Load() != 3 {
		t.Fatalf("after the cooldown the poll answered %+v, %v, asked %d", fresh, err, stub.asked.Load())
	}
}

func TestAColdCacheServesTheLoggedReadingWhenTheEndpointFails(t *testing.T) {
	dir, stub, clock := t.TempDir(), newUsageStub(t), &testClock{at: recordedNow}
	logged := Reading{Provider: ClaudeSub, Account: 1, At: recordedNow.Add(-3 * time.Hour), Windows: []ReadingWindow{{ID: fiveHourWindow, Used: 0.42}}}
	if err := AppendReading(dir, logged); err != nil {
		t.Fatal(err)
	}
	if err := AppendReading(dir, Reading{Provider: ClaudeSub, Account: 2, At: recordedNow.Add(-time.Hour), Windows: []ReadingWindow{{ID: fiveHourWindow, Used: 0.9}}}); err != nil {
		t.Fatal(err)
	}
	stub.status.Store(http.StatusTooManyRequests)
	report, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1))
	if err == nil || !report.Stale || report.Source != SourceLog || windowUsed(t, report, fiveHourWindow) != 0.42 || !report.FetchedAt.Equal(logged.At) {
		t.Fatalf("a cold cache over a logged reading answered %+v, %v", report, err)
	}
}

func TestPollersOpenedAtOnceAskOncePerAccount(t *testing.T) {
	dir, stub := t.TempDir(), newUsageStub(t)
	stub.delay = 200 * time.Millisecond
	var polls sync.WaitGroup
	for range 3 {
		poller := sharedPoller(t, dir, stub, time.Now)
		for range 4 {
			for _, row := range []int64{1, 2} {
				polls.Go(func() {
					if report, err := poller.Poll(context.Background(), claudeRow(row)); err != nil || len(report.Windows) == 0 {
						t.Errorf("account %d answered %+v, %v", row, report, err)
					}
				})
			}
		}
	}
	polls.Wait()
	if asked := stub.asked.Load(); asked != 2 {
		t.Fatalf("twelve callers on each of two accounts asked %d times, want 2", asked)
	}
}

func TestAFailureWithoutRetryAfterCoolsDownDoublingToTheCap(t *testing.T) {
	dir, stub, clock := t.TempDir(), newUsageStub(t), &testClock{at: recordedNow}
	stub.status.Store(http.StatusBadGateway)
	poller := sharedPoller(t, dir, stub, clock.now)
	var gaps []time.Duration
	for range 6 {
		report, _ := poller.Poll(context.Background(), claudeRow(1))
		gaps = append(gaps, report.RetryAt.Sub(clock.now()))
		clock.pass(report.RetryAt.Sub(clock.now()))
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	for i := range want {
		if gaps[i] != want[i] {
			t.Fatalf("cooldowns were %v, want %v", gaps, want)
		}
	}
	if asked := stub.asked.Load(); asked != 6 {
		t.Fatalf("six expired cooldowns asked %d times", asked)
	}
}

func TestACanceledPollLeavesNoCooldown(t *testing.T) {
	dir, stub, clock := t.TempDir(), newUsageStub(t), &testClock{at: recordedNow}
	stub.delay = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _ = sharedPoller(t, dir, stub, clock.now).Poll(ctx, claudeRow(1))
	report, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1))
	if err != nil || len(report.Windows) == 0 || stub.asked.Load() != 2 {
		t.Fatalf("after a canceled poll the next answered %+v, %v, asked %d", report, err, stub.asked.Load())
	}
}

func anthropicReply(fiveHour, sevenDay string) http.Header {
	header := http.Header{}
	header.Set("anthropic-ratelimit-unified-5h-utilization", fiveHour)
	header.Set("anthropic-ratelimit-unified-5h-reset", "1780490000")
	header.Set("anthropic-ratelimit-unified-7d-utilization", sevenDay)
	header.Set("anthropic-ratelimit-unified-7d-reset", "1780704000")
	return header
}

func TestReplyHeadersFillTheReadingWithoutAskingTheEndpoint(t *testing.T) {
	dir, stub, clock := t.TempDir(), newUsageStub(t), &testClock{at: recordedNow}
	poller := sharedPoller(t, dir, stub, clock.now)
	if _, err := poller.Poll(context.Background(), claudeRow(1)); err != nil {
		t.Fatal(err)
	}
	clock.pass(4 * time.Minute)
	if err := poller.Heard(claudeRow(1), anthropicReply("0.31", "0.5")); err != nil {
		t.Fatal(err)
	}
	clock.pass(4 * time.Minute)
	report, err := sharedPoller(t, dir, stub, clock.now).Poll(context.Background(), claudeRow(1))
	if err != nil || stub.asked.Load() != 1 || report.Source != SourceHeaders || !report.FetchedAt.Equal(recordedNow.Add(4*time.Minute)) {
		t.Fatalf("after a reply the poll answered %+v, %v, asked %d", report, err, stub.asked.Load())
	}
	if windowUsed(t, report, fiveHourWindow) != 0.31 || windowUsed(t, report, sevenDayWindow) != 0.5 || windowUsed(t, report, "7d:opus") != 0.2 {
		t.Fatalf("the merged reading is %+v", report.Windows)
	}
}

func TestCodexReplyHeadersReadPercentAndMinutes(t *testing.T) {
	header := http.Header{}
	header.Set("x-codex-primary-used-percent", "12.5")
	header.Set("x-codex-primary-window-minutes", "300")
	header.Set("x-codex-primary-reset-at", "1780490000")
	header.Set("x-codex-secondary-used-percent", "60")
	header.Set("x-codex-secondary-window-minutes", "10080")
	report := fromCodexHeaders(header, recordedNow)
	if windowUsed(t, report, fiveHourWindow) != 0.125 || windowUsed(t, report, sevenDayWindow) != 0.6 {
		t.Fatalf("codex headers read %+v", report.Windows)
	}
}

func TestRepliesInsideAMinuteMergeOnlyWhenAWindowIsSpent(t *testing.T) {
	dir, stub, clock := t.TempDir(), newUsageStub(t), &testClock{at: recordedNow}
	poller := sharedPoller(t, dir, stub, clock.now)
	for _, reply := range []struct {
		fiveHour string
		want     float64
	}{{"0.3", 0.3}, {"0.4", 0.3}, {"1", 1}} {
		if err := poller.Heard(claudeRow(1), anthropicReply(reply.fiveHour, "0.5")); err != nil {
			t.Fatal(err)
		}
		report, _ := poller.Poll(context.Background(), claudeRow(1))
		if got := windowUsed(t, report, fiveHourWindow); got != reply.want {
			t.Fatalf("after a reply of %s the reading is %v, want %v", reply.fiveHour, got, reply.want)
		}
		clock.pass(10 * time.Second)
	}
	if stub.asked.Load() != 0 {
		t.Fatalf("readings from replies asked the endpoint %d times", stub.asked.Load())
	}
}

func TestFreshnessIsJitteredAndShorterNearALimitOnlyFromTheEndpoint(t *testing.T) {
	poller := &Poller{spread: func() float64 { return 0 }}
	near := Report{Source: SourceEndpoint, Windows: []Window{reported(0.92, fiveHourWindow, time.Time{})}}
	for _, check := range []struct {
		report Report
		spread float64
		want   time.Duration
	}{
		{Report{Source: SourceEndpoint}, 0, freshFor * 3 / 4},
		{Report{Source: SourceEndpoint}, 1, freshFor * 5 / 4},
		{near, 0.5, nearLimitCloseFor},
		{Report{Source: SourceHeaders, Windows: near.Windows}, 0.5, freshFor},
	} {
		poller.spread = func() float64 { return check.spread }
		if got := poller.freshFor(check.report); got != check.want {
			t.Errorf("freshness of %+v at spread %v is %v, want %v", check.report, check.spread, got, check.want)
		}
	}
}
