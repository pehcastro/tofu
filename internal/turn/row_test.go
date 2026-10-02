package turn

import (
	"context"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
)

func TestASubAgentWritingAfterTheLeadForksKeepsEveryEventInTheSessionLog(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey:    {spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is on it"), claimDecision("sub-1 is done")},
		usersRoute: {claimDecision("the users route is added")},
	})
	release := model.hold(usersRoute, 1)
	defer release()
	store, first := session.NewStore(t.TempDir()), session.NewEventID()
	lead := crewLead(t, model)
	lead.Sessions, lead.Session, lead.Budget = store, first, recall.Budget{Bands: recall.Bands{Recent: 1}}
	led := startLead(context.Background(), lead, nil)
	waitFor(t, "the lead's first turn to fork and end", func() bool { return len(led.turns()) == 1 })
	if forked := led.turns()[0]; forked.Session == first {
		t.Fatalf("the lead's first turn did not fork out of %s, so this proves nothing", first)
	}
	release()
	led.wait(t)

	events, err := store.Events(first)
	if err != nil {
		t.Fatal(err)
	}
	ended, said := false, false
	for _, event := range events {
		if event.Agent != "sub-1" {
			continue
		}
		ended = ended || event.Kind == session.EventAgentEnd
		said = said || event.Kind == session.EventMessage && strings.Contains(string(event.Body), "the users route is added")
	}
	if !ended || !said {
		t.Fatalf("the session sub-1 was spawned in kept its answer %v and its agent_end %v, want both, written after the lead forked", said, ended)
	}
}
