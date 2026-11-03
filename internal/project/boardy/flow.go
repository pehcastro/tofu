package boardy

import "fmt"

type Flow string

const (
	Jira   Flow = "jira"
	Linear Flow = "linear"
)

const (
	FlowSetting       = "boardFlow"
	ManagementSetting = "projectManagement"
)

type Words struct {
	Ticket string `json:"ticket"`
	Board  string `json:"board"`
	Epic   string `json:"epic"`
	Sprint string `json:"sprint"`
	Points string `json:"points"`
}

func ParseFlow(raw string) (Flow, error) {
	switch flow := Flow(raw); flow {
	case "":
		return Jira, nil
	case Jira, Linear:
		return flow, nil
	}
	return "", fmt.Errorf("%s is %q, and only jira or linear are flows", FlowSetting, raw)
}

func (f Flow) Words() Words {
	if f == Linear {
		return Words{Ticket: "issue", Board: "team", Epic: "project", Sprint: "cycle", Points: "estimate"}
	}
	return Words{Ticket: "ticket", Board: "board", Epic: "epic", Sprint: "sprint", Points: "points"}
}
