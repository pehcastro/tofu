package host

import (
	"encoding/json"
	"path/filepath"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	boardyPrefix = "boardy."
	boardyPerson = "person"
)

func boardyRequests() []method {
	return []method{
		{name: boardyPrefix + "boards", params: NoParams{}, result: boardy.BoardsAnswer{}},
		{name: boardyPrefix + "list", params: boardy.ListRequest{}, result: boardy.ListAnswer{}},
		{name: boardyPrefix + "get", params: boardy.GetRequest{}, result: boardy.GetAnswer{}},
		{name: boardyPrefix + "events", params: boardy.EventsRequest{}, result: boardy.EventsAnswer{}},
		{name: boardyPrefix + "report", params: boardy.ReportRequest{}, result: boardy.ReportAnswer{}},
		{name: boardyPrefix + "create", params: boardy.CreateRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "move", params: boardy.MoveRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "assign", params: boardy.AssignRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "log", params: boardy.LogRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "hand", params: boardy.HandRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "triage", params: boardy.TriageRequest{}, result: boardy.TriageAnswer{}},
	}
}

func (s *server) boardyAPI() (boardy.API, error) {
	store, err := boardy.OpenStore(s.Dir)
	if err != nil {
		return boardy.API{}, err
	}
	flow := ""
	home, _ := sys.HomeConfigDir()
	if values, openErr := settings.Open(filepath.Join(home, settings.FileName), filepath.Join(sys.StateDir(s.Dir), settings.FileName)); openErr == nil {
		flow = values.Text(boardy.FlowSetting)
	}
	parsed, err := boardy.ParseFlow(flow)
	return boardy.API{Board: boardy.Managed{Local: boardy.Local{Store: store}, Person: boardyPerson}, Actor: boardyPerson, Words: parsed.Words()}, err
}

func answered[P, A any](run func(P) (A, error)) func(P) (any, error) {
	return func(params P) (any, error) { return run(params) }
}

func (s *server) boardy(method string, raw json.RawMessage) (any, error) {
	api, err := s.boardyAPI()
	if err != nil {
		return nil, err
	}
	switch method {
	case boardyPrefix + "boards":
		return handle(raw, func(NoParams) (any, error) { return api.Boards() })
	case boardyPrefix + "list":
		return handle(raw, answered(api.List))
	case boardyPrefix + "get":
		return handle(raw, answered(api.Get))
	case boardyPrefix + "events":
		return handle(raw, answered(api.Events))
	case boardyPrefix + "report":
		return handle(raw, answered(api.Report))
	case boardyPrefix + "create":
		return handle(raw, answered(api.Create))
	case boardyPrefix + "move":
		return handle(raw, answered(api.Move))
	case boardyPrefix + "assign":
		return handle(raw, answered(api.Assign))
	case boardyPrefix + "log":
		return handle(raw, answered(api.Log))
	case boardyPrefix + "hand":
		return handle(raw, answered(api.Hand))
	case boardyPrefix + "triage":
		return handle(raw, answered(api.Triage))
	}
	return nil, &Refusal{Code: CodeNoMethod, Message: "no method " + method}
}

func (s *server) watchBoardy(quit <-chan struct{}) {
	store, err := boardy.OpenStore(s.Dir)
	if err != nil {
		return
	}
	boardy.NewWatcher(store.Dir).Watch(quit, konst.ServeShellPollMillis*time.Millisecond, func(change boardy.Changed) {
		s.box.push(notify(boardyPrefix+"changed", change))
	})
}
