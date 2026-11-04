package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
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
	HookTicketEvidence HookEvent = "ticket.evidence"
	HookTicketComment  HookEvent = "ticket.comment"
	HookTicketSet      HookEvent = "ticket.set"
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
		HookTicketEvidence, HookTicketComment, HookTicketSet, HookEpicCreate, HookSprintOpen, HookSprintClose, HookMilestoneReach, HookTriageRequest, HookTriageAccept,
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
	DoCommand  Do = "command"
	DoJev      Do = "jev"
	DoField    Do = "field"
	DoPerson   Do = "person"
	DoContract Do = "contract"
	DoCheck    Do = "check"
	DoCreate   Do = "create"
	DoMove     Do = "move"
	DoAssign   Do = "assign"
	DoLog      Do = "log"
	DoNotify   Do = "notify"
	DoSet      Do = "set"
)

func (d Do) Check() bool {
	return slices.Contains([]Do{DoCommand, DoJev, DoField, DoPerson, DoContract, DoCheck}, d)
}

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

func hookActors(key string, values []string, at string) ([]HookActor, error) {
	var actors []HookActor
	for _, one := range values {
		if !slices.Contains([]HookActor{ActorPerson, ActorLead, ActorSubAgent, ActorHook}, HookActor(one)) {
			return nil, fmt.Errorf("%s: %s %q is %s, %s, %s or %s", at, key, one, ActorPerson, ActorLead, ActorSubAgent, ActorHook)
		}
		actors = append(actors, HookActor(one))
	}
	return actors, nil
}

type Accept string

const (
	AcceptPerson  Accept = "person"
	AcceptManager Accept = "manager"
	AcceptLead    Accept = "lead"
	AcceptJev     Accept = "jev"
	AcceptNone    Accept = "none"
)

type Wait string

const (
	WaitBlock Wait = "block"
	WaitAfter Wait = "after"
)

type Bypass struct {
	Who    []HookActor
	Accept Accept
	Wait   Wait
}

func (b *Bypass) set(key, value, at string) error {
	var err error
	switch key {
	case "who":
		b.Who, err = hookActors(key, commaList(value), at)
	case "accept":
		b.Accept = Accept(value)
		if !slices.Contains([]Accept{AcceptPerson, AcceptManager, AcceptLead, AcceptJev, AcceptNone}, b.Accept) {
			err = fmt.Errorf("%s: accept %q is %s, %s, %s, %s or %s", at, value, AcceptPerson, AcceptManager, AcceptLead, AcceptJev, AcceptNone)
		}
	case "wait":
		b.Wait = Wait(value)
		if b.Wait != WaitBlock && b.Wait != WaitAfter {
			err = fmt.Errorf("%s: wait %q is %s or %s", at, value, WaitBlock, WaitAfter)
		}
	default:
		err = fmt.Errorf("%s: %q is not a bypass field, and a bypass names who, accept and wait", at, key)
	}
	return err
}

type Conditions struct {
	From, To, Type, Priority, Labels, NotLabels []string
	Actor                                       []HookActor
	Field                                       []string
	Epic, Sprint, Owns                          string
	InStatusFor                                 time.Duration
	CreatedBefore, CreatedAfter                 time.Time
}

type Action struct {
	Do  Do
	Arg string
}

func (a Action) Setting() (field, value, asks string) {
	field, value, _ = strings.Cut(a.Arg, " ")
	value = strings.TrimSpace(value)
	if set, fromJev := strings.CutPrefix(value, string(DoJev)+" "); fromJev {
		return field, "", strings.TrimSpace(set)
	}
	return field, value, ""
}

type Hook struct {
	ID, File, Text    string
	Name, Description string
	Bypass            *Bypass
	When              When
	On                HookEvent
	Also              []HookEvent
	Every             time.Duration
	Daily             string
	If                Conditions
	Action            Action
	Then              Then
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
		h, on, loadErr := loadHook(filepath.Join(dir, entry.Name()))
		if on && loadErr == nil {
			hooks = append(hooks, h)
		}
		err = errors.Join(err, loadErr)
	}
	return hooks, err
}

