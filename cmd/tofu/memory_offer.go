package main

import (
	"context"
	"encoding/json"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	settingspkg "tofu/internal/settings"
)

const memoryOfferSet = state.MemoryOfferPoint + "@1"

func memoryOn(dir string) bool {
	on, _ := appSetting(dir, settingspkg.Memory)
	return on != 0
}

func autoMemoryOn(dir string) bool {
	on, _ := appSetting(dir, settingspkg.AutoMemory)
	return on != 0
}

func trustMemory() error {
	store, err := openSettings(".")
	if err != nil {
		return err
	}
	return store.Set(settingspkg.Global, settingspkg.AutoMemory, 1)
}

func askMemoryOffer(ctx context.Context, typed string) (ledger.Row, error) {
	built, builder, err := state.BuildMemoryOffer(state.MemoryOfferState{Message: typed})
	if err != nil {
		return ledger.Row{}, err
	}
	set, err := resolveLibrary(memoryOfferSet, "")
	if err != nil {
		return ledger.Row{}, err
	}
	client, err := newJevClient(oneCallAtATime)
	if err != nil {
		return ledger.Row{}, err
	}
	asked := json.RawMessage(built)
	decision, err := client.Ask(ctx, jev.Request{State: asked, Questions: set.Questions})
	if err != nil {
		return ledger.Row{}, err
	}
	return appendRow(asked, set, rowInput{decision: &decision, answers: toLedgerAnswers(set.QuestionsVersion, decision.Answers), stateBuilder: builder})
}
