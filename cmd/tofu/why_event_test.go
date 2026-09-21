package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	sessionstore "tofu/internal/session"
	"tofu/internal/turn"
)

func recordOneCall(t *testing.T, callID string) string {
	t.Helper()
	t.Chdir(t.TempDir())
	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	id := turn.NewID(time.Now())
	row := turn.Row{
		ID: id, Schema: turn.SchemaVersion, At: time.Now(), Task: "read the note",
		Wire: "anthropic", Model: "stub-model", Spend: turn.SpendSubscription, Root: id,
		Steps: []turn.StepRow{{Index: 1, ToolCalls: []turn.ToolCallRow{{
			ID: callID, Tool: "bash", Command: "ls -la", ResultBytes: 1204, DurationMS: 41, GateVerdict: "allow",
		}}}},
		Outcome: turn.OutcomeStopped,
	}
	if err := turn.WriteSession(store, row); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWhyFindsTheCallChatDrewByTheHashItDrew(t *testing.T) {
	const callID = "4ae0dce7-6263-4a50-b3d7-cd3c3a016c47"
	session := recordOneCall(t, callID)

	var out, errOut bytes.Buffer
	if code := whyVerb([]string{"#016c47"}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("tofu why #016c47 exited %d: %s", code, errOut.String())
	}
	printed := out.String()
	for _, want := range []string{callID, session, "bash", "ls -la", "step 1", "attempt 1"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("tofu why printed %q, which does not name %q", printed, want)
		}
	}
}

func TestWhyStillSaysNothingIsThereWhenNoRecordCarriesTheHash(t *testing.T) {
	recordOneCall(t, "4ae0dce7-6263-4a50-b3d7-cd3c3a016c47")
	var out, errOut bytes.Buffer
	if code := whyVerb([]string{"#ffffff"}, &out, &errOut, time.Now); code == exitOK {
		t.Fatalf("tofu why #ffffff exited ok and printed %q", out.String())
	}
	if !strings.Contains(errOut.String(), "is not a row id") {
		t.Fatalf("tofu why #ffffff failed with %q, want the ledger's own refusal", errOut.String())
	}
}
