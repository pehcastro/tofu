package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/settings"
)

func TestTheSpawnAtTheSettingRunsAndTheOnePastItIsRefusedNamingIt(t *testing.T) {
	const perTurn = 15
	decisions := make([]llm.Decision, perTurn)
	for i := range decisions {
		decisions[i] = claimDecision("done")
	}
	_, spawn := orchestratorTurn(t, t.TempDir(), decisions)
	spawn.Limits = func() SubAgentLimits { return SubAgentLimits{PerTurn: perTurn, Depth: 2} }
	if described := spawn.Definition().Description; !strings.Contains(described, "At most 15 sub-agents per turn") || !strings.Contains(described, settings.SubAgentsPerTurn) {
		t.Errorf("the model is not told the setting's number and name: %q", described)
	}
	spawnNumber := func(n int) error {
		args, err := json.Marshal(spawnArgs{Task: "piece " + strconv.Itoa(n), Owns: []string{"piece" + strconv.Itoa(n) + "/**"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = spawn.Run(context.Background(), args)
		return err
	}
	for n := 1; n <= perTurn; n++ {
		if err := spawnNumber(n); err != nil {
			t.Fatalf("spawn %d of %d was refused: %v", n, perTurn, err)
		}
	}
	refused := spawnNumber(perTurn + 1)
	if refused == nil || !strings.Contains(refused.Error(), settings.SubAgentsPerTurn) || !strings.Contains(refused.Error(), "15") {
		t.Fatalf("spawn 16 with the setting at 15 was not refused naming the setting: %v", refused)
	}
}
