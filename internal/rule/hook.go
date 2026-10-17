package rule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/subagent"
)

type When string

const (
	WhenBefore When = "before"
	WhenAfter  When = "after"
)

type HookEvent string

const (
	HookTicketCreate   HookEvent = "ticket.create"
	HookTicketMove     HookEvent = "ticket.move"
	HookTicketAssign   HookEvent = "ticket.assign"
	HookTicketLog      HookEvent = "ticket.log"
	HookTicketWiden    HookEvent = "ticket.widen"
	HookTicketClose    HookEvent = "ticket.close"
	HookEpicCreate     HookEvent = "epic.create"
	HookSprintOpen     HookEvent = "sprint.open"
	HookSprintClose    HookEvent = "sprint.close"
	HookMilestoneReach HookEvent = "milestone.reach"
	HookTriageRequest  HookEvent = "triage.request"
	HookTriageAccept   HookEvent = "triage.accept"
	HookQuestionAsk    HookEvent = "question.ask"
	HookQuestionAnswer HookEvent = "question.answer"
	HookAgentSpawn     HookEvent = "agent.spawn"
	HookAgentReport    HookEvent = "agent.report"
	HookLeadAttach     HookEvent = "lead.attach"
	HookSchedule       HookEvent = "schedule"
)

func HookEvents() []HookEvent {
	return []HookEvent{HookTicketCreate, HookTicketMove, HookTicketAssign, HookTicketLog, HookTicketWiden, HookTicketClose,
		HookEpicCreate, HookSprintOpen, HookSprintClose, HookMilestoneReach, HookTriageRequest, HookTriageAccept,
		HookQuestionAsk, HookQuestionAnswer, HookAgentSpawn, HookAgentReport, HookLeadAttach, HookSchedule}
}

func (e HookEvent) ticketed() bool {
	switch e {
	case HookEpicCreate, HookSprintOpen, HookSprintClose, HookMilestoneReach, HookTriageRequest, HookLeadAttach:
		return false
	}
	return true
}

func (e Event) Hook() HookEvent { return HookEvent("ticket." + string(e)) }

type Then string

const (
	ThenRefuse Then = "refuse"
	ThenNudge  Then = "nudge"
	ThenAsk    Then = "ask"
	ThenRecord Then = "record"
)

type Do string

const (
	DoCommand Do = "command"
	DoJev     Do = "jev"
	DoField   Do = "field"
	DoPerson  Do = "person"
	DoCreate  Do = "create"
	DoMove    Do = "move"
	DoAssign  Do = "assign"
	DoLog     Do = "log"
	DoNotify  Do = "notify"
	DoSet     Do = "set"
)

func (d Do) Check() bool { return slices.Contains([]Do{DoCommand, DoJev, DoField, DoPerson}, d) }

func (d Do) effect() bool {
	return slices.Contains([]Do{DoCreate, DoMove, DoAssign, DoLog, DoNotify, DoSet}, d)
}

type HookActor string

const (
	ActorPerson   HookActor = "person"
	ActorLead     HookActor = "lead"
	ActorSubAgent HookActor = "sub-agent"
	ActorHook     HookActor = "hook"
)

type Conditions struct {
	From, To, Type, Priority, Labels []string
	Actor                            []HookActor
	Epic, Sprint, Owns               string
	InStatusFor                      time.Duration
}

type Action struct {
	Do  Do
	Arg string
}

type Hook struct {
	ID, File, Text string
	When           When
	On             HookEvent
	Every          time.Duration
	Daily          string
	If             Conditions
	Action         Action
	Then           Then
}

const dailyLayout = "15:04"

func LoadHooks(dir string) ([]Hook, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var hooks []Hook
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			return nil, readErr
		}
		if declared(data, "when") != "" {
			h, parseErr := parseHook(data, file)
			if parseErr != nil {
				return nil, parseErr
			}
			hooks = append(hooks, h)
			continue
		}
		r, parseErr := parseFile(data, file)
		if parseErr != nil {
			return nil, parseErr
		}
		if r.Mode == ModeOff {
			continue
		}
		h, convertErr := r.boardHook()
		if convertErr != nil {
			return nil, convertErr
		}
		hooks = append(hooks, h)
	}
	return hooks, err
}

func (r Rule) boardHook() (Hook, error) {
	if r.Trigger.event == EventNone || (r.Checker != CheckerCommand && r.Checker != CheckerPersonApproves) {
		return Hook{}, fmt.Errorf("%s: a board rule declares on: and a board checker, %s or %s, and a hook declares when", r.File, CheckerCommand, CheckerPersonApproves)
	}
	h := Hook{ID: r.ID, File: r.File, Text: r.Text, When: WhenBefore, On: r.Trigger.event.Hook(), Action: Action{Do: DoCommand, Arg: r.Command}, Then: ThenRefuse}
	if r.Trigger.to != "" {
		h.If.To = []string{r.Trigger.to}
	}
	if r.Checker == CheckerPersonApproves {
		h.Action, h.Then = Action{Do: DoPerson}, ThenAsk
	}
	if r.Mode != ModeEnforced {
		h.Then = ThenRecord
	}
	return h, nil
}

