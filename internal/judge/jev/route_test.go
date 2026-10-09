package jev

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"tofu/internal/transport"
)

func TestARouteFailsOverOnTheAccountNeverOnTheRequest(t *testing.T) {
	answered := func(status int) error {
		return &transport.Error{Kind: transport.StatusKind(status), Op: "transport.Do", Status: status, Detail: "stub"}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, each := range []struct {
		name  string
		ctx   context.Context
		err   error
		moves bool
	}{
		{"402 insufficient credits", context.Background(), answered(http.StatusPaymentRequired), true},
		{"429 after the retries", context.Background(), answered(http.StatusTooManyRequests), true},
		{"403 key limit exceeded", context.Background(), answered(http.StatusForbidden), true},
		{"401 a revoked key", context.Background(), answered(http.StatusUnauthorized), true},
		{"404 the route does not serve jev", context.Background(), answered(http.StatusNotFound), true},
		{"503 the provider is down", context.Background(), answered(http.StatusServiceUnavailable), true},
		{"connection refused", context.Background(), &transport.Error{Kind: transport.KindProvider, Op: "transport.Do", Err: errors.New("connection refused")}, true},
		{"the attempt timed out", context.Background(), &transport.Error{Kind: transport.KindTimeout, Op: "transport.Do"}, true},
		{"a spend budget", context.Background(), transport.Fail("x", transport.KindBudget, nil, "over"), true},
		{"400 the request is refused", context.Background(), answered(http.StatusBadRequest), false},
		{"413 the request is too large", context.Background(), answered(http.StatusRequestEntityTooLarge), false},
		{"refused locally as too large", context.Background(), transport.Fail("jev.Ask", transport.KindRequestTooLarge, nil, "big"), false},
		{"a malformed answer", context.Background(), transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "no build"), false},
		{"an untyped error", context.Background(), errors.New("plain"), false},
		{"the person cancelled", cancelled, &transport.Error{Kind: transport.KindUnknown, Op: "transport.Do", Err: context.Canceled}, false},
		{"a 402 after the person cancelled", cancelled, answered(http.StatusPaymentRequired), false},
		{"no error", context.Background(), nil, false},
	} {
		if got := failsOver(each.ctx, each.err); got != each.moves {
			t.Errorf("%s: fails over %v, want %v", each.name, got, each.moves)
		}
	}
}

type stubWire struct {
	name, alias string
	fail        error
	sent        []string
}

func (s *stubWire) Caps() WireCaps { return WireCaps{Name: s.name} }

func (s *stubWire) Model() string { return s.alias }

func (s *stubWire) Post(_ context.Context, body []byte) (Raw, error) {
	s.sent = append(s.sent, string(body))
	if s.fail != nil {
		return Raw{}, s.fail
	}
	return Raw{Body: []byte(`{"model":"` + s.name + `-build","answers":{"a":{"type":"noul","noul":0.2}}}`)}, nil
}

func TestAClientAsksTheFallbackOnlyWhenTheAccountFails(t *testing.T) {
	request := Request{State: map[string]string{"tool": "bash"}, Questions: []Question{{ID: "a", Kind: QuestionNoul, True: "t", False: "f"}}}
	credits := &transport.Error{Kind: transport.KindBilling, Op: "transport.Do", Status: http.StatusPaymentRequired, Detail: "Insufficient credits"}
	refused := &transport.Error{Kind: transport.KindBadRequest, Op: "transport.Do", Status: http.StatusBadRequest, Detail: "bad"}
	down := &transport.Error{Kind: transport.KindProvider, Op: "transport.Do", Status: http.StatusBadGateway}
	aliases := map[string]string{"openrouter": "~typesafe/jev-latest", "typesafe": "jev-latest"}
	for _, each := range []struct {
		name         string
		primary      error
		fallback     error
		noFallback   bool
		wantWire     string
		wantFailover bool
		wantKind     transport.Kind
		fallbackSent int
	}{
		{name: "the primary answers", wantWire: "openrouter"},
		{name: "402 moves to the other wire", primary: credits, wantWire: "typesafe", wantFailover: true, fallbackSent: 1},
		{name: "400 stays and fails", primary: refused, wantKind: transport.KindBadRequest},
		{name: "both down reports the primary first", primary: credits, fallback: down, wantKind: transport.KindBilling, fallbackSent: 1},
		{name: "no fallback behaves as one wire", primary: credits, noFallback: true, wantKind: transport.KindBilling},
	} {
		t.Run(each.name, func(t *testing.T) {
			primary := &stubWire{name: "openrouter", alias: aliases["openrouter"], fail: each.primary}
			fallback := &stubWire{name: "typesafe", alias: aliases["typesafe"], fail: each.fallback}
			var failovers []Failover
			config := Config{Wire: primary, Fallback: fallback, OnFailover: func(f Failover) { failovers = append(failovers, f) }}
			if each.noFallback {
				config.Fallback = nil
			}
			client, err := NewClient(config)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := client.Ask(context.Background(), request)
			if len(fallback.sent) != each.fallbackSent {
				t.Fatalf("the fallback was asked %d times, want %d", len(fallback.sent), each.fallbackSent)
			}
			for _, body := range fallback.sent {
				if !strings.HasPrefix(body, `{"model":"jev-latest"`) {
					t.Fatalf("the fallback was sent %s, which carries another wire's alias", body)
				}
			}
			if each.wantWire == "" {
				if transport.KindOf(err) != each.wantKind {
					t.Fatalf("err %v, want kind %s", err, each.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if decision.Wire != each.wantWire || decision.Build != each.wantWire+"-build" || decision.Alias != aliases[each.wantWire] {
				t.Fatalf("answered by %q with build %q alias %q, want %q", decision.Wire, decision.Build, decision.Alias, each.wantWire)
			}
			if each.wantFailover != (len(failovers) == 1 && failovers[0].From == "openrouter" && failovers[0].To == "typesafe" && transport.KindOf(failovers[0].Cause) == transport.KindBilling) {
				t.Fatalf("failovers %+v, want one from openrouter to typesafe on billing: %v", failovers, each.wantFailover)
			}
		})
	}
}
