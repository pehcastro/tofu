package codex

import (
	"errors"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func TestAFailedCodexStreamIsMarkedForAnotherPostOnlyWhenTheCauseIsTransient(t *testing.T) {
	for _, failed := range []struct {
		event string
		said  string
		again bool
	}{
		{`{"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"busy"}}}`, "busy", true},
		{`{"type":"error","error":{"type":"server_error","message":"oops"}}`, "oops", true},
		{`{"type":"response.failed","response":{"error":{"code":"insufficient_quota","message":"no quota"}}}`, "no quota", false},
		{`{"type":"response.output_text.delta","delta":"half"}`, "drained before a terminal event", true},
	} {
		_, err := ReadStream(strings.NewReader("data: "+failed.event+"\n\n"), nil)
		if err == nil {
			t.Fatalf("%s ended without a failure", failed.event)
		}
		if errors.Is(err, llm.ErrStreamBroke) != failed.again || !strings.Contains(err.Error(), failed.said) {
			t.Errorf("%s ended with %v: want %q in it, and posting again %v", failed.event, err, failed.said, failed.again)
		}
	}
}
