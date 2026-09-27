package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/settings"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type askingGate struct{}

func (askingGate) Decide(context.Context, turn.GateRequest) (turn.GateDecision, error) {
	return turn.GateDecision{ID: "dec-settings-1", Verdict: ledger.VerdictAsk}, nil
}

func TestTheSettingsToolChangesSubAgentsPerTurnOnlyAfterTheGateSaysYes(t *testing.T) {
	for _, answered := range []struct {
		person turn.PersonAnswer
		want   int
	}{{turn.PersonDenied, 10}, {turn.PersonAllowedOnce, 15}} {
		global := filepath.Join(t.TempDir(), settings.FileName)
		asked := 0
		_, err := turn.Run(context.Background(), turn.Config{
			Model: &scriptedModel{calls: []llm.ToolCall{
				{ID: "c1", Name: "settings", Arguments: json.RawMessage(`{"key":"subAgentsPerTurn","value":15}`)},
			}},
			Spend:    turn.SpendSubscription,
			Tools:    turn.NewRegistry(tools.NewSettings(global, "")),
			Gate:     askingGate{},
			GateMode: turn.GateEnforce,
			Person: func(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
				asked++
				return answered.person, nil
			},
			Task:           "raise the sub-agent limit to 15",
			ResultBytesCap: 4096,
			ArtifactDir:    t.TempDir(),
		})
		if err != nil {
			t.Fatalf("turn.Run: %v", err)
		}
		store, err := settings.Open(global, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := store.Int(settings.SubAgentsPerTurn); asked != 1 || got != answered.want {
			t.Errorf("person answered %v after %d asks, and the setting reads %d, want %d", answered.person, asked, got, answered.want)
		}
	}
}

func TestTheSettingsToolRefusesAnyOtherKey(t *testing.T) {
	global := filepath.Join(t.TempDir(), settings.FileName)
	_, err := tools.NewSettings(global, "").Run(context.Background(), json.RawMessage(`{"key":"decisionCap","value":15}`))
	if err == nil || !strings.Contains(err.Error(), settings.DecisionCap) {
		t.Errorf("a decisionCap change was not refused by name: %v", err)
	}
	if _, statErr := os.Stat(global); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a refused key still wrote the settings file: %v", statErr)
	}
}