func loadHook(file string) (Hook, bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Hook{}, false, err
	}
	if declared(data, "when") != "" {
		h, err := ParseHook(data, file)
		return h, true, err
	}
	r, err := parseFile(data, file)
	if err != nil || r.Mode == ModeOff {
		return Hook{}, false, err
	}
	h, err := r.boardHook()
	return h, true, err
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

func ParseHook(data []byte, file string) (Hook, error) {
	h := Hook{File: file}
	topIndent, block := -1, ""
	err := scanIndented(data, file, func(indent int, key, value string, line int) error {
		at := fmt.Sprintf("%s:%d", file, line)
		if topIndent < 0 || indent <= topIndent {
			topIndent, block = indent, ""
		}
		switch block {
		case "bypass":
			return h.Bypass.set(key, value, at)
		case "if":
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
			block = key
		case "bypass":
			if value != "" {
				return fmt.Errorf("%s: bypass takes the block form, its fields indented on the lines below it: bypass:, then who: lead, sub-agent, then accept: manager, then wait: block", at)
			}
			h.Bypass, block = &Bypass{Wait: WaitBlock}, key
		case "id":
			h.ID = value
		case "name":
			h.Name = value
		case "description":
			h.Description = value
		case "when":
			h.When = When(value)
		case "on":
			events := commaList(value)
			h.On = HookEvent(value)
			if len(events) > 1 {
				h.On = HookEvent(events[0])
				for _, event := range events[1:] {
					h.Also = append(h.Also, HookEvent(event))
				}
			}
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

func commaList(value string) []string {
	var list []string
	for one := range strings.SplitSeq(value, ",") {
		if one = strings.TrimSpace(one); one != "" {
			list = append(list, one)
		}
	}
	return list
}

func (c *Conditions) set(key, value, at string) (bool, error) {
	list := commaList(value)
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
	case "not_labels":
		c.NotLabels = list
	case "field":
		c.Field = list
	case "epic":
		c.Epic, err = value, planValue(value)
	case "sprint":
		c.Sprint, err = value, planValue(value)
	case "owns":
		c.Owns = value
		_, err = subagents.Matches("a/path/the/owns/condition/is/tested/against", []string{value})
	case "in_status_for":
		c.InStatusFor, err = time.ParseDuration(value)
	case "created_before":
		c.CreatedBefore, err = moment(value)
	case "created_after":
		c.CreatedAfter, err = moment(value)
	case "actor":
		c.Actor, err = hookActors(key, list, at)
		return true, err
	default:
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("%s: %s: %w", at, key, err)
	}
	return true, nil
}

const (
	planSet   = "set"
	planEmpty = "empty"
)

func planValue(value string) error {
	if value == planSet || value == planEmpty {
		return nil
	}
	misspelt := slices.ContainsFunc([]string{planSet, planEmpty}, func(word string) bool { return edits(value, word) <= len(word)/2 })
	plainID := value != "" && unicode.IsLetter(rune(value[0])) && !strings.ContainsFunc(value, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	})
	if misspelt || !plainID {
		return fmt.Errorf("%q is %s, %s or an id of letters, digits and dashes that starts with a letter", value, planSet, planEmpty)
	}
	return nil
}

func edits(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := range len(a) {
		diagonal := row[0]
		row[0] = i + 1
		for j := range len(b) {
			cost := 1
			if a[i] == b[j] {
				cost = 0
			}
			diagonal, row[j+1] = row[j+1], min(row[j+1]+1, row[j]+1, diagonal+cost)
		}
	}
	return row[len(b)]
}

func moment(value string) (time.Time, error) {
	at, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Parse(time.RFC3339, value)
	}
	return at, nil
}

func (h Hook) valid() error {
	do := h.Action.Do
	var why string
	switch {
	case h.ID == "":
		why = "declares no id"
	case h.When != WhenBefore && h.When != WhenAfter:
		why = fmt.Sprintf("declares when %q, and when is %s or %s", h.When, WhenBefore, WhenAfter)
	case slices.ContainsFunc(h.Events(), func(event HookEvent) bool { return !slices.Contains(HookEvents(), event) }):
		why = fmt.Sprintf("declares on %v, and an event is one of %v", h.Events(), HookEvents())
	case h.Also != nil && h.Fires(HookSchedule):
		why = "is on schedule beside another event, and a schedule hook is on schedule alone"
	case (h.On == HookSchedule) != (h.Every > 0 || h.Daily != ""):
		why = "declares every or daily exactly when it is on schedule"
	case h.Every > 0 && h.Daily != "":
		why = "declares both every and daily"
	case h.On == HookSchedule && h.When != WhenAfter:
		why = "is on schedule, and a schedule runs after, so its when is after"
	case h.Every > 0 && h.Every < konst.CronMinIntervalSeconds*time.Second:
		why = fmt.Sprintf("runs every %s, under the %ds minimum", h.Every, konst.CronMinIntervalSeconds)
	case !do.Check() && !do.effect():
		why = fmt.Sprintf("does %q, and do is a check (command, jev, field, person, contract, check) or an effect (create, move, assign, log, notify, set)", do)
	case do.Check() && !slices.Contains([]Then{ThenRefuse, ThenNudge, ThenAsk, ThenRecord}, h.Then):
		why = fmt.Sprintf("runs the check %s, and then says what its failure does: %s, %s, %s or %s", do, ThenRefuse, ThenNudge, ThenAsk, ThenRecord)
	case do.effect() && h.Then != "":
		why = fmt.Sprintf("runs the effect %s, and then belongs to a check", do)
	case h.When == WhenAfter && (h.Then == ThenRefuse || h.Then == ThenAsk):
		why = fmt.Sprintf("runs after and declares then %s, and an after hook cannot stop what already happened", h.Then)
	case h.When == WhenBefore && do.effect() && do != DoSet:
		why = fmt.Sprintf("runs before and does %s, and before a hook runs a check or a set", do)
	case (do == DoPerson || do == DoContract) != (h.Action.Arg == ""):
		why = fmt.Sprintf("does %s, and every do but person and contract names what it works on", do)
	case slices.ContainsFunc(h.Events(), func(event HookEvent) bool { return !event.ticketed() }) && slices.Contains([]Do{DoMove, DoAssign, DoLog, DoSet, DoContract}, do):
		why = fmt.Sprintf("is on %v, and one of them has no ticket to %s", h.Events(), do)
	case do == DoSet && len(strings.Fields(h.Action.Arg)) < 2:
		why = "does set, which names a field and its value"
	case h.Fires(HookTicketMove) && do == DoMove && (h.If.To == nil || slices.Contains(h.If.To, h.Action.Arg)):
		why = fmt.Sprintf("is on %s and does move %s, which fires it again: name the statuses it runs on in if: to, without %s", HookTicketMove, h.Action.Arg, h.Action.Arg)
	case !h.If.CreatedBefore.IsZero() && !h.If.CreatedAfter.IsZero() && !h.If.CreatedBefore.After(h.If.CreatedAfter):
		why = "declares created_before no later than created_after, so no ticket holds it"
	case h.Bypass == nil:
		return nil
	case h.Then != ThenRefuse && h.Then != ThenAsk:
		why = fmt.Sprintf("declares a bypass and then %q, and only a hook that refuses or asks can be bypassed", h.Then)
	case h.Bypass.Who == nil:
		why = fmt.Sprintf("declares a bypass with no who, and who names %s, %s, %s or %s", ActorPerson, ActorLead, ActorSubAgent, ActorHook)
	case h.Bypass.Accept == "":
		why = fmt.Sprintf("declares a bypass with no accept, and accept is %s, %s, %s, %s or %s", AcceptPerson, AcceptManager, AcceptLead, AcceptJev, AcceptNone)
	default:
		return nil
	}
	return fmt.Errorf("%s: hook %q %s", h.File, h.ID, why)
}

func (h Hook) Events() []HookEvent { return append([]HookEvent{h.On}, h.Also...) }

func (h Hook) Fires(event HookEvent) bool { return slices.Contains(h.Events(), event) }

func (h Hook) Due(last, now time.Time) bool {
	if h.Every > 0 {
		halfTick := konst.CronMinIntervalSeconds * time.Second / 2
		return now.Sub(last) >= h.Every-halfTick
	}
	clock, _ := time.Parse(dailyLayout, h.Daily)
	today := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	return !now.Before(today) && last.Before(today)
}