func parseHook(data []byte, file string) (Hook, error) {
	h := Hook{File: file}
	underIf := false
	err := scanKV(data, file, func(key, value string, line int) error {
		at := fmt.Sprintf("%s:%d", file, line)
		if underIf {
			if known, err := h.If.set(key, value, at); known {
				return err
			}
		}
		var err error
		switch key {
		case "if":
			if value != "" {
				return fmt.Errorf("%s: if: takes its conditions on the lines below it", at)
			}
			underIf = true
		case "id":
			h.ID = value
		case "when":
			h.When = When(value)
		case "on":
			h.On = HookEvent(value)
		case "every":
			h.Every, err = time.ParseDuration(value)
		case "daily":
			h.Daily = value
			_, err = time.Parse(dailyLayout, value)
		case "do":
			verb, arg, _ := strings.Cut(value, " ")
			h.Action = Action{Do: Do(verb), Arg: strings.TrimSpace(arg)}
		case "then":
			h.Then = Then(value)
		case "text":
			h.Text = value
		default:
			return fmt.Errorf("%s: %q is not a hook field, and a condition goes on the lines under if", at, key)
		}
		if err != nil {
			return fmt.Errorf("%s: %s: %w", at, key, err)
		}
		return nil
	})
	if err != nil {
		return Hook{}, err
	}
	return h, h.valid()
}

func (c *Conditions) set(key, value, at string) (bool, error) {
	var list []string
	for one := range strings.SplitSeq(value, ",") {
		if one = strings.TrimSpace(one); one != "" {
			list = append(list, one)
		}
	}
	var err error
	switch key {
	case "from":
		c.From = list
	case "to":
		c.To = list
	case "type":
		c.Type = list
	case "priority":
		c.Priority = list
	case "labels":
		c.Labels = list
	case "epic":
		c.Epic = value
	case "sprint":
		c.Sprint = value
	case "owns":
		c.Owns = value
		_, err = subagent.Matches("a/path/the/owns/condition/is/tested/against", []string{value})
	case "in_status_for":
		c.InStatusFor, err = time.ParseDuration(value)
	case "actor":
		for _, one := range list {
			if !slices.Contains([]HookActor{ActorPerson, ActorLead, ActorSubAgent, ActorHook}, HookActor(one)) {
				return true, fmt.Errorf("%s: actor %q is %s, %s, %s or %s", at, one, ActorPerson, ActorLead, ActorSubAgent, ActorHook)
			}
			c.Actor = append(c.Actor, HookActor(one))
		}
	default:
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("%s: %s: %w", at, key, err)
	}
	return true, nil
}

func (h Hook) valid() error {
	do := h.Action.Do
	var why string
	switch {
	case h.ID == "":
		why = "declares no id"
	case h.When != WhenBefore && h.When != WhenAfter:
		why = fmt.Sprintf("declares when %q, and when is %s or %s", h.When, WhenBefore, WhenAfter)
	case !slices.Contains(HookEvents(), h.On):
		why = fmt.Sprintf("declares on %q, and an event is one of %v", h.On, HookEvents())
	case (h.On == HookSchedule) != (h.Every > 0 || h.Daily != ""):
		why = "declares every or daily exactly when it is on schedule"
	case h.Every > 0 && h.Daily != "":
		why = "declares both every and daily"
	case h.On == HookSchedule && h.When != WhenAfter:
		why = "is on schedule, and a schedule runs after, so its when is after"
	case h.Every > 0 && h.Every < konst.CronMinIntervalSeconds*time.Second:
		why = fmt.Sprintf("runs every %s, under the %ds minimum", h.Every, konst.CronMinIntervalSeconds)
	case !do.Check() && !do.effect():
		why = fmt.Sprintf("does %q, and do is a check (command, jev, field, person) or an effect (create, move, assign, log, notify, set)", do)
	case do.Check() && !slices.Contains([]Then{ThenRefuse, ThenNudge, ThenAsk, ThenRecord}, h.Then):
		why = fmt.Sprintf("runs the check %s, and then says what its failure does: %s, %s, %s or %s", do, ThenRefuse, ThenNudge, ThenAsk, ThenRecord)
	case do.effect() && h.Then != "":
		why = fmt.Sprintf("runs the effect %s, and then belongs to a check", do)
	case h.When == WhenAfter && (h.Then == ThenRefuse || h.Then == ThenAsk):
		why = fmt.Sprintf("runs after and declares then %s, and an after hook cannot stop what already happened", h.Then)
	case h.When == WhenBefore && do.effect() && do != DoSet:
		why = fmt.Sprintf("runs before and does %s, and before a hook runs a check or a set", do)
	case (do == DoPerson) != (h.Action.Arg == ""):
		why = fmt.Sprintf("does %s, and every do but person names what it works on", do)
	case !h.On.ticketed() && slices.Contains([]Do{DoMove, DoAssign, DoLog, DoSet}, do):
		why = fmt.Sprintf("is on %s, which has no ticket to %s", h.On, do)
	case do == DoSet && len(strings.Fields(h.Action.Arg)) < 2:
		why = "does set, which names a field and its value"
	default:
		return nil
	}
	return fmt.Errorf("%s: hook %q %s", h.File, h.ID, why)
}

func (h Hook) Due(last, now time.Time) bool {
	if h.Every > 0 {
		return now.Sub(last) >= h.Every
	}
	clock, _ := time.Parse(dailyLayout, h.Daily)
	today := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	return !now.Before(today) && last.Before(today)
}
