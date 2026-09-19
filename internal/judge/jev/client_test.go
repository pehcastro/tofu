package jev

import (
	"context"
	"strings"
	"testing"

	"boji/internal/konst"
	"boji/internal/transport"
)

const stubRequestByteCap = 90000

type stubWire struct {
	caps  WireCaps
	body  []byte
	calls int
	reply string
}

func (s *stubWire) Caps() WireCaps { return s.caps }
func (s *stubWire) Model() string  { return "~typesafe/jev-latest" }

func (s *stubWire) Post(ctx context.Context, body []byte) (Raw, error) {
	s.calls++
	s.body = body
	return Raw{Body: []byte(s.reply), RequestID: "req-stub", Attempts: 1}, nil
}

func newStub(reply string) *stubWire {
	return &stubWire{
		caps: WireCaps{
			Name:              "stub",
			MaxRequestBytes:   stubRequestByteCap,
			MaxChoiceOptions:  konst.ChoiceCeiling,
			MaxScoreLevels:    konst.JudgeScoreLevelCeiling,
			ReturnsConfidence: true,
		},
		reply: reply,
	}
}

const gateReply = `{"model":"typesafe/jev-1.13-20260917",
 "answers":{
  "act":{"type":"choice","choice":"read","probabilities":{"read":0.8,"write":0.15,"ask":0.05},"confidence":0.75},
  "risk":{"type":"score","score":0,"legend":{"0":"none"},"probabilities":{"0":1,"1":0,"2":0,"3":0},"confidence":1},
  "approval":{"type":"noul","noul":0.11}},
 "usage":{"input_tokens":776,"output_tokens":69,"cost":0.000032592},
 "id":"gen-dec-1789749261-nXRyUDXmVQe5htOwSK6M",
 "provider":"TypeSafe"}`

func TestAskReturnsTheBuildTheResponseReported(t *testing.T) {
	wire := newStub(gateReply)
	client, err := NewClient(Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	decision, err := client.Ask(context.Background(), battery())
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision.Build != "typesafe/jev-1.13-20260917" {
		t.Fatalf("expected the dated build, got %q", decision.Build)
	}
	if decision.Alias != "~typesafe/jev-latest" {
		t.Fatalf("expected the alias to be kept apart from the build, got %q", decision.Alias)
	}
	if decision.RequestID != "gen-dec-1789749261-nXRyUDXmVQe5htOwSK6M" {
		t.Fatalf("expected the request id from the body, got %q", decision.RequestID)
	}
	if decision.TransportID != "req-stub" {
		t.Fatalf("expected the transport id, got %q", decision.TransportID)
	}
	if decision.Answers["approval"].Noul != 0.11 {
		t.Fatalf("expected the noul, got %v", decision.Answers["approval"].Noul)
	}
	if !strings.Contains(string(wire.body), `"model":"~typesafe/jev-latest"`) {
		t.Fatalf("expected the alias on the wire, got %s", wire.body)
	}
}

func TestAskRefusesARequestOverTheWireByteCap(t *testing.T) {
	wire := newStub(gateReply)
	client, err := NewClient(Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	request := battery()
	request.State = map[string]string{"blob": strings.Repeat("x", stubRequestByteCap)}

	_, err = client.Ask(context.Background(), request)
	if err == nil {
		t.Fatal("expected an oversize request to be refused")
	}
	if kind := transport.KindOf(err); kind != transport.KindRequestTooLarge {
		t.Fatalf("expected kind request_too_large, got %s", kind)
	}
	if !strings.Contains(err.Error(), "accepts at most 90000") {
		t.Fatalf("expected the cap in the refusal, got %v", err)
	}
	if wire.calls != 0 {
		t.Fatalf("expected nothing to be sent, the wire saw %d calls", wire.calls)
	}
}

func TestAskSendsARequestJustUnderTheByteCap(t *testing.T) {
	wire := newStub(gateReply)
	client, err := NewClient(Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	request := battery()
	request.State = map[string]string{"blob": strings.Repeat("x", stubRequestByteCap-600)}

	if _, err := client.Ask(context.Background(), request); err != nil {
		t.Fatalf("expected the call to go out, got %v", err)
	}
	if wire.calls != 1 {
		t.Fatalf("expected one call, got %d", wire.calls)
	}
	if len(wire.body) > stubRequestByteCap {
		t.Fatalf("the body was %d bytes, over the cap", len(wire.body))
	}
}

func TestAskRejectsAnInvalidAnswer(t *testing.T) {
	wire := newStub(`{"model":"typesafe/jev-1.13-20260917",
 "answers":{
  "act":{"type":"choice","choice":"read","probabilities":{"read":0.6,"write":0.15,"ask":0.05},"confidence":0.75},
  "risk":{"type":"score","score":0,"probabilities":{"0":1,"1":0,"2":0,"3":0},"confidence":1},
  "approval":{"type":"noul","noul":0.11}},
 "usage":{"input_tokens":10,"output_tokens":1,"cost":0.0001},"id":"gen-1","provider":"TypeSafe"}`)
	client, err := NewClient(Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	if _, err := client.Ask(context.Background(), battery()); transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
}

func TestAskRecordsTheReportedCostOnTheRow(t *testing.T) {
	client, err := NewClient(Config{Wire: newStub(gateReply)})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	decision, err := client.Ask(context.Background(), battery())
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	t.Logf("build %s tokens %d/%d cost $%.9f", decision.Build,
		decision.Usage.InputTokens, decision.Usage.OutputTokens, decision.Usage.Cost)
	if decision.Usage.Cost != 0.000032592 {
		t.Fatalf("expected the cost the wire reported, got %v", decision.Usage.Cost)
	}
}
