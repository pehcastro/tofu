package host

import (
	"encoding/json"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/konst"
)

func boardyRuleRequests() []method {
	return []method{
		{name: boardyPrefix + "rules", params: boardy.RulesRequest{}, result: boardy.RulesAnswer{}},
		{name: boardyPrefix + "rule.get", params: boardy.RuleRequest{}, result: boardy.RuleAnswer{}},
		{name: boardyPrefix + "rule.put", params: boardy.RulePutRequest{}, result: boardy.RuleAnswer{}},
		{name: boardyPrefix + "rule.remove", params: boardy.RuleRequest{}, result: boardy.RuleAnswer{}},
		{name: boardyPrefix + "rule.test", params: boardy.RuleTestRequest{}, result: boardy.RuleTestAnswer{}},
	}
}

func boardyRule(api boardy.API, method string, raw json.RawMessage) (any, error) {
	switch method {
	case boardyPrefix + "rules":
		return handle(raw, answered(api.Rules))
	case boardyPrefix + "rule.get":
		return handle(raw, answered(api.RuleGet))
	case boardyPrefix + "rule.put":
		return handle(raw, answered(api.RulePut))
	case boardyPrefix + "rule.remove":
		return handle(raw, answered(api.RuleRemove))
	case boardyPrefix + "rule.test":
		return handle(raw, answered(api.RuleTest))
	}
	return nil, &Refusal{Code: CodeNoMethod, Message: "no method " + method}
}

func (s *server) tickBoardy(quit <-chan struct{}) {
	every := time.NewTicker(konst.CronMinIntervalSeconds * time.Second)
	defer every.Stop()
	said := map[string]string{}
	for {
		if api, err := s.boardyAPI(); err == nil {
			boards, _ := api.Board.Store.Boards()
			for _, board := range boards {
				_, err := api.Board.Tick(board.Key)
				text := ""
				if err != nil {
					text = err.Error()
				}
				if text != "" && text != said[board.Key] {
					_ = api.Board.Store.Record(board.Key, boardy.Event{Kind: boardy.HookFailed, Actor: "tofu serve", Text: text})
				}
				said[board.Key] = text
			}
		}
		select {
		case <-quit:
			return
		case <-every.C:
		}
	}
}
