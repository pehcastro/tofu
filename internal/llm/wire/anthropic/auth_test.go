package anthropic_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/transport"
)

func revokedEndpoint(t *testing.T, revoked ...string) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		bearer := cmp.Or(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), r.Header.Get("X-Api-Key"))
		for _, token := range revoked {
			if bearer == token {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"revoked"}}`))
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answered by ` + bearer + `"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func renewing(renewals *atomic.Int32, renewed string) llm.TokenSource {
	return func(_ context.Context, rejected string) (string, error) {
		if rejected == "" {
			return "sk-ant-oat01-stale", nil
		}
		renewals.Add(1)
		return renewed, nil
	}
}

func ask(t *testing.T, server *httptest.Server, token llm.TokenSource) (anthropic.Result, error) {
	wire, err := anthropic.New(anthropic.Config{BaseURL: server.URL, Model: "claude-test", Proxy: true, Token: token})
	if err != nil {
		t.Fatalf("new wire: %v", err)
	}
	result, _, err := wire.Ask(context.Background(), anthropic.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	return result, err
}

func TestA401OnAFreshTokenRenewsItAndSendsOnceMore(t *testing.T) {
	server, calls := revokedEndpoint(t, "sk-ant-oat01-stale")
	var renewals atomic.Int32
	result, err := ask(t, server, renewing(&renewals, "sk-ant-oat01-renewed"))
	if err != nil || result.Content != "answered by sk-ant-oat01-renewed" {
		t.Fatalf("the turn after a 401 gave %q, %v", result.Content, err)
	}
	if calls.Load() != 2 || renewals.Load() != 1 {
		t.Fatalf("a 401 then a 200 took %d posts and %d renewals", calls.Load(), renewals.Load())
	}
}

func TestA401OnTheRenewedTokenIsFatalAfterOneResend(t *testing.T) {
	server, calls := revokedEndpoint(t, "sk-ant-oat01-stale", "sk-ant-oat01-renewed")
	var renewals atomic.Int32
	_, err := ask(t, server, renewing(&renewals, "sk-ant-oat01-renewed"))
	if transport.KindOf(err) != transport.KindAuth || calls.Load() != 2 || renewals.Load() != 1 {
		t.Fatalf("two 401s ended with %v after %d posts and %d renewals", err, calls.Load(), renewals.Load())
	}
}

func TestA401OnAKeyThatCannotChangeIsNotSentTwice(t *testing.T) {
	server, calls := revokedEndpoint(t, "sk-ant-api-key")
	_, err := ask(t, server, func(context.Context, string) (string, error) { return "sk-ant-api-key", nil })
	if transport.KindOf(err) != transport.KindAuth || calls.Load() != 1 {
		t.Fatalf("a 401 on a fixed key ended with %v after %d posts", err, calls.Load())
	}
}

func TestA401WhoseRenewalFailsStaysAnAuthFailure(t *testing.T) {
	server, calls := revokedEndpoint(t, "sk-ant-oat01-stale")
	refused := errors.New("cred: the claude-sub credential is disabled: invalid_grant")
	_, err := ask(t, server, func(_ context.Context, rejected string) (string, error) {
		if rejected != "" {
			return "", refused
		}
		return "sk-ant-oat01-stale", nil
	})
	if transport.KindOf(err) != transport.KindAuth || !errors.Is(err, refused) || calls.Load() != 1 {
		t.Fatalf("a failed renewal ended with %v after %d posts", err, calls.Load())
	}
}
