package host

import (
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	boardyPrefix      = "boardy."
	boardyPerson      = "person"
	hubAskBoardyHear  = "boardy.hear"
	hubAskBoardyWrite = "boardy.write"
	boardyPoll        = konst.ServeShellPollMillis * time.Millisecond
)

func boardyRequests() []method {
	return append([]method{
		{name: boardyPrefix + "boards", params: NoParams{}, result: boardy.BoardsAnswer{}},
		{name: boardyPrefix + "list", params: boardy.ListRequest{}, result: boardy.ListAnswer{}},
		{name: boardyPrefix + "rows", params: boardy.ListRequest{}, result: boardy.RowsAnswer{}},
		{name: boardyPrefix + "get", params: boardy.GetRequest{}, result: boardy.GetAnswer{}},
		{name: boardyPrefix + "events", params: boardy.EventsRequest{}, result: boardy.EventsAnswer{}},
		{name: boardyPrefix + "report", params: boardy.ReportRequest{}, result: boardy.ReportAnswer{}},
		{name: boardyPrefix + "audit", params: boardy.AuditRequest{}, result: boardy.AuditAnswer{}},
		{name: boardyPrefix + "create", params: boardy.CreateRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "move", params: boardy.MoveRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "assign", params: boardy.AssignRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "log", params: boardy.LogRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "hand", params: boardy.HandRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "triage", params: boardy.TriageRequest{}, result: boardy.TriageAnswer{}},
		{name: boardyPrefix + "questions", params: boardy.QuestionsRequest{}, result: boardy.QuestionsAnswer{}},
		{name: boardyPrefix + "answer", params: boardy.AnswerRequest{}, result: boardy.TicketAnswer{}},
		{name: boardyPrefix + "plan", params: boardy.PlanRequest{}, result: boardy.PlanAnswer{}},
		{name: boardyPrefix + "init", params: boardy.InitRequest{}, result: boardy.BoardsAnswer{}},
	}, boardyRuleRequests()...)
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
	case boardyPrefix + "rows":
		return handle(raw, answered(api.Rows))
	case boardyPrefix + "get":
		return handle(raw, answered(api.Get))
	case boardyPrefix + "events":
		return handle(raw, answered(api.Events))
	case boardyPrefix + "report":
		return handle(raw, answered(api.Report))
	case boardyPrefix + "audit":
		return handle(raw, answered(api.Audit))
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
	case boardyPrefix + "questions":
		return handle(raw, answered(api.Questions))
	case boardyPrefix + "answer":
		return handle(raw, answered(api.Answer))
	case boardyPrefix + "plan":
		return handle(raw, answered(api.Plan))
	case boardyPrefix + "init":
		return handle(raw, answered(api.Init))
	}
	return boardyRule(api, method, raw)
}

func (s *server) watchBoardy(quit <-chan struct{}) {
	store, err := boardy.OpenStore(s.Dir)
	if err != nil {
		return
	}
	told := make(chan boardy.Writing)
	safe.Go(s.report, "serve boardy told", func() { s.hearBoardy(quit, told) })
	boardy.NewWatcher(store.Dir).Watch(quit, boardyPoll, told, func(change boardy.Changed) {
		s.box.push(notify(boardyPrefix+"changed", change))
	})
}

func (s *server) hearBoardy(quit <-chan struct{}, told chan<- boardy.Writing) {
	place, err := placeOf(s.Dir)
	if err != nil {
		return
	}
	tick := time.NewTicker(boardyPoll)
	defer tick.Stop()
	for {
		if conn, err := dialHub(place.socket); err == nil {
			heard := make(chan struct{})
			safe.Go(s.report, "serve boardy hang up", func() {
				select {
				case <-quit:
				case <-heard:
				}
				_ = conn.Close()
			})
			if json.NewEncoder(conn).Encode(hubAsk{Ask: hubAskBoardyHear}) == nil {
				for lines := json.NewDecoder(conn); ; {
					var writing boardy.Writing
					if lines.Decode(&writing) != nil {
						break
					}
					select {
					case told <- writing:
					case <-quit:
					}
				}
			}
			close(heard)
			select {
			case told <- boardy.Writing{Done: true}:
			case <-quit:
			}
		}
		select {
		case <-quit:
			return
		case <-tick.C:
		}
	}
}

func BoardyWriting(project, by string) (done func()) {
	nothing := func() {}
	place, err := placeOf(project)
	if err != nil {
		return nothing
	}
	conn, err := dialHub(place.socket)
	if err != nil {
		return nothing
	}
	_ = conn.SetDeadline(time.Now().Add(hubAnswerWait))
	out := json.NewEncoder(conn)
	var status HubStatus
	if out.Encode(hubAsk{Ask: hubAskBoardyWrite}) != nil || out.Encode(boardy.Writing{By: by}) != nil || json.NewDecoder(conn).Decode(&status) != nil {
		_ = conn.Close()
		return nothing
	}
	return func() { _ = conn.Close() }
}

type boardyHearers struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (b *boardyHearers) serve(conn net.Conn, lines *json.Decoder, asked hubAsk, answer func(net.Conn) error) {
	_ = conn.SetDeadline(time.Time{})
	if asked.Ask == hubAskBoardyHear {
		b.mu.Lock()
		if b.conns == nil {
			b.conns = map[net.Conn]struct{}{}
		}
		b.conns[conn] = struct{}{}
		b.mu.Unlock()
		_, _ = io.Copy(io.Discard, conn)
		b.mu.Lock()
		delete(b.conns, conn)
		b.mu.Unlock()
		return
	}
	var writing boardy.Writing
	if lines.Decode(&writing) != nil {
		return
	}
	b.tell(writing)
	if answer(conn) == nil {
		_, _ = io.Copy(io.Discard, conn)
	}
	writing.Done = true
	b.tell(writing)
}

func (b *boardyHearers) tell(writing boardy.Writing) {
	line, err := json.Marshal(writing)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for conn := range b.conns {
		_ = conn.SetWriteDeadline(time.Now().Add(hubAnswerWait))
		_, _ = conn.Write(append(line, '\n'))
	}
}
