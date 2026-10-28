package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/sys"
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
	place, err := placeOf(s.Dir)
	if err != nil {
		return
	}
	ticker := strings.TrimSuffix(place.lock, ".lock") + ".hooks.lock"
	if os.MkdirAll(filepath.Dir(ticker), 0o700) != nil {
		return
	}
	var held *os.File
	defer func() {
		if held != nil {
			_ = sys.Unlock(held)
		}
	}()
	every := time.NewTicker(konst.CronMinIntervalSeconds * time.Second)
	defer every.Stop()
	said := map[string]string{}
	for {
		if held == nil {
			held, _, _ = sys.TryLock(ticker)
		}
		if api, err := s.boardyAPI(); held != nil && err == nil {
			boards, _ := api.Board.Store.Boards()
			for _, board := range boards {
				ran, err := api.Board.Tick(board.Key)
				if len(ran) == 0 && err == nil {
					continue
				}
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
