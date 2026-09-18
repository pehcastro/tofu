package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(waits *[]time.Duration) Config {
	return Config{
		AttemptTimeout: 500 * time.Millisecond,
		Retries:        1,
		Backoff:        50 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
		Concurrency:    2,
		Sleep: func(ctx context.Context, wait time.Duration) error {
			*waits = append(*waits, wait)
			return nil
		},
		NewRequestID: func() string { return "req-fixed" },
	}
}

func TestDoCarriesTheRequestIdAndTheBody(t *testing.T) {
	var seenID, seenBody, seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenID, seenBody, seenAuth = r.Header.Get(RequestIDHeader), string(body), r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var waits []time.Duration
	client, err := New(testConfig(&waits))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer secret-value")
	response, err := client.Do(context.Background(), Request{
		Method: http.MethodPost, URL: server.URL, Body: []byte(`{"q":1}`), Header: header,
	})
	if err != nil {
		t.Fatalf("doing the request: %v", err)
	}
	if seenID != "req-fixed" || response.RequestID != "req-fixed" {
		t.Fatalf("request id sent %q, returned %q", seenID, response.RequestID)
	}
	if seenBody != `{"q":1}` {
		t.Fatalf("body was %q", seenBody)
	}
	if seenAuth != "Bearer secret-value" {
		t.Fatalf("the caller header was not forwarded: %q", seenAuth)
	}
	if response.Attempts != 1 || string(response.Body) != `{"ok":true}` {
		t.Fatalf("response is %+v", response)
	}
}

func TestDoRetriesAndHonoursRetryAfterMillisFirst(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set(RetryAfterMillis, "120")
			w.Header().Set(RetryAfter, "30")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var waits []time.Duration
	client, err := New(testConfig(&waits))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL})
	if err != nil {
		t.Fatalf("doing the request: %v", err)
	}
	if response.Attempts != 2 {
		t.Fatalf("expected two attempts, got %d", response.Attempts)
	}
	if len(waits) != 1 || waits[0] != 120*time.Millisecond {
		t.Fatalf("expected one wait of 120ms, got %v", waits)
	}
}

func TestDoCapsTheAdvisedWait(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set(RetryAfter, "30")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var waits []time.Duration
	client, err := New(testConfig(&waits))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL}); err != nil {
		t.Fatalf("doing the request: %v", err)
	}
	if len(waits) != 1 || waits[0] != 200*time.Millisecond {
		t.Fatalf("expected the wait capped at 200ms, got %v", waits)
	}
}

func TestDoUsesTheConfiguredBackoffWhenTheServerAdvisesNothing(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var waits []time.Duration
	client, err := New(testConfig(&waits))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL}); err != nil {
		t.Fatalf("doing the request: %v", err)
	}
	if len(waits) != 1 || waits[0] != 50*time.Millisecond {
		t.Fatalf("expected the configured backoff, got %v", waits)
	}
}

func TestDoStopsOnAFatalStatus(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"no key"}`))
	}))
	defer server.Close()

	var waits []time.Duration
	client, err := New(testConfig(&waits))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	_, err = client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL})
	if KindOf(err) != KindAuth {
		t.Fatalf("expected kind auth, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one attempt on a fatal status, got %d", calls)
	}
	if len(waits) != 0 {
		t.Fatalf("expected no wait, got %v", waits)
	}
}

func TestDoReturnsTheLastFailureWhenTheRetryIsSpent(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	var waits []time.Duration
	client, err := New(testConfig(&waits))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	_, err = client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL})
	if KindOf(err) != KindRateLimit {
		t.Fatalf("expected kind rate_limit, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected two attempts, got %d", calls)
	}
}

func TestDoTimesOutPerAttempt(t *testing.T) {
	release := make(chan struct{})
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			<-release
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	defer close(release)

	var waits []time.Duration
	config := testConfig(&waits)
	config.AttemptTimeout = 80 * time.Millisecond
	client, err := New(config)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL})
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if response.Attempts != 2 {
		t.Fatalf("expected two attempts, got %d", response.Attempts)
	}
}

func TestDoHoldsTheConcurrencyCap(t *testing.T) {
	var live, peak int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&live, 1)
		mu.Lock()
		if current > peak {
			peak = current
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&live, -1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var waits []time.Duration
	config := testConfig(&waits)
	config.Concurrency = 2
	config.AttemptTimeout = 2 * time.Second
	client, err := New(config)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}

	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL}); err != nil {
				t.Errorf("doing the request: %v", err)
			}
		}()
	}
	group.Wait()
	if peak > 2 {
		t.Fatalf("expected at most two calls at once, saw %d", peak)
	}
	if peak == 0 {
		t.Fatal("no call reached the server")
	}
}

func TestNewRefusesAnImpossibleConfig(t *testing.T) {
	cases := []struct {
		name   string
		config Config
	}{
		{"no timeout", Config{Concurrency: 1}},
		{"negative retries", Config{AttemptTimeout: time.Second, Retries: -1, Concurrency: 1}},
		{"retries with no backoff", Config{AttemptTimeout: time.Second, Retries: 1, Concurrency: 1}},
		{"no concurrency", Config{AttemptTimeout: time.Second}},
		{"a cap below the backoff", Config{AttemptTimeout: time.Second, Retries: 1,
			Backoff: time.Second, MaxBackoff: time.Millisecond, Concurrency: 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.config); err == nil {
				t.Fatal("expected the config to be refused")
			}
		})
	}
}

func TestRetryAfterReadsAnHttpDate(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	var waits []time.Duration
	config := testConfig(&waits)
	config.Now = func() time.Time { return now }
	config.MaxBackoff = time.Hour
	client, err := New(config)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	header := http.Header{}
	header.Set(RetryAfter, now.Add(90*time.Second).Format(http.TimeFormat))
	if got := client.retryAfter(header); got != 90*time.Second {
		t.Fatalf("expected 90s from the date, got %v", got)
	}
}

func TestKindFatality(t *testing.T) {
	fatal := []Kind{KindUnknown, KindMissingCredential, KindAuth, KindBilling,
		KindModelAccess, KindBudget, KindRequestTooLarge, KindBadRequest}
	retryable := []Kind{KindRateLimit, KindTimeout, KindInvalidAnswer, KindProvider}
	for _, kind := range fatal {
		if !kind.Fatal() {
			t.Fatalf("%s must be fatal", kind)
		}
	}
	for _, kind := range retryable {
		if kind.Fatal() {
			t.Fatalf("%s must be retryable", kind)
		}
	}
}

func TestStatusKind(t *testing.T) {
	cases := map[int]Kind{
		400: KindBadRequest, 401: KindAuth, 402: KindBilling, 403: KindAuth,
		404: KindModelAccess, 408: KindTimeout, 413: KindRequestTooLarge,
		422: KindBadRequest, 429: KindRateLimit, 500: KindProvider, 529: KindProvider,
	}
	for status, want := range cases {
		if got := statusKind(status); got != want {
			t.Fatalf("status %d is %s, expected %s", status, got, want)
		}
	}
}
