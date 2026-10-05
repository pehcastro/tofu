package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/web"
)

const slowAnswer = 500 * time.Millisecond

func slowServer(t *testing.T, status int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(slowAnswer)
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte("a page"))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func fetchAll(client *web.Client, addresses []string) []error {
	failures := make([]error, len(addresses))
	var group sync.WaitGroup
	for index, address := range addresses {
		group.Go(func() {
			_, failures[index] = client.Get(context.Background(), address)
		})
	}
	group.Wait()
	return failures
}

func TestConcurrentFetchesOfOneURLReachTheServerOnce(t *testing.T) {
	server, hits := slowServer(t, http.StatusOK)
	client := web.NewClient(web.Config{MaxPageBytes: 1 << 20, TimeoutMS: 5000})
	addresses := make([]string, 10)
	for index := range addresses {
		addresses[index] = server.URL + "/one#part" + string(rune('a'+index))
	}
	for _, err := range fetchAll(client, addresses) {
		if err != nil {
			t.Fatalf("a fetch failed: %v", err)
		}
	}
	t.Logf("server count for ten concurrent fetches of one URL: %d", hits.Load())
	if hits.Load() != 1 {
		t.Fatalf("the server was reached %d times, want 1", hits.Load())
	}
}

func TestConcurrentFetchesOfTwoURLsRunTogether(t *testing.T) {
	server, hits := slowServer(t, http.StatusOK)
	client := web.NewClient(web.Config{MaxPageBytes: 1 << 20, TimeoutMS: 5000})
	started := time.Now()
	fetchAll(client, []string{server.URL + "/one", server.URL + "/two"})
	elapsed := time.Since(started)
	t.Logf("two different URLs at once: %d requests in %v", hits.Load(), elapsed)
	if hits.Load() != 2 || elapsed >= 2*slowAnswer*9/10 {
		t.Fatalf("two URLs took %v over %d requests, want 2 requests near %v", elapsed, hits.Load(), slowAnswer)
	}
}

func TestAFailureIsSharedByWaitersAndNotKept(t *testing.T) {
	server, hits := slowServer(t, http.StatusServiceUnavailable)
	client := web.NewClient(web.Config{MaxPageBytes: 1 << 20, TimeoutMS: 5000})
	for _, err := range fetchAll(client, []string{server.URL, server.URL, server.URL}) {
		var status web.StatusError
		if !errors.As(err, &status) || status.Status != http.StatusServiceUnavailable {
			t.Fatalf("a waiter got %v, want the shared 503", err)
		}
	}
	fetchAll(client, []string{server.URL})
	if hits.Load() != 2 {
		t.Fatalf("the server was reached %d times, want 1 for the three waiters and 1 for the retry", hits.Load())
	}
}

func TestACancelledCallerStopsWaitingAndOthersStillGetThePage(t *testing.T) {
	server, hits := slowServer(t, http.StatusOK)
	client := web.NewClient(web.Config{MaxPageBytes: 1 << 20, TimeoutMS: 5000})
	impatient, cancel := context.WithTimeout(context.Background(), slowAnswer/5)
	defer cancel()
	started := time.Now()
	_, err := client.Get(impatient, server.URL)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) >= slowAnswer/2 {
		t.Fatalf("the cancelled caller returned %v after %v, want a deadline error well before %v", err, time.Since(started), slowAnswer)
	}
	page, err := client.Get(context.Background(), server.URL)
	if err != nil || page.URL == "" {
		t.Fatalf("the patient caller got %v, want the page", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("the server was reached %d times, want 1: the patient caller joins the fetch the cancelled one started", hits.Load())
	}
}
