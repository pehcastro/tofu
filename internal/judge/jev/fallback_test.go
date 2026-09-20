package jev

import (
	"errors"
	"testing"

	"tofu/internal/transport"
)

func TestUnavailabilityNamesTheReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{err: transport.Fail("wire", transport.KindTimeout, nil, "no answer in 2.5 s"), want: "timeout"},
		{err: transport.Fail("wire", transport.KindAuth, nil, "the key was rejected"), want: "refused"},
		{err: transport.Fail("wire", transport.KindBilling, nil, "no credit"), want: "refused"},
		{err: transport.Fail("wire", transport.KindRequestTooLarge, nil, "90411 bytes"), want: "refused"},
		{err: transport.Fail("wire", transport.KindRateLimit, nil, "429"), want: "rate_limit"},
		{err: transport.Fail("wire", transport.KindProvider, nil, "no such host"), want: "transport"},
		{err: errors.New("dial tcp: lookup nowhere.invalid"), want: "transport"},
		{err: transport.Fail("wire", transport.KindInvalidAnswer, nil, "probabilities sum to 0.4"), want: "malformed_answer"},
	}
	for _, c := range cases {
		if got := Unavailability(c.err).String(); got != c.want {
			t.Fatalf("%v: reason = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestEveryTransportKindMapsToOneOfTheFiveReasons(t *testing.T) {
	known := map[string]bool{"timeout": true, "refused": true, "rate_limit": true, "transport": true, "malformed_answer": true}
	for kind := transport.KindUnknown; kind <= transport.KindProvider; kind++ {
		if name := Unavailability(transport.Fail("wire", kind, nil, "failed")).String(); !known[name] {
			t.Fatalf("kind %s produced the reason %q, which is not one of the five", kind, name)
		}
	}
}
