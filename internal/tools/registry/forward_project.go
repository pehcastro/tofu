package registry

import (
	"tofu/internal/project/boardy"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/project/report"
	"tofu/internal/tools/project/ticket"
)

type Project = report.Project

func NewProject(dir string) (Project, error) { return report.New(dir) }

func TicketTools(board boardy.Managed, actor string) ([]contract.Tool, error) {
	return ticket.Tools(board, actor)
}

func TicketGrant(board boardy.Managed, id, lead, agent string) ([]string, error) {
	return ticket.Grant(board, id, lead, agent)
}
