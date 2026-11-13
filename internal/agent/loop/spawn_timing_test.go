package loop

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
)

func TestASpawnWritesHowLongEachStepBeforeItRanTookIntoTheLeadsTrace(t *testing.T) {
	root := t.TempDir()
	store := session.NewStore(filepath.Join(root, "sessions"))
	log, err := store.Open(session.Header{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	base := Config{Model: &stubModel{decisions: []core.Decision{claimDecision("done")}}, Spend: SpendAPIKey, Tools: NewRegistry(),
		Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096, ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }}
	spawn := NewSpawnTool("turn-lead", base, &subagents.Roster{})
	ctx := context.WithValue(context.Background(), spawnSiteKey{}, spawnSite{log: log, turn: "turn-lead", agent: "turn-lead", call: "call-spawn"})
	if _, err := spawn.Run(ctx, json.RawMessage(`{"task":"say done","owns":["piece/**"]}`)); err != nil {
		t.Fatal(err)
	}
	reported(t, spawn)
	events, err := store.Events(log.ID())
	if err != nil {
		t.Fatal(err)
	}
	var spawns []session.WallEvent
	steps := map[string]bool{}
	for _, event := range events {
		if event.Kind == session.EventNotice && strings.Contains(string(event.Body), "spawn of sub-1") {
			t.Errorf("the spawn timing is still a notice: %s", event.Body)
		}
		wall, err := session.DecodeWall(event)
		switch {
		case err != nil || wall.Op != string(walls.OpSpawn) && wall.Op != string(walls.OpSpawnStep):
		case wall.Agent != "turn-lead" || wall.Call != "call-spawn" || wall.OutsideMS == nil:
			t.Errorf("a spawn wall is not the lead's spawn call with outside_ms: %+v", wall)
		case wall.Op == string(walls.OpSpawn):
			spawns = append(spawns, wall)
		case wall.Op == string(walls.OpSpawnStep):
			steps[wall.Name] = true
		}
	}
	if len(spawns) != 1 {
		t.Fatalf("the lead's trace holds %d spawn walls, want 1: %+v", len(spawns), spawns)
	}
	for _, step := range []string{"memory", "ticket grant", "hook", "prompt", "account pick", "earlier siblings", "scratch", "brief", "estimate"} {
		if !steps[step] {
			t.Errorf("no spawn_step wall names the %q step: %v", step, steps)
		}
	}
}
