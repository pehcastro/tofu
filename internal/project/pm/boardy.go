package pm

import (
	"tofu/internal/agent/loop"
	"tofu/internal/project/boardy"
	"tofu/internal/store/session"
)

type Boardy struct{ boardy.Managed }

func (b Boardy) As(actor string) Tracker {
	seeded := b.Store.Speakers[actor]
	if seeded.Handle != "" || b.Store.Root == "" {
		return b
	}
	sessions, err := session.OpenIn(b.Store.Root)
	if err != nil {
		return b
	}
	if lead, found := loop.PMAuthor(sessions, actor, seeded.Model); found {
		b.Store.Speakers = map[string]boardy.Author{actor: *lead}
	}
	return b
}

func (b Boardy) Start(key, name string) error {
	_, err := b.Store.InitBoard(key, name, boardy.InitDefault, nil)
	return err
}

func (b Boardy) InStatus(status boardy.Status) []boardy.Front {
	return loop.BoardTickets(b.Store, status)
}

func (b Boardy) Move(id string, to boardy.Status, reason, actor string, leave []int, bypass boardy.Bypassing) (boardy.Ticket, error) {
	b.Bypass = bypass
	return b.MoveLeaving(id, to, reason, actor, leave)
}

func (b Boardy) Boards() ([]boardy.Board, error) { return b.Store.Boards() }

func (b Boardy) EventSizes() map[string]int64 { return b.Store.EventSizes() }

func (b Boardy) NudgesSince(sizes map[string]int64) []string { return b.Store.NudgesSince(sizes) }

func (b Boardy) Epics(board string) ([]boardy.Epic, error) { return b.Store.Epics(board) }

func (b Boardy) SaveEpic(board string, epic boardy.Epic) error { return b.Store.SaveEpic(board, epic) }

func (b Boardy) Research(board, id, note string) (boardy.Epic, error) {
	return b.Store.Research(board, id, note)
}

func (b Boardy) Design(board, id, design, first string) (boardy.Epic, error) {
	return b.Store.Design(board, id, design, first)
}

func (b Boardy) Sprints(board string) ([]boardy.Sprint, error) { return b.Store.Sprints(board) }

func (b Boardy) SaveSprint(board string, sprint boardy.Sprint) error {
	return b.Store.SaveSprint(board, sprint)
}

func (b Boardy) SetSprint(board, id string, state boardy.SprintState) error {
	return b.Store.SetSprint(board, id, state)
}

func (b Boardy) Milestones(board string) ([]boardy.Milestone, error) {
	return b.Store.Milestones(board)
}

func (b Boardy) SaveMilestone(board string, milestone boardy.Milestone) error {
	return b.Store.SaveMilestone(board, milestone)
}

func (b Boardy) RequestStandings(board string) ([]boardy.RequestStanding, error) {
	return b.Store.RequestStandings(board)
}

func (b Boardy) RequestStanding(board, id string) (boardy.RequestStanding, error) {
	return b.Store.RequestStanding(board, id)
}

func (b Boardy) api(actor string) boardy.API { return boardy.API{Board: b.Managed, Actor: actor} }

func (b Boardy) Triage(request boardy.TriageRequest, actor string) (boardy.TriageAnswer, error) {
	return b.api(actor).Triage(request)
}

func (b Boardy) Rules(request boardy.RulesRequest) (boardy.RulesAnswer, error) {
	return b.api("").Rules(request)
}

func (b Boardy) RuleGet(request boardy.RuleRequest) (boardy.RuleAnswer, error) {
	return b.api("").RuleGet(request)
}

func (b Boardy) RulePut(request boardy.RulePutRequest, actor string) (boardy.RuleAnswer, error) {
	return b.api(actor).RulePut(request)
}

func (b Boardy) RuleRemove(request boardy.RuleRequest, actor string) (boardy.RuleAnswer, error) {
	return b.api(actor).RuleRemove(request)
}

func (b Boardy) RuleTest(request boardy.RuleTestRequest, actor string) (boardy.RuleTestAnswer, error) {
	return b.api(actor).RuleTest(request)
}
