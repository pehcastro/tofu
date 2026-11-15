package pm

import "tofu/internal/project/boardy"

type Tracker interface {
	As(actor string) Tracker
	Boards() ([]boardy.Board, error)
	Start(key, name string) error
	Manages(key, actor, doing string) error
	EventSizes() map[string]int64
	NudgesSince(sizes map[string]int64) []string

	Get(id string) (boardy.Ticket, error)
	InStatus(status boardy.Status) []boardy.Front
	Create(board string, ticket boardy.Ticket, actor string) (boardy.Ticket, error)
	Request(board string, ticket boardy.Ticket, actor string) (boardy.Ticket, error)
	Move(id string, to boardy.Status, reason, actor string, leave []int, bypass boardy.Bypassing) (boardy.Ticket, error)
	Assign(id, to, actor string) (boardy.Ticket, error)
	Comment(id, text, actor string) (boardy.Ticket, error)
	Evidence(id, text, actor string, line int) (boardy.Ticket, error)
	CaptureBefore(id, text, actor string) (boardy.Ticket, error)
	CaptureLateBefore(id, text, ranOn, actor string) (boardy.Ticket, error)
	Ask(id string, q boardy.Question, actor string) (boardy.Ticket, error)
	AskInLog(id, question, actor string) (boardy.Ticket, error)
	ReportNotPossible(id string, q boardy.Question, actor string) (boardy.Ticket, error)

	Plan(id, epic, sprint, actor string) (boardy.Ticket, error)
	Epics(board string) ([]boardy.Epic, error)
	SaveEpic(board string, epic boardy.Epic) error
	Research(board, id, note string) (boardy.Epic, error)
	Design(board, id, design, first string) (boardy.Epic, error)
	Sprints(board string) ([]boardy.Sprint, error)
	SaveSprint(board string, sprint boardy.Sprint) error
	SetSprint(board, id string, state boardy.SprintState) error
	Milestones(board string) ([]boardy.Milestone, error)
	SaveMilestone(board string, milestone boardy.Milestone) error
	Triage(request boardy.TriageRequest, actor string) (boardy.TriageAnswer, error)
	RequestStandings(board string) ([]boardy.RequestStanding, error)
	RequestStanding(board, id string) (boardy.RequestStanding, error)
	NewRequest(board, words string, tickets []string, actor string) (boardy.RequestStanding, error)
	AddToRequest(board, id string, tickets []string, actor string) (boardy.RequestStanding, error)
	ReviewRequest(board, id string, verdict boardy.ReviewVerdict, note, actor string) (boardy.RequestStanding, error)

	Rules(request boardy.RulesRequest) (boardy.RulesAnswer, error)
	RuleGet(request boardy.RuleRequest) (boardy.RuleAnswer, error)
	RulePut(request boardy.RulePutRequest, actor string) (boardy.RuleAnswer, error)
	RuleRemove(request boardy.RuleRequest, actor string) (boardy.RuleAnswer, error)
	RuleTest(request boardy.RuleTestRequest, actor string) (boardy.RuleTestAnswer, error)
}
