package boardy

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/sys"
)

const (
	hookActorPrefix   = "hook:"
	userBoardyDirName = "boardy"
	tickedFileName    = "hooks-ticked.json"
)

type subject struct {
	event    rule.HookEvent
	key      string
	ticket   Ticket
	path     string
	from, to Status
	actor    string
	depth    int
	entered  time.Time
}

func (m Managed) subjectOf(event rule.HookEvent, key string, ticket Ticket, to Status, actor string, depth int) subject {
	path, _ := m.Store.TicketPath(ticket.ID)
	return subject{event: event, key: key, ticket: ticket, path: path, from: ticket.Status, to: to, actor: actor, depth: depth}
}

func (s subject) expand(text string) string {
	return strings.NewReplacer("{ticket}", s.ticket.ID, "{title}", s.ticket.Title).Replace(text)
}

func (s Store) hooks(key string) ([]rule.Hook, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	var hooks []rule.Hook
	for _, dir := range []string{filepath.Join(s.BoardDir(key), RulesDirName), filepath.Join(s.Dir, RulesDirName), filepath.Join(home, userBoardyDirName, RulesDirName)} {
		scoped, err := rule.LoadHooks(dir)
		for _, h := range scoped {
			err = errors.Join(err, validHook(h))
		}
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, scoped...)
	}
	return hooks, nil
}

