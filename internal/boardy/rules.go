package boardy

import (
	"strings"

	"tofu/internal/rule"
)

const RulesDirName = "rules"

const (
	RuleFired       EventKind = "rule"
	Waiting         EventKind = "waiting"
	Requested       EventKind = "requested"
	Accepted        EventKind = "accepted"
	RequestDropped  EventKind = "request-dropped"
	Handed          EventKind = "handed"
	Widened         EventKind = "widened"
	ManagersChanged EventKind = "managers"
	Assigned        EventKind = "assigned"
)

type Fired struct {
	rule.Fire
	Checker string    `json:"checker"`
	Then    rule.Then `json:"then"`
}

func (f Fired) Text() string {
	details := make([]string, len(f.Findings))
	for i, finding := range f.Findings {
		details[i] = finding.Detail
	}
	return strings.Join(details, "; ")
}

func (f Fired) waits() bool { return f.Blocked && f.Then == rule.ThenAsk }
