package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/transport"
)

const quietTestIdle = 100 * time.Millisecond

func saidHi() Request {
	return Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}
}

func quietThenWhole(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		post := posts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string) {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			w.(http.Flusher).Flush()
		}
		send(`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`)
		send(`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
		send(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}`)
		if post == 1 {
			<-r.Context().Done()
			return
		}
		send(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`)
		send(`{"type":"content_block_stop","index":0}`)
		send(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`)
		send(`{"type":"message_stop"}`)
	}))
	t.Cleanup(server.Close)
	return server, &posts
}

func quietWire(t *testing.T, url string, retries int) *Wire {
	t.Helper()
	wire, err := New(Config{
		BaseURL:    url,
		Model:      "claude-test",
		Proxy:      true,
		StreamIdle: quietTestIdle,
		Token:      func(context.Context, string) (string, error) { return "sk-ant-api-test", nil },
		Transport:  transport.Config{Retries: retries},
	})
	if err != nil {
		t.Fatalf("new wire: %v", err)
	}
	return wire
}

func TestAStreamThatGoesQuietAfterOneDeltaFailsAsRetryableWithinTheIdleTimeout(t *testing.T) {
	server, posts := quietThenWhole(t)
	started := time.Now()
	_, _, err := quietWire(t, server.URL, 0).Ask(context.Background(), saidHi())
	took := time.Since(started)
	if !errors.Is(err, llm.ErrStreamIdle) {
		t.Fatalf("a stream quiet for longer than %s ended with %v, not the idle failure", quietTestIdle, err)
	}
	if transport.KindOf(err).Fatal() {
		t.Fatalf("the idle failure is %s, which the retry treats as fatal", transport.KindOf(err))
	}
	if took > 10*quietTestIdle {
		t.Fatalf("the idle failure took %s against an idle timeout of %s", took, quietTestIdle)
	}
	if posts.Load() != 1 {
		t.Fatalf("with no retries the wire posted %d times", posts.Load())
	}
	t.Logf("quiet after one delta: failed in %s against %s, kind %s", took, quietTestIdle, transport.KindOf(err))
}

func TestAStreamThatGoesQuietIsPostedAgainAndTheSecondAttemptAnswers(t *testing.T) {
	server, posts := quietThenWhole(t)
	result, _, err := quietWire(t, server.URL, 1).Ask(context.Background(), saidHi())
	if err != nil {
		t.Fatalf("the retry did not answer: %v", err)
	}
	if posts.Load() != 2 {
		t.Fatalf("the wire posted %d times, not twice", posts.Load())
	}
	if result.Content != "hello" {
		t.Fatalf("the answer was %q, not the second attempt's whole text", result.Content)
	}
	if result.FirstTokenMS < quietTestIdle.Milliseconds() {
		t.Fatalf("first token at %d ms, sooner than the %s the first attempt sat quiet", result.FirstTokenMS, quietTestIdle)
	}
	t.Logf("posted %d times, answered %q, first token %d ms from the first send", posts.Load(), result.Content, result.FirstTokenMS)
}
