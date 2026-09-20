package llm

import (
	"context"
	"testing"
	"time"

	"tofu/internal/transport"
)

type fakeWire struct {
	model    string
	body     []byte
	err      error
	requests int
}

func (w *fakeWire) Model() string { return w.model }

func (w *fakeWire) Post(ctx context.Context, body []byte) (Raw, error) {
	w.requests++
	if w.err != nil {
		return Raw{}, w.err
	}
	return Raw{Body: w.body, RequestID: "req-fixed", Attempts: 1, Latency: time.Millisecond}, nil
}

func TestNewClientRefusesAClientWithNoWire(t *testing.T) {
	_, err := NewClient(nil)
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestAskReturnsADecisionFromTheDecodedResponse(t *testing.T) {
	wire := &fakeWire{
		model: "anthropic/claude-fable-5-1",
		body: []byte(`{"id":"gen-1","model":"anthropic/claude-fable-5.1","choices":[
{"finish_reason":"stop","message":{"content":"done"}}],
"usage":{"prompt_tokens":12,"completion_tokens":3,"cost":0.000009}}`),
	}
	client, err := NewClient(wire)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	decision, err := client.Ask(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision.Build != "anthropic/claude-fable-5.1" || decision.RequestID != "gen-1" || decision.TransportID != "req-fixed" {
		t.Fatalf("decision is %+v", decision)
	}
	if decision.Outcome != OutcomeMessage || decision.Content != "done" {
		t.Fatalf("decision is %+v", decision)
	}
	if decision.Usage.Cost != 0.000009 {
		t.Fatalf("decision usage is %+v", decision.Usage)
	}
}

func TestAskReturnsTheWireErrorWhenPostFails(t *testing.T) {
	wire := &fakeWire{model: "m", err: transport.Fail("fakeWire.Post", transport.KindProvider, nil, "the wire is down")}
	client, err := NewClient(wire)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	_, err = client.Ask(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if transport.KindOf(err) != transport.KindProvider {
		t.Fatalf("expected kind provider, got %v", err)
	}
	if wire.requests != 1 {
		t.Fatalf("expected exactly one request, wire saw %d", wire.requests)
	}
}

func TestAskRejectsAMalformedEncodeBeforePosting(t *testing.T) {
	wire := &fakeWire{model: "m"}
	client, err := NewClient(wire)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	_, err = client.Ask(context.Background(), Request{})
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
	if wire.requests != 0 {
		t.Fatalf("expected nothing posted, wire saw %d requests", wire.requests)
	}
}
