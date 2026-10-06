package main

import (
	"context"
	"time"

	"tofu/interface/tui"
	"tofu/internal/host"
	"tofu/internal/turn"
)

const cancelledAt = "cancelled at"

type appSession struct {
	*host.Host
	engine  *appEngine
	answers <-chan tui.Answer
	dir     string
}

func newAppSession(dir string, open func(runOpts) (appWire, error), answers <-chan tui.Answer, now func() time.Time, resumed sessionResume) *appSession {
	engine := &appEngine{dir: dir, open: open}
	live, _ := host.New(host.Config{Dir: dir, Engine: engine, Now: now, Check: cronChecker(dir), Resumed: resumed.hosted()})
	return &appSession{Host: live, engine: engine, answers: answers, dir: dir}
}

func (s *appSession) run(ctx context.Context, pick tui.Pick, task string, emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {
	if !s.Send(pick, task) {
		panic("a turn is already running on this host")
	}
	defer s.Close()
	defer context.AfterFunc(ctx, s.Stop)()
	for event := range s.Events() {
		switch event.Kind {
		case host.EventTurnStarted:
		case host.EventTurnEnded:
			return
		case host.EventAwaitPerson:
			emit(event)
			select {
			case answer := <-s.answers:
				s.Answer(answer)
			default:
				s.Answer(tui.Denied)
			}
		default:
			emit(event)
		}
	}
}

func (s *appSession) end() []string {
	return turn.EndSession(context.Background(), s.dir, s.ID(), sessionEndExit)
}

func (s *appSession) startFresh() string { return freshSession(s.Host) }

func (s *appSession) resume(handle string) (string, []tui.Event) { return appResume(s.Host, handle) }

func (s *appSession) compact() string { return compactSession(s.Host) }
