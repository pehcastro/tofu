package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/librarian/rules"
)

func (m Managed) AddManager(key, name, actor string) (Board, error) {
	return m.changeManagers(key, name, actor, func(board *Board) error {
		if slices.Contains(board.Managers, name) {
			return fmt.Errorf("%s already manages %s", name, key)
		}
		board.Managers = append(board.Managers, name)
		return nil
	})
}

func (m Managed) RemoveManager(key, name, actor string) (Board, error) {
	return m.changeManagers(key, name, actor, func(board *Board) error {
		at := slices.Index(board.Managers, name)
		if at < 0 {
			return fmt.Errorf("%s does not manage %s", name, key)
		}
		board.Managers = slices.Delete(board.Managers, at, at+1)
		return nil
	})
}

func (m Managed) Assign(id, assignee, actor string) (Ticket, error) {
	ref, err := SplitID(id)
	if err != nil {
		return Ticket{}, err
	}
	if err := m.Manages(ref.Key, actor, "assign a ticket"); err != nil {
		return Ticket{}, err
	}
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	return m.assign(board, ticket, assignee, actor, 0)
}

func (m Managed) assign(board Board, ticket Ticket, assignee, actor string, depth int) (Ticket, error) {
	s := m.subjectOf(rules.HookTicketAssign, board.Key, ticket, ticket.Status, actor, depth)
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	assigned, err := m.Store.Edit(ticket.ID, func(t *Ticket) error {
		t.Assignee = assignee
		return nil
	})
	if err == nil {
		err = m.Store.Record(board.Key, Event{Ticket: ticket.ID, Kind: Assigned, Actor: actor, Text: assignee})
	}
	if err != nil {
		return assigned, err
	}
	s.ticket = assigned
	return assigned, m.after(s)
}

func (m Managed) Plan(id, epic, sprint, actor string) (Ticket, error) {
	var said, fields []string
	for _, field := range [][2]string{{"epic", epic}, {"sprint", sprint}} {
		if field[1] != "" {
			said, fields = append(said, field[0]+" "+field[1]), append(fields, field[0])
		}
	}
	if said == nil {
		return Ticket{}, fmt.Errorf("name an epic, a sprint, or both")
	}
	return m.setFields(id, "set "+strings.Join(said, ", "), actor, fields, func(t *Ticket) error {
		if err := m.Store.planned(t.Board(), Front{Epic: epic, Sprint: sprint}); err != nil {
			return err
		}
		t.Epic, t.Sprint = cmp.Or(epic, t.Epic), cmp.Or(sprint, t.Sprint)
		return nil
	})
}

func (m Managed) Retitle(id, title, actor string) (Ticket, error) {
	if title = strings.TrimSpace(title); title == "" {
		return Ticket{}, errors.New("a ticket needs a title")
	}
	return m.revise(id, "set title "+title, actor, func(t *Ticket) error {
		t.Title = title
		return nil
	})
}

func (m Managed) Reown(id string, owns []string, actor string) (Ticket, error) {
	return m.setFields(id, "set owns "+cmp.Or(strings.Join(owns, ", "), "to nothing"), actor, []string{"owns"}, func(t *Ticket) error {
		t.Owns = owns
		return nil
	})
}

func (m Managed) setFields(id, text, actor string, fields []string, change func(*Ticket) error) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.Manages(board.Key, actor, text+" on a ticket"); err != nil {
		return ticket, err
	}
	if err := change(&ticket); err != nil {
		return ticket, err
	}
	s, err := m.gateSet(board.Key, ticket, fields, actor)
	if err != nil {
		return ticket, err
	}
	revised, err := m.revise(id, text, actor, change)
	if err != nil {
		return revised, err
	}
	s.ticket = revised
	return revised, m.after(s)
}

func (m Managed) gateSet(key string, ticket Ticket, fields []string, actor string) (subject, error) {
	s := m.subjectOf(rules.HookTicketSet, key, ticket, ticket.Status, actor, 0)
	s.fields = fields
	return s, m.gate(&s)
}

