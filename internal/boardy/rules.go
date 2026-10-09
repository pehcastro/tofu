package boardy

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/rule"
	"tofu/internal/sys"
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
	Checker string `json:"checker"`
}

func (f Fired) Text() string {
	details := make([]string, len(f.Findings))
	for i, finding := range f.Findings {
		details[i] = finding.Detail
	}
	return strings.Join(details, "; ")
}

func (s Store) BoardRules(key string) ([]rule.Rule, error) {
	dir := filepath.Join(s.BoardDir(key), RulesDirName)
	if found, err := sys.Exists(dir); !found || err != nil {
		return nil, err
	}
	rules, err := rule.LoadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if r.Trigger.On() == rule.EventNone || rule.Builtins()[r.Checker].Subject != rule.SubjectBoardEvent {
			return nil, fmt.Errorf("%s: a board rule declares on: and a board checker, %s or %s", r.File, rule.CheckerCommand, rule.CheckerPersonApproves)
		}
		if to := r.Trigger.To(); to != "" {
			if _, err := ParseStatus(to); err != nil {
				return nil, fmt.Errorf("%s: %w", r.File, err)
			}
		}
	}
	return rules, nil
}

func (s Store) Check(key string, e rule.BoardEvent) ([]Fired, error) {
	rules, err := s.BoardRules(key)
	if err != nil {
		return nil, err
	}
	var fired []Fired
	for _, r := range rules {
		if !r.Trigger.FiresOn(e) {
			continue
		}
		fire, err := rule.Run(r, rule.Builtins(), e, e.Ticket, time.Now())
		if err != nil {
			return fired, err
		}
		if len(fire.Findings) > 0 {
			fired = append(fired, Fired{Fire: fire, Checker: r.Checker})
		}
	}
	return fired, nil
}

func (m Managed) gate(key string, e rule.BoardEvent) error {
	fired, err := m.Store.Check(key, e)
	if err != nil {
		return err
	}
	var refusals []string
	var waits *WaitingError
	for _, f := range fired {
		kind := RuleFired
		switch {
		case !f.Blocked:
		case f.Checker == rule.CheckerPersonApproves:
			kind, waits = Waiting, &WaitingError{Ticket: e.Ticket, Rule: f.RuleID, Detail: f.Text()}
		default:
			refusals = append(refusals, f.RuleID+": "+f.Text())
		}
		err = errors.Join(err, m.Store.Record(key, Event{Ticket: e.Ticket, Kind: kind, Actor: e.Actor, To: Status(e.To),
			Text: fmt.Sprintf("%s (%s, %s): %s", f.RuleID, f.Mode, e.Event, f.Text())}))
	}
	switch {
	case err != nil:
		return err
	case refusals != nil:
		return RefusedError{Ticket: e.Ticket, Why: strings.Join(refusals, "; ")}
	case waits != nil:
		return *waits
	}
	return nil
}
