package main

import (
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	settingspkg "tofu/internal/settings"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
	shipped "tofu/library"
	"tofu/library/questions"
)

const askPoint = "ask@1"

func askPersonTool(dir, log string, person turn.Person, inbox *turn.Inbox, say func(string)) tools.AskPerson {
	tool := tools.AskPerson{Person: person, Inbox: inbox, Wait: time.Duration(settingInt(dir, settingspkg.AskPersonWaitSeconds, say)) * time.Second,
		Auto: func() bool { return settingText(dir, settingspkg.GatePrompt, say) != settingspkg.GatePromptAsk }, Log: log}
	judge, err := askJudge(dir, log)
	if err != nil && say != nil {
		say("ask_person records no ask row in shadow: " + err.Error())
	}
	if err != nil {
		return tool
	}
	tool.Judge = &judge
	return tool
}

func askJudge(dir, log string) (subagent.AskJudge, error) {
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
	client, err := newJevClient(oneCallAtATime)
	if err != nil {
		return subagent.AskJudge{}, err
	}
	return subagent.AskJudge{Client: client, Set: set, Rule: rule, Ledger: ledger.NewWriter(log)}, nil
}
