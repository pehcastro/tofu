package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"tofu/internal/transport"
)

func servedInTurn(t *testing.T, first string, reset bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	read := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "turn-survives", name))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	firstBody, answerBody := read(first), read("answer.sse")
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if posts.Add(1) > 1 {
			_, _ = w.Write(answerBody)
			return
		}
		if reset {
			w.Header().Set("Content-Length", strconv.Itoa(len(firstBody)+64))
		}
		_, _ = w.Write(firstBody)
	}))
	t.Cleanup(server.Close)
	return server, &posts
}

func TestAStreamCutAfterTwoHundredIsPostedAgainOnlyWhenTheCauseIsTransient(t *testing.T) {
	for _, cut := range []struct {
		name  string
		first string
		reset bool
		posts int32
		shown string
	}{
		{"an overloaded_error event after some text", "overloaded-cut.sse", false, 2, "the answer, whole"},
		{"a connection lost after some text", "half.sse", true, 2, "the answer, whole"},
		{"a stream that ends after some text with no message_stop", "half.sse", false, 2, "the answer, whole"},
		{"an invalid_request_error event", "invalid-request.sse", false, 1, ""},
	} {
		t.Run(cut.name, func(t *testing.T) {
			server, posts := servedInTurn(t, cut.first, cut.reset)
			wire, err := New(Config{
				BaseURL:   server.URL,
				Model:     "claude-test",
				Proxy:     true,
				Token:     func(context.Context, string) (string, error) { return "sk-ant-api-test", nil },
				Transport: transport.Config{Retries: 3},
			})
			if err != nil {
				t.Fatal(err)
			}
			shown, resets := "", 0
			request := saidHi()
			request.OnDelta = func(text string) { shown += text }
			request.OnRetry = func() { shown, resets = "", resets+1 }
			result, _, err := wire.Ask(context.Background(), request)
			if posts.Load() != cut.posts {
				t.Fatalf("posted %d times, not %d, and ended with %v", posts.Load(), cut.posts, err)
			}
			if cut.posts == 1 {
				if err == nil {
					t.Fatalf("a fatal stream error answered %q", result.Content)
				}
				return
			}
			if err != nil {
				t.Fatalf("the retry did not answer: %v", err)
			}
			if result.Content != cut.shown || shown != cut.shown || resets != 1 {
				t.Fatalf("answered %q and showed %q after %d resets, not %q after one", result.Content, shown, resets, cut.shown)
			}
		})
	}
}
