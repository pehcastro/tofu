package turn

import (
	"reflect"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func thought(decision llm.Decision, signature string) llm.Decision {
	decision.Thinking = llm.Thinking{Text: "reasoning before " + signature, Signature: signature}
	return decision
}

func TestASubAgentSentBackReplaysItsLastRequestUnchangedThinkingIncluded(t *testing.T) {
	run := gatedRun(t, "", rustDev,
		thought(edited("src/lib.rs"), "sig-1"), claimDecision("done"),
		thought(bashed("cargo clippy", 0), "sig-2"), claimDecision("done"),
		thought(bashed("cargo test", 0), "sig-3"), claimDecision("done"))
	reopenedFor(t, run.rows, 3)
	requests := run.spawn.base.Model.(*stubModel).requests
	boundaries := 0
	for i := 1; i < len(requests); i++ {
		before, after := requests[i-1].Messages, requests[i].Messages
		if !strings.Contains(after[len(after)-1].Content, "You reported this finished") {
			continue
		}
		boundaries++
		if len(after) < len(before) || !reflect.DeepEqual(before, after[:len(before)]) {
			t.Fatalf("round %d does not start with the last request of the round before:\nbefore %+v\nafter  %+v", boundaries+1, before, after)
		}
	}
	if boundaries != 2 {
		t.Fatalf("%d round boundaries in %d requests, want 2", boundaries, len(requests))
	}
}
