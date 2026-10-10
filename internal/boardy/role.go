package boardy

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

type Role string

const (
	RolePerson  Role = "person"
	RoleManager Role = "manager"
	RoleAgent   Role = "agent"
)

type Managed struct {
	Local
	Person string
}

type RefusedError struct {
	Ticket string
	Why    string
}

func (e RefusedError) Error() string {
	if e.Ticket == "" {
		return "refused: " + e.Why
	}
	return fmt.Sprintf("%s refused: %s", e.Ticket, e.Why)
}

type WaitingError struct {
	Ticket string
	Rule   string
	Detail string
}

func (e WaitingError) Error() string {
	return fmt.Sprintf("%s waits for %s: %s (rule %s)", e.Ticket, RolePerson, e.Detail, e.Rule)
}

const subAgentOfSession = "/"

func (m Managed) actsForPerson(actor string) bool {
	if m.Person == "" {
		return actor != "" && !strings.Contains(actor, subAgentOfSession)
	}
	return actor == m.Person
}

func (m Managed) role(board Board, actor string) Role {
	forPerson := m.actsForPerson(actor)
	switch {
	case slices.Contains(board.Managers, actor):
		return RoleManager
	case forPerson && (len(board.Managers) == 0 || slices.Contains(board.Managers, cmp.Or(m.Person, string(RolePerson)))):
		return RoleManager
	case forPerson:
		return RolePerson
	}
	return RoleAgent
}

func (m Managed) Manages(key, actor, doing string) error {
	board, err := m.Store.Board(key)
	if err != nil {
		return err
	}
	if m.role(board, actor) != RoleManager {
		return RefusedError{Why: fmt.Sprintf("%s is not a manager of %s, and only a manager may %s", actor, key, doing)}
	}
	return nil
}
