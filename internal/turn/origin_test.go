package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/subagent"
)

func TestEveryTextEnteringTheSessionSaysWhereItCameFrom(t *testing.T) {
	root := t.TempDir()
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatal(err)
	}
	bash.softLimit = 200 * time.Millisecond
	backup := llm.ToolCall{ID: "b", Name: "bash", Arguments: json.RawMessage(`{"command":"sleep 2 && echo backup done","timeout_ms":60000}`)}
	model := newCrew(map[string][]llm.Decision{
		leadKey: {toolCallDecision(spawnCall("s", usersRoute, "src/users.ts").ToolCalls[0], backup),
			claimDecision("both started"), claimDecision("read what came in"), claimDecision("read what came in"), claimDecision("read what came in")},
		usersRoute: {claimDecision("the users route is added")},
	})
	store, id := session.NewStore(t.TempDir()), session.NewEventID()
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(read, bash), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }, Sessions: store, Session: id}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "start the backup and hand the users route to a sub-agent", NewRegistry(read, bash, spawn), spawn.Inbox
	typed := make(chan string, 1)
	led := startLead(WithShellRegistry(context.Background(), shell.OpenAt(t.TempDir())), lead, typed)
	waitFor(t, "the lead's first turn to end", func() bool { return len(led.turns()) >= 1 })
	typed <- "and keep the old route too"
	select {
	case err := <-led.done:
		if err != nil {
			t.Fatalf("the lead ended in an error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the lead did not end within 15s")
	}

	events, err := store.Events(id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range events {
		var said struct {
			Role    string          `json:"role"`
			Content string          `json:"content"`
			From    *session.Origin `json:"from"`
		}
		if json.Unmarshal(event.Body, &said) != nil || event.Kind == session.EventMessage && said.Role != session.RoleUser {
			continue
		}
		switch event.Kind {
		case session.EventMessage, session.EventTurnStart, session.EventNotice:
			if said.From == nil || said.From.Kind == "" || said.From.Sub == "" {
				t.Errorf("seq %d, a %s of %q, carries no origin: %s", event.Seq, event.Kind, event.Agent, event.Body)
				continue
			}
		}
		if event.Kind == session.EventMessage && event.Agent == "" {
			seen[said.From.Kind+"/"+said.From.Sub] = true
			if said.From.Kind != string(llm.OriginPerson) && strings.Contains(said.Content, TheTaskFollows) {
				t.Errorf("seq %d, from %s/%s, is sent under the task heading: %q", event.Seq, said.From.Kind, said.From.Sub, said.Content)
			}
		}
	}
	for _, want := range []string{"person/task", "agent/report", "tofu/shell.exit", "person/typed"} {
		if !seen[want] {
			t.Errorf("the lead read nothing from %s; it read %v", want, seen)
		}
	}
}
