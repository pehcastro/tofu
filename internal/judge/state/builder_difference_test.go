package state

import (
	"encoding/json"
	"testing"

	"tofu/internal/judge/ledger"
)

const recordedHeredocTicketCase = "tx-055"

func TestTheRicherBuilderAddsWriteTargetsAndNothingElse(t *testing.T) {
	one, ok := gateCases(t)[recordedHeredocTicketCase]
	if !ok {
		t.Fatalf("%s is not in the gate corpus", recordedHeredocTicketCase)
	}
	in := recordedInput(t, one)

	shipped, shippedVersion, err := BuildToolGate(in)
	if err != nil {
		t.Fatalf("BuildToolGate: %v", err)
	}
	richer, richerVersion, err := BuildToolGateV3(in)
	if err != nil {
		t.Fatalf("BuildToolGateV3: %v", err)
	}
	t.Logf("command: %v", in.Input["command"])
	t.Logf("%s: %s", shippedVersion, shipped)
	t.Logf("%s: %s", richerVersion, richer)

	var decoded map[string]any
	if err := json.Unmarshal(richer, &decoded); err != nil {
		t.Fatalf("decode the richer state: %v", err)
	}
	context, ok := decoded["context"].(map[string]any)
	if !ok {
		t.Fatalf("context is %#v", decoded["context"])
	}
	if _, present := context["write_targets"]; !present {
		t.Fatal("the richer state carries no write_targets")
	}
	delete(context, "write_targets")

	withoutTargets, err := ledger.Canonical(decoded)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if string(withoutTargets) != string(shipped) {
		t.Fatalf("the two builders differ by more than write_targets:\nshipped: %s\nricher less targets: %s", shipped, withoutTargets)
	}
}