func (m Managed) revise(id, text, actor string, change func(*Ticket) error) (Ticket, error) {
	ref, err := SplitID(id)
	if err != nil {
		return Ticket{}, err
	}
	if err := m.Manages(ref.Key, actor, text+" on a ticket"); err != nil {
		return Ticket{}, err
	}
	revised, err := m.Store.Edit(id, func(t *Ticket) error {
		if err := change(t); err != nil {
			return err
		}
		t.Log = strings.TrimSpace(t.Log + "\n" + m.Store.logLine(actor, text))
		return nil
	})
	if err != nil {
		return revised, err
	}
	return revised, m.Store.Record(ref.Key, Event{Ticket: id, Kind: Commented, Actor: actor, Text: text})
}

func (s Store) planned(key string, front Front) error {
	epics, epicsErr := s.Epics(key)
	sprints, sprintsErr := s.Sprints(key)
	switch {
	case front.Epic != "" && !slices.ContainsFunc(epics, func(e Epic) bool { return e.ID == front.Epic }):
		return errors.Join(epicsErr, RefusedError{Ticket: front.ID, Why: fmt.Sprintf("%s has no epic %s: tofu boardy epic new %s <title>", key, front.Epic, front.Epic)})
	case front.Sprint != "" && !slices.ContainsFunc(sprints, func(one Sprint) bool { return one.ID == front.Sprint }):
		return errors.Join(sprintsErr, RefusedError{Ticket: front.ID, Why: fmt.Sprintf("%s has no sprint %s: tofu boardy sprint new %s <title>", key, front.Sprint, front.Sprint)})
	}
	return nil
}

func (s Store) epicReady(key string, front Front) error {
	epics, _ := s.Epics(key)
	at := slices.IndexFunc(epics, func(e Epic) bool { return e.ID == front.Epic })
	if front.Epic == "" || front.Parent != "" || at < 0 || !epics[at].Gated {
		return nil
	}
	epic := epics[at]
	switch {
	case epic.Research == "":
		return RefusedError{Ticket: front.ID, Why: fmt.Sprintf("the epic %s has no research note, and its first ticket waits for one: spawn research to read how two rivals solve it, then tofu boardy epic research %s <what they do and the options>", epic.ID, epic.ID)}
	case epic.Design == "" || epic.First == "":
		return RefusedError{Ticket: front.ID, Why: fmt.Sprintf("the epic %s has research but no design naming the smallest first version that runs, and its first ticket waits for one: tofu boardy epic design %s <the shape> --first <the smallest first version>", epic.ID, epic.ID)}
	}
	fronts, err := s.Fronts(key)
	if err != nil {
		return err
	}
	var unrun []string
	for _, held := range fronts {
		if held.Epic != epic.ID || held.ID == front.ID || held.Parent != "" || held.Status == Dropped || held.Status == Triage {
			continue
		}
		if held.Status == Done {
			ticket, err := s.Get(held.ID)
			if err != nil || ticket.Driven != "" {
				return err
			}
		}
		unrun = append(unrun, held.ID)
	}
	if len(unrun) == 0 {
		return nil
	}
	return RefusedError{Ticket: front.ID, Why: fmt.Sprintf("the epic %s already has %s and none is done with a driven path, and an epic takes its next ticket only once its first one runs: drive %s, log the run, and move it to done", epic.ID, strings.Join(unrun, ", "), unrun[0])}
}

func (m Managed) changeManagers(key, name, actor string, change func(*Board) error) (Board, error) {
	if name == "" {
		return Board{}, fmt.Errorf("a manager is a session id or the person's name")
	}
	var board Board
	err := m.Store.locked(key, "board", func() error {
		var err error
		if board, err = m.Store.Board(key); err != nil {
			return err
		}
		if !m.actsForPerson(actor) && m.role(board, actor) != RoleManager {
			return RefusedError{Why: fmt.Sprintf("%s is neither %s nor a manager of %s, so it cannot change who manages it", actor, RolePerson, key)}
		}
		if err := change(&board); err != nil {
			return err
		}
		return writeAtomic(filepath.Join(board.Dir, BoardFileName), board.toml())
	})
	if err != nil {
		return board, err
	}
	return board, m.Store.Record(key, Event{Kind: ManagersChanged, Actor: actor, Text: fmt.Sprintf("managers: %v", board.Managers)})
}
