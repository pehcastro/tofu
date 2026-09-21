package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tofu/internal/transport"
)

func recordingPlan(retries int, waits *[]time.Duration) transport.Config {
	return transport.Config{
		Retries:    retries,
		Backoff:    100 * time.Millisecond,
		MaxBackoff: 400 * time.Millisecond,
		Growth:     2,
		Sleep: func(_ context.Context, wait time.Duration) error {
			*waits = append(*waits, wait)
			return nil
		},
	}
}

func post(t *testing.T, plan transport.Config, handler http.HandlerFunc) (*http.Response, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := &http.Client{Transport: Retrying(nil, plan)}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	return client.Do(request)
}

func TestEveryAttemptCarriesTheBodyAgain(t *testing.T) {
	var waits []time.Duration
	var bodies []string
	response, err := post(t, recordingPlan(2, &waits), func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if len(bodies) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if len(bodies) != 3 {
		t.Fatalf("the stub saw %d requests, want the first and both retries", len(bodies))
	}
	for attempt, body := range bodies {
		if body != "hello" {
			t.Fatalf("attempt %d carried %q", attempt+1, body)
		}
	}
	if len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 200*time.Millisecond {
		t.Fatalf("the waits are %v, want the konst schedule doubling", waits)
	}
}

func TestTheTotalWaitCeilingEndsTheRetries(t *testing.T) {
	var waits []time.Duration
	plan := recordingPlan(4, &waits)
	plan.TotalWait = 250 * time.Millisecond
	served := 0
	response, err := post(t, plan, func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusInternalServerError)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("the last answer is %d", response.StatusCode)
	}
	if served != 2 || len(waits) != 1 {
		t.Fatalf("the stub saw %d requests over waits %v, and 100ms plus 200ms passes a 250ms ceiling", served, waits)
	}
}

func TestRetryAfterIsHonouredOverTheBackoff(t *testing.T) {
	var waits []time.Duration
	served := 0
	response, err := post(t, recordingPlan(2, &waits), func(w http.ResponseWriter, _ *http.Request) {
		served++
		if served == 1 {
			w.Header().Set(transport.RetryAfterHeader, "0.25")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if len(waits) != 1 || waits[0] != 250*time.Millisecond {
		t.Fatalf("the waits are %v, and the answer asked for 250ms", waits)
	}
}

func TestAPlanWithNoRetriesLeavesTheRoundTripperAlone(t *testing.T) {
	if got := Retrying(nil, transport.Config{}); got != nil {
		t.Fatalf("a plan with no retries wrapped the transport in %T", got)
	}
}
