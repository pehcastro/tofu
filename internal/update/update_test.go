package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tofu/internal/transport"
)

func testClient(t *testing.T) *transport.Client {
	t.Helper()
	client, err := transport.New(transport.Config{AttemptTimeout: time.Second, Concurrency: 1})
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	return client
}

func servedVersion(t *testing.T, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestCheckReportsANewerVersionAsAvailable(t *testing.T) {
	url := servedVersion(t, `{"version":"0.5.0"}`)
	result, err := Check(context.Background(), testClient(t), url, "0.4.2")
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	if !result.Available {
		t.Fatal("a newer version served does not report available")
	}
	if result.Latest != "0.5.0" || result.Current != "0.4.2" {
		t.Fatalf("got %+v, want current 0.4.2 and latest 0.5.0", result)
	}
}

func TestCheckReportsTheSameVersionAsNotAvailable(t *testing.T) {
	url := servedVersion(t, `{"version":"0.4.2"}`)
	result, err := Check(context.Background(), testClient(t), url, "0.4.2")
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	if result.Available {
		t.Fatalf("the running version reports available: %+v", result)
	}
}

func TestCheckReportsAnOlderServedVersionAsNotAvailable(t *testing.T) {
	url := servedVersion(t, `{"version":"0.3.9"}`)
	result, err := Check(context.Background(), testClient(t), url, "0.4.2")
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	if result.Available {
		t.Fatalf("an older served version reports available: %+v", result)
	}
}

func TestRunIsSilentWithNoNetwork(t *testing.T) {
	result := Run(context.Background(), testClient(t), "http://127.0.0.1:1/does-not-listen", "0.4.2")
	if result.Available {
		t.Fatal("a failed check reports available")
	}
	if result != (Result{Current: "0.4.2"}) {
		t.Fatalf("a failed check does not leave the plain current value: %+v", result)
	}
}

func TestRunNeverBlocksOnAnEmptySource(t *testing.T) {
	start := time.Now()
	result := Run(context.Background(), testClient(t), Source(), "0.4.2")
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("an empty source took %v, want an immediate return", elapsed)
	}
	if result.Available {
		t.Fatal("no source configured reports available")
	}
}