func validHook(h rule.Hook) error {
	var errs []error
	for _, status := range slices.Concat(h.If.From, h.If.To) {
		_, err := ParseStatus(status)
		errs = append(errs, err)
	}
	for _, kind := range h.If.Type {
		_, err := ParseKind(kind)
		errs = append(errs, err)
	}
	for _, priority := range h.If.Priority {
		_, err := ParsePriority(priority)
		errs = append(errs, err)
	}
	switch h.Action.Do {
	case rule.DoMove:
		_, err := ParseStatus(h.Action.Arg)
		errs = append(errs, err)
	case rule.DoField:
		if !slices.Contains(ticketFields(), h.Action.Arg) {
			errs = append(errs, fmt.Errorf("%q is not a ticket field: %s", h.Action.Arg, strings.Join(ticketFields(), ", ")))
		}
	case rule.DoSet:
		field, value, _ := strings.Cut(h.Action.Arg, " ")
		if settable := []string{"epic", "sprint", "assignee", "priority", "component", "spec", "size"}; !slices.Contains(settable, field) {
			errs = append(errs, fmt.Errorf("set names %q, and a hook sets one of %s", field, strings.Join(settable, ", ")))
		} else {
			errs = append(errs, new(Ticket).set(field, strings.TrimSpace(value)))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%s: hook %q: %w", h.File, h.ID, err)
	}
	return nil
}

func ticketFields() []string {
	var names []string
	for _, shape := range []reflect.Type{reflect.TypeFor[Front](), reflect.TypeFor[Ticket]()} {
		for i := range shape.NumField() {
			if name, _, _ := strings.Cut(shape.Field(i).Tag.Get("json"), ","); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func fieldSet(ticket Ticket, name string) bool {
	raw, _ := json.Marshal(ticket)
	var all map[string]any
	_ = json.Unmarshal(raw, &all)
	switch value := all[name].(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return slices.ContainsFunc(value, func(one any) bool { return one != "" })
	case float64:
		return value != 0
	}
	return false
}

func (m Managed) actorKind(actor string) rule.HookActor {
	switch {
	case strings.HasPrefix(actor, hookActorPrefix):
		return rule.ActorHook
	case strings.Contains(actor, subAgentOfSession):
		return rule.ActorSubAgent
	case m.actsForPerson(actor):
		return rule.ActorPerson
	}
	return rule.ActorLead
}

type personCheck struct{ ByPerson bool }

func (m Managed) boardEvent(_ rule.HookEvent, _ Ticket, _ Status, actor string) personCheck {
	return personCheck{ByPerson: m.actorKind(actor) == rule.ActorPerson}
}

func (m Managed) holds(h rule.Hook, s subject) bool {
	c, t := h.If, s.ticket
	among := func(want []string, have ...string) bool {
		return want == nil || slices.ContainsFunc(have, func(one string) bool { return slices.Contains(want, one) })
	}
	named := func(want, have string) bool {
		switch want {
		case "":
			return true
		case "set":
			return have != ""
		case "empty":
			return have == ""
		}
		return want == have
	}
	return among(c.From, string(s.from)) && among(c.To, string(s.to)) && among(c.Type, string(t.Type)) && among(c.Priority, string(t.Priority)) && among(c.Labels, t.Labels...) &&
		named(c.Epic, t.Epic) && named(c.Sprint, t.Sprint) &&
		(c.Owns == "" || slices.ContainsFunc(t.Owns, func(glob string) bool { return rule.ScopeReaches(c.Owns, glob) })) &&
		(c.Actor == nil || slices.Contains(c.Actor, m.actorKind(s.actor))) &&
		time.Since(s.entered) >= c.InStatusFor
}

func enteredStatus(events []Event) map[string]time.Time {
	at := map[string]time.Time{}
	for _, event := range events {
		if event.Ticket != "" && event.To != "" && (event.Kind == Created || event.Kind == Moved || event.Kind == Accepted) {
			at[event.Ticket] = event.At
		}
	}
	return at
}

func (m Managed) check(h rule.Hook, s subject) (string, bool, error) {
	switch h.Action.Do {
	case rule.DoField:
		return cmp.Or(h.Text, "it has no "+h.Action.Arg), !fieldSet(s.ticket, h.Action.Arg), nil
	case rule.DoPerson:
		return cmp.Or(h.Text, "this waits for the person to approve it"), m.actorKind(s.actor) != rule.ActorPerson, nil
	case rule.DoJev:
		return "the Jev question set " + h.Action.Arg + " is not wired to board hooks, so the check fails closed", true, nil
	case rule.DoCommand:
		output, failed, err := rule.RunCommand(h.Action.Arg, s.path, []string{"TOFU_BOARD_EVENT=" + string(s.event), "TOFU_TICKET=" + s.path,
			"TOFU_TICKET_ID=" + s.ticket.ID, "TOFU_FROM=" + string(s.from), "TOFU_TO=" + string(s.to), "TOFU_ACTOR=" + s.actor})
		if err != nil {
			return "", false, fmt.Errorf("hook %q %w", h.ID, err)
		}
		return strings.TrimSpace(h.Text + "\n" + output), failed, nil
	}
	panic("boardy: unknown check " + string(h.Action.Do))
}

func (m Managed) weigh(hooks []rule.Hook, when rule.When, s subject) ([]Fired, []rule.Hook, error) {
	hooks = slices.DeleteFunc(slices.Clone(hooks), func(h rule.Hook) bool { return h.When != when || h.On != s.event })
	if s.entered.IsZero() {
		s.entered = time.Now()
		if s.ticket.ID != "" && slices.ContainsFunc(hooks, func(h rule.Hook) bool { return h.If.InStatusFor > 0 }) {
			if events, err := m.Store.Events(s.key, 0); err == nil {
				s.entered = cmp.Or(enteredStatus(events)[s.ticket.ID], s.ticket.Created)
			}
		}
	}
	var fired []Fired
	var effects []rule.Hook
	var refusals []string
	var waits error
	for _, h := range hooks {
		if !m.holds(h, s) {
			continue
		}
		if !h.Action.Do.Check() {
			effects = append(effects, h)
			continue
		}
		detail, failed, err := m.check(h, s)
		if err != nil {
			return nil, nil, err
		}
		if !failed {
			continue
		}
		then := h.Then
		if h.Action.Do == rule.DoJev && when == rule.WhenBefore {
			then = rule.ThenAsk
		}
		blocked := then == rule.ThenRefuse || then == rule.ThenAsk && m.actorKind(s.actor) != rule.ActorPerson
		mode := rule.ModeShadow
		if blocked {
			mode = rule.ModeEnforced
		}
		finding := rule.Finding{RuleID: h.ID, Target: s.ticket.ID, Detail: detail}
		fired = append(fired, Fired{Fire: rule.Fire{RuleID: h.ID, Mode: mode, Target: s.ticket.ID, Findings: []rule.Finding{finding}, Blocked: blocked, At: time.Now()}, Checker: string(h.Action.Do), Then: then})
		switch {
		case blocked && then == rule.ThenRefuse:
			refusals = append(refusals, h.ID+": "+detail)
		case blocked:
			waits = WaitingError{Ticket: s.ticket.ID, Rule: h.ID, Detail: detail}
		}
	}
	if refusals != nil {
		return fired, effects, RefusedError{Ticket: s.ticket.ID, Why: strings.Join(refusals, "; ")}
	}
	return fired, effects, waits
}

func hookText(id, what string, event rule.HookEvent, detail string) string {
	return fmt.Sprintf("%s (%s, %s): %s", id, what, event, detail)
}

func (e Event) Nudge() (string, bool) {
	id, rest, found := strings.Cut(e.Text, " ("+string(rule.ThenNudge)+", ")
	_, detail, _ := strings.Cut(rest, "): ")
	return id + ": " + detail, found && e.Kind == RuleFired
}

func (s Store) EventSizes() map[string]int64 {
	boards, _ := s.Boards()
	sizes := map[string]int64{}
	for _, board := range boards {
		if info, err := os.Stat(filepath.Join(board.Dir, EventsFileName)); err == nil {
			sizes[board.Key] = info.Size()
		}
	}
	return sizes
}

func (s Store) NudgesSince(sizes map[string]int64) []string {
	boards, _ := s.Boards()
	var nudges []string
	for _, board := range boards {
		raw, err := os.ReadFile(filepath.Join(board.Dir, EventsFileName))
		if err != nil || int64(len(raw)) <= sizes[board.Key] {
			continue
		}
		for line := range strings.Lines(string(raw[sizes[board.Key]:])) {
			var event Event
			if json.Unmarshal([]byte(line), &event) != nil {
				continue
			}
			if text, isNudge := event.Nudge(); isNudge {
				nudges = append(nudges, text)
			}
		}
	}
	return nudges
}

func (m Managed) record(s subject, fired []Fired) error {
	var err error
	for _, f := range fired {
		kind := RuleFired
		if f.waits() {
			kind = Waiting
		}
		err = errors.Join(err, m.Store.Record(s.key, Event{Ticket: s.ticket.ID, Kind: kind, Actor: s.actor, To: s.to, Text: hookText(f.RuleID, string(f.Then), s.event, f.Text())}))
	}
	return err
}

func (m Managed) note(h rule.Hook, s subject, text string) error {
	return m.Store.Record(s.key, Event{Ticket: s.ticket.ID, Kind: RuleFired, Actor: hookActorPrefix + h.ID, To: s.to, Text: hookText(h.ID, string(h.Action.Do), s.event, text)})
}

func (m Managed) gate(s *subject) error {
	hooks, err := m.Store.hooks(s.key)
	if err != nil {
		return err
	}
	fired, sets, err := m.weigh(hooks, rule.WhenBefore, *s)
	if err = errors.Join(m.record(*s, fired), err); err != nil {
		return err
	}
	for _, h := range sets {
		if err := m.set(h, s); err != nil {
			return err
		}
		if err := m.note(h, *s, s.expand(h.Action.Arg)); err != nil {
			return err
		}
	}
	return nil
}

func (m Managed) after(s subject) error {
	hooks, err := m.Store.hooks(s.key)
	if err != nil {
		return err
	}
	return m.run(hooks, s)
}

func (m Managed) run(hooks []rule.Hook, s subject) error {
	fired, effects, err := m.weigh(hooks, rule.WhenAfter, s)
	err = errors.Join(err, m.record(s, fired))
	for _, h := range effects {
		err = errors.Join(err, m.effect(h, s))
	}
	return err
}

func (m Managed) effect(h rule.Hook, s subject) error {
	arg, depth, actor := s.expand(h.Action.Arg), s.depth+1, hookActorPrefix+h.ID
	if depth > konst.BoardHookDepthMost {
		return m.note(h, s, fmt.Sprintf("stopped at depth %d, the cap: %s %s did not run", konst.BoardHookDepthMost, h.Action.Do, arg))
	}
	var err error
	switch h.Action.Do {
	case rule.DoCreate:
		var made Ticket
		follow := Ticket{Front: Front{Title: arg, Epic: s.ticket.Epic, Sprint: s.ticket.Sprint}, Log: logLine(actor, fmt.Sprintf("made by hook %s after %s of %s", h.ID, s.event, cmp.Or(s.ticket.ID, s.ticket.Title)))}
		if s.ticket.ID != "" {
			follow.Relates = []string{s.ticket.ID}
		}
		made, err = m.created(s.key, follow, actor, "", depth)
		arg += " is " + made.ID
	case rule.DoMove, rule.DoAssign, rule.DoLog:
		var ticket Ticket
		var board Board
		if ticket, board, err = m.ticketOnBoard(s.ticket.ID); err != nil {
			break
		}
		switch h.Action.Do {
		case rule.DoMove:
			_, err = m.move(board, ticket, Status(arg), "hook "+h.ID, actor, depth)
		case rule.DoAssign:
			_, err = m.assign(board, ticket, arg, actor, depth)
		case rule.DoLog:
			_, err = m.comment(board, ticket, arg, actor, depth)
		}
	case rule.DoSet:
		err = m.set(h, &s)
	case rule.DoNotify:
	}
	if err != nil {
		return m.note(h, s, "did not run: "+err.Error())
	}
	return m.note(h, s, arg)
}

func (m Managed) set(h rule.Hook, s *subject) error {
	field, value, _ := strings.Cut(h.Action.Arg, " ")
	value = s.expand(strings.TrimSpace(value))
	if err := s.ticket.set(field, value); err != nil || s.ticket.ID == "" {
		return err
	}
	_, err := m.Store.Edit(s.ticket.ID, func(t *Ticket) error { return t.set(field, value) })
	return err
}

func (m Managed) Fire(when rule.When, event rule.HookEvent, key, id, actor string) error {
	s := subject{event: event, key: key, actor: actor}
	if id != "" {
		ticket, board, err := m.ticketOnBoard(id)
		if err != nil {
			return err
		}
		s = m.subjectOf(event, board.Key, ticket, ticket.Status, actor, 0)
	}
	if when == rule.WhenBefore {
		return m.gate(&s)
	}
	return m.after(s)
}

func (m Managed) Tick(key string) ([]string, error) {
	hooks, err := m.Store.hooks(key)
	if err != nil {
		return nil, err
	}
	state := filepath.Join(m.Store.BoardDir(key), tickedFileName)
	var ran []string
	err = m.Store.locked(key, "hooks-ticked", func() error {
		last := map[string]time.Time{}
		raw, err := os.ReadFile(state)
		if err == nil {
			err = json.Unmarshal(raw, &last)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		now := time.Now()
		due := slices.DeleteFunc(hooks, func(h rule.Hook) bool { return h.On != rule.HookSchedule || !h.Due(last[h.File], now) })
		if len(due) == 0 {
			return nil
		}
		tickets, err := m.Store.Tickets(key)
		events, eventsErr := m.Store.Events(key, 0)
		entered := enteredStatus(events)
		for _, h := range due {
			last[h.File], ran = now, append(ran, h.ID)
			for _, ticket := range tickets {
				s := m.subjectOf(rule.HookSchedule, key, ticket, ticket.Status, "", 0)
				s.from, s.entered = "", cmp.Or(entered[ticket.ID], ticket.Created)
				err = errors.Join(err, m.run([]rule.Hook{h}, s))
			}
		}
		data, marshalErr := json.Marshal(last)
		return errors.Join(err, eventsErr, marshalErr, writeAtomic(state, data))
	})
	return ran, err
}
