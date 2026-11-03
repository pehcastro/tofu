package session

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestASubAgentAskOutlivesTheLeadsTurn(t *testing.T) {
	for _, raised := range []string{"before the lead stops", "after the lead stops"} {
		t.Run(raised, func(t *testing.T) {
			model := New(fixed(), counted(new(int)))
			model.SetSize(100, 24)
			model.Start()
			if raised == "before the lead stops" {
				model.Await("sub-ask", "scratch_read", "scratch_read other/shared", nil)
				model.Stop()
			} else {
				model.Stop()
				model.Await("sub-ask", "scratch_read", "scratch_read other/shared", nil)
			}
			if model.AskedID() != "sub-ask" || !model.TakesAnswerDigits() {
				t.Fatalf("the idle lead holds ask %q, takes digits %v", model.AskedID(), model.TakesAnswerDigits())
			}
			if frame := ansi.Strip(model.View()); !strings.Contains(frame, "[1] allow once") {
				t.Fatalf("the idle frame does not offer the answers\n%s", frame)
			}
			model.Start()
			model.Stop()
			model.Resume("sub-ask")
			if model.AskedID() != "" || model.Awaiting() {
				t.Fatalf("the answered ask is still open: %q", model.AskedID())
			}
		})
	}
}
