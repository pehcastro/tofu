package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
)

func TestASpawnWritesHowLongEachStepBeforeItRanTookIntoTheLeadsTrace(t *testing.T) {
	root := t.TempDir()
	store := session.NewStore(filepath.Join(root, "sessions"))
	log, err := store.Open(session.Header{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	base := Config{Model: &stubModel{decisions: []llm.Decision{claimDecision("done")}}, Spend: SpendAPIKey, Tools: NewRegistry(),
		Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096, ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	ctx := context.WithValue(context.Background(), spawnSiteKey{}, spawnSite{log: log, turn: "turn-lead", agent: "turn-lead", call: "call-spawn"})
	if _, err := spawn.Run(ctx, json.RawMessage(`{"task":"say done","owns":["piece/**"]}`)); err != nil {
		t.Fatal(err)
	}
	reported(t, spawn)
	events, err := store.Events(log.ID())
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	for _, event := range events {
		if event.Kind == session.EventNotice && event.Agent == "turn-lead" && strings.Contains(string(event.Body), "spawn of sub-1") {
			notices = append(notices, string(event.Body))
		}
	}
	if len(notices) != 1 {
		t.Fatalf("the lead's trace holds %d spawn timing notices, want 1: %q", len(notices), notices)
	}
	for _, step := range []string{"memory ", "ticket grant ", "hook ", "prompt ", "account pick ", "earlier siblings ", "scratch ", "brief "} {
		if !strings.Contains(notices[0], step) {
			t.Errorf("the timing notice names no %q step: %s", strings.TrimSpace(step), notices[0])
		}
	}
}
