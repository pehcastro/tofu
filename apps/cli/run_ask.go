package main

import (
	"time"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/tools/registry"
	shipped "tofu/library"
	"tofu/library/questions"
)

const askPoint = questions.AskPin

func askPersonTool(dir, log string, person turn.Person, inbox *turn.Inbox, keys jev.Keyring, say func(string)) tools.AskPerson {
	tool := tools.AskPerson{Person: person, Inbox: inbox, Wait: time.Duration(settingspkg.IntSaying(dir, settingspkg.AskPersonWaitSeconds, say)) * time.Second,
		Auto: func() bool {
			return settingspkg.TextSaying(dir, settingspkg.GatePrompt, say) != settingspkg.GatePromptAsk
		}, Log: log}
	judge, err := askJudge(dir, log, keys)
	if err != nil && say != nil {
		say("ask_person records no ask row in shadow: " + err.Error())
	}
	if err != nil {
		return tool
	}
	tool.Judge = &judge
	return tool
}

func askJudge(dir, log string, keys jev.Keyring) (subagent.AskJudge, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return subagent.AskJudge{}, err
	}
	set, _, err := question.Resolve(askPoint, layers)
	if err != nil {
		return subagent.AskJudge{}, err
	}
	rule, err := subagent.LoadAskRule(shipped.Files(), askPoint)
	if err != nil {
		return subagent.AskJudge{}, err
	}
	client, err := wiring.JevClientFrom(keys, wiring.OneCallAtATime)
	if err != nil {
		return subagent.AskJudge{}, err
	}
	return subagent.AskJudge{Client: client, Set: set, Rule: rule, Ledger: ledger.NewWriter(log)}, nil
}
