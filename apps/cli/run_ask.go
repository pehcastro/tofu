package main

import (
	"time"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/tools/human/ask"
	shipped "tofu/library"
	"tofu/library/questions"
)

const askPoint = questions.AskPin

func askPersonTool(dir, log string, person loop.Person, inbox *loop.Inbox, keys core.Keyring, say func(string)) ask.AskPerson {
	tool := ask.AskPerson{Person: person, Inbox: inbox, Wait: time.Duration(settingspkg.IntSaying(dir, settingspkg.AskPersonWaitSeconds, say)) * time.Second,
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

func askJudge(dir, log string, keys core.Keyring) (subagents.AskJudge, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return subagents.AskJudge{}, err
	}
	set, _, err := question.Resolve(askPoint, layers)
	if err != nil {
		return subagents.AskJudge{}, err
	}
	rule, err := subagents.LoadAskRule(shipped.Files(), askPoint)
	if err != nil {
		return subagents.AskJudge{}, err
	}
	client, err := wiring.JevClientFrom(keys, wiring.OneCallAtATime)
	if err != nil {
		return subagents.AskJudge{}, err
	}
	return subagents.AskJudge{Client: client, Set: set, Rule: rule, Ledger: ledger.NewWriter(log)}, nil
}
