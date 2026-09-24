package anthropic

import (
	"encoding/json"
	"testing"
)

func decodedSystem(t *testing.T, request Request) []systemBlock {
	t.Helper()
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		System []systemBlock `json:"system"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return decoded.System
}

func TestTheAnchorNeverLandsOnTheSoleBillingBlock(t *testing.T) {
	request := minimalRequest()
	request.System = []string{BillingHeaderPrefix + " cc_version=x; cc_entrypoint=cli; cch=00000;"}

	system := decodedSystem(t, request)
	if len(system) != 1 {
		t.Fatalf("system blocks are %+v", system)
	}
	if system[0].CacheControl != nil {
		t.Fatal("the anchor landed on the one block that varies with every request")
	}
}

func TestTheAnchorSkipsAnAlreadyBilledBlockToReachTheCallersOwnTail(t *testing.T) {
	request := minimalRequest()
	request.System = []string{
		BillingHeaderPrefix + " cc_version=x; cc_entrypoint=cli; cch=00000;",
		"be terse",
	}

	system := decodedSystem(t, request)
	if len(system) != 2 {
		t.Fatalf("system blocks are %+v", system)
	}
	if system[0].CacheControl != nil {
		t.Fatal("the anchor landed on the billing block instead of walking past it")
	}
	if system[1].CacheControl == nil {
		t.Fatal("the caller's own stable block never got the anchor")
	}
}
