package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/agent/loop"
	"tofu/internal/judge/ledger"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/settings"
	settingstool "tofu/internal/tools/human/settings"
)

type scriptedModel struct {
	calls []core.ToolCall
	step  int
}

func (m *scriptedModel) Ask(context.Context, core.Request) (core.Decision, error) {
	if m.step >= len(m.calls) {
		return core.Decision{Build: "scripted", Outcome: core.OutcomeMessage, Content: "done"}, nil
	}
	call := m.calls[m.step]
	m.step++
	return core.Decision{Build: "scripted", Outcome: core.OutcomeToolCalls, ToolCalls: []core.ToolCall{call}}, nil
}

type askingGate struct{}

func (askingGate) Decide(context.Context, loop.GateRequest) (loop.GateDecision, error) {
	return loop.GateDecision{ID: "dec-settings-1", Verdict: ledger.VerdictAsk}, nil
}

func TestTheSettingsToolChangesSubAgentsPerTurnOnlyAfterTheGateSaysYes(t *testing.T) {
	for _, answered := range []struct {
		person loop.PersonAnswer
		want   int
	}{{loop.PersonDenied, 10}, {loop.PersonAllowedOnce, 15}} {
		global := filepath.Join(t.TempDir(), settings.FileName)
		asked := 0
		_, err := loop.Run(context.Background(), loop.Config{
			Model: &scriptedModel{calls: []core.ToolCall{
				{ID: "c1", Name: "settings", Arguments: json.RawMessage(`{"key":"subAgentsPerTurn","value":15}`)},
			}},
			Spend:    loop.SpendSubscription,
			Tools:    loop.NewRegistry(settingstool.NewSettings(global, "")),
			Gate:     askingGate{},
			GateMode: loop.GateEnforce,
			Person: func(context.Context, loop.GateRequest, loop.GateDecision) (loop.PersonAnswer, error) {
				asked++
				return answered.person, nil
			},
			Task:           "raise the sub-agent limit to 15",
			ResultBytesCap: 4096,
			ArtifactDir:    t.TempDir(),
		})
		if err != nil {
			t.Fatalf("loop.Run: %v", err)
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
	_, err := settingstool.NewSettings(global, "").Run(context.Background(), json.RawMessage(`{"key":"decisionCap","value":15}`))
	if err == nil || !strings.Contains(err.Error(), settings.DecisionCap) {
		t.Errorf("a decisionCap change was not refused by name: %v", err)
	}
	if _, statErr := os.Stat(global); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a refused key still wrote the settings file: %v", statErr)
	}
}
