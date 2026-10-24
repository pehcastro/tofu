package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/rule"
	"tofu/internal/subagent"
)

func (m Managed) Create(key string, ticket Ticket, actor string) (Ticket, error) {
	board, err := m.Store.Board(key)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.Status == Triage || m.role(board, actor) != RoleManager {
		return m.Request(key, ticket, actor)
	}
	if err := m.Store.planned(key, ticket.Front); err != nil {
		return Ticket{}, err
	}
	ticket.Acceptance = bulleted(ticket.Acceptance)
	return m.create(key, ticket, actor, "")
}

func (m Managed) Request(key string, ticket Ticket, actor string) (Ticket, error) {
	if _, err := m.Store.Board(key); err != nil {
		return Ticket{}, err
	}
	if err := m.Store.planned(key, ticket.Front); err != nil {
		return Ticket{}, err
	}
	ticket.Acceptance = bulleted(ticket.Acceptance)
	return m.request(key, ticket, actor)
}

func bulleted(revisions []string) []string {
	var out []string
	for _, revision := range revisions {
		lines := slices.DeleteFunc(strings.Split(revision, "\n"), func(line string) bool { return strings.TrimSpace(line) == "" })
		if !slices.ContainsFunc(lines, func(line string) bool { return strings.HasPrefix(line, "- ") }) {
			for i, line := range lines {
				lines[i] = "- " + strings.TrimSpace(line)
			}
			revision = strings.Join(lines, "\n")
		}
		out = append(out, revision)
	}
	return out
}

func (m Managed) create(key string, ticket Ticket, actor, replaces string) (Ticket, error) {
	return m.created(key, ticket, actor, replaces, 0)
}

func (m Managed) created(key string, ticket Ticket, actor, replaces string, depth int) (Ticket, error) {
	board, err := m.Store.Board(key)
	if err != nil {
		return Ticket{}, err
	}
	status := cmp.Or(ticket.Status, Backlog)
	if err := m.vetCreate(board, ticket, status, replaces); err != nil {
		return Ticket{}, err
	}
	preview, err := os.CreateTemp(m.Store.BoardDir(key), ".tofu-create-*.md")
	if err != nil {
		return Ticket{}, err
	}
	_, err = preview.Write(ticket.Markdown())
	err = errors.Join(err, preview.Close())
	s := subject{event: rule.HookTicketCreate, key: key, ticket: ticket, path: preview.Name(), to: status, actor: actor, depth: depth}
	if err == nil {
		err = m.gate(&s)
	}
	if err = errors.Join(err, os.Remove(preview.Name())); err != nil {
		return Ticket{}, err
	}
	made, err := m.Local.Create(key, s.ticket, actor)
	if err != nil {
		return made, err
	}
	s.ticket = made
	s.path, _ = m.Store.TicketPath(made.ID)
	return made, m.after(s)
}

func (m Managed) Move(id string, to Status, reason, actor string) (Ticket, error) {
	return m.MoveLeaving(id, to, reason, actor, nil)
}

func (m Managed) MoveLeaving(id string, to Status, reason, actor string, leave []int) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.mayMove(m.role(board, actor), ticket, to, actor); err != nil || ticket.Status == to {
		return ticket, err
	}
	return m.move(board, ticket, to, reason, actor, 0, leave)
}

func (m Managed) move(board Board, ticket Ticket, to Status, reason, actor string, depth int, leave []int) (Ticket, error) {
	if err := m.validMove(board, ticket, to); err != nil {
		return ticket, err
	}
	s := m.subjectOf(moveEvent(to), board.Key, ticket, to, actor, depth)
	s.leave = leave
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	moved, err := m.Local.Move(ticket.ID, to, reason, actor, leave)
	if err != nil {
		return moved, err
	}
	s.ticket = moved
	return moved, m.after(s)
}

func (m Managed) Check(event rule.Event, id string, to Status, actor string) ([]Fired, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return nil, err
	}
	to = cmp.Or(to, ticket.Status)
	hook := event.Hook()
	switch event {
	case rule.EventMove, rule.EventClose:
		hook = moveEvent(to)
		if err = m.mayMove(m.role(board, actor), ticket, to, actor); err == nil {
			err = m.validMove(board, ticket, to)
		}
	case rule.EventCreate:
		err = m.vetCreate(board, ticket, to, "")
	case rule.EventWiden:
		err = m.mayWiden(board, ticket, actor)
	default:
		return nil, fmt.Errorf("%q is not a board event", event)
	}
	if err != nil {
		return nil, err
	}
	hooks, err := m.Store.hooks(board.Key)
	if err != nil {
		return nil, err
	}
	fired, _, err := m.weigh(hooks, rule.WhenBefore, m.subjectOf(hook, board.Key, ticket, to, actor, 0))
	return fired, err
}

func moveEvent(to Status) rule.HookEvent {
	if !to.Live() {
		return rule.HookTicketClose
	}
	return rule.HookTicketMove
}

func (m Managed) vetCreate(board Board, ticket Ticket, status Status, replaces string) error {
	if err := m.referencesResolve(ticket); err != nil {
		return err
	}
	if err := board.cited(ticket); err != nil {
		return err
	}
	return m.ownsFree(board.Key, ticket.Front, ticket.Owns, status, replaces)
}

func (m Managed) mayWiden(board Board, ticket Ticket, actor string) error {
	if err := m.Manages(board.Key, actor, "widen owns"); err != nil {
		return err
	}
	return board.cited(ticket)
}

func (m Managed) Comment(id, text, actor string) (Ticket, error) {
	return m.appended(id, Commented, text, actor)
}

func (m Managed) Evidence(id, text, actor string, line int) (Ticket, error) {
	ticket, board, err := m.mayAppend(id, actor)
	if err != nil {
		return ticket, err
	}
	if lines := len(subagent.InForce(ticket.Acceptance)); line < 0 || line > lines {
		return ticket, fmt.Errorf("line counts %s's acceptance lines from 1, and it has %d", id, lines)
	}
	return m.entry(board, ticket, Event{Kind: Evidence, Text: text, Line: line}, actor, 0)
}

func (m Managed) appended(id string, kind EventKind, text, actor string) (Ticket, error) {
	ticket, board, err := m.mayAppend(id, actor)
	if err != nil {
		return ticket, err
	}
	return m.entry(board, ticket, Event{Kind: kind, Text: text}, actor, 0)
}

func (m Managed) evidenceRunes(id string) (int, error) {
	thread, err := m.Store.Thread(id)
	runes := 0
	for _, event := range thread {
		if event.Kind == Evidence {
			runes += utf8.RuneCountInString(event.Text)
		}
	}
	return runes, err
}

func (m Managed) comment(board Board, ticket Ticket, text, actor string, depth int) (Ticket, error) {
	return m.entry(board, ticket, Event{Kind: Commented, Text: text}, actor, depth)
}

func (m Managed) entry(board Board, ticket Ticket, event Event, actor string, depth int) (Ticket, error) {
	s := m.subjectOf(rule.HookTicketLog, board.Key, ticket, ticket.Status, actor, depth)
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	logged, err := m.appendLog(ticket.ID, event.Text, actor, event)
	if err != nil {
		return logged, err
	}
	s.ticket = logged
	return logged, m.after(s)
}

func (m Managed) AskInLog(id, question, actor string) (Ticket, error) {
	if ticket, _, err := m.mayAppend(id, actor); err != nil {
		return ticket, err
	}
	return m.appendLog(id, "question for a manager: "+question, actor, Event{Kind: Asked, Text: question})
}

func (m Managed) mayAppend(id, actor string) (Ticket, Board, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err == nil && m.role(board, actor) == RoleAgent && !heldBy(ticket, actor) {
		err = RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and an agent appends only to its own ticket's Log", ticket.Assignee)}
	}
	return ticket, board, err
}

func (m Managed) Widen(id string, owns []string, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.mayWiden(board, ticket, actor); err != nil {
		return ticket, err
	}
	added := slices.DeleteFunc(slices.Clone(owns), func(glob string) bool { return slices.Contains(ticket.Owns, glob) })
	if len(added) == 0 {
		return ticket, fmt.Errorf("%s already owns %s", id, strings.Join(owns, ", "))
	}
	if err := m.ownsFree(board.Key, ticket.Front, added, ticket.Status, ""); err != nil {
		return ticket, err
	}
	s := m.subjectOf(rule.HookTicketWiden, board.Key, ticket, ticket.Status, actor, 0)
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	widened, err := m.Store.Edit(id, func(t *Ticket) error {
		t.Owns = append(t.Owns, added...)
		return nil
	})
	if err == nil {
		err = m.Store.Record(board.Key, Event{Ticket: id, Kind: Widened, Actor: actor, Text: strings.Join(added, ", ")})
	}
	if err != nil {
		return widened, err
	}
	s.ticket = widened
	return widened, m.after(s)
}

func (m Managed) ticketOnBoard(id string) (Ticket, Board, error) {
	ticket, err := m.Store.Get(id)
	if err != nil {
		return ticket, Board{}, err
	}
	board, err := m.Store.Board(ticket.Board())
	return ticket, board, err
}

func (m Managed) mayMove(role Role, ticket Ticket, to Status, actor string) error {
	if ticket.Status == to {
		return nil
	}
	switch role {
	case RoleManager:
		return nil
	case RolePerson:
		if !to.Live() {
			return nil
		}
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("%s is not a manager of %s, and %s only closes a ticket", actor, ticket.Board(), RolePerson)}
	case RoleAgent:
		if !heldBy(ticket, actor) {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it is assigned to %q, and an agent moves only its own ticket", ticket.Assignee)}
		}
		if ticket.Status != Doing || to != Review {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("an agent moves its own ticket from %s to %s, and only a manager moves it from %s to %s", Doing, Review, ticket.Status, to)}
		}
		return nil
	}
	panic("boardy: unknown role " + string(role))
}

func (m Managed) validMove(board Board, ticket Ticket, to Status) error {
	if slices.Contains([]Status{Doing, Review, Done}, to) && !slices.ContainsFunc(ticket.Acceptance, func(lines string) bool { return strings.TrimSpace(lines) != "" }) {
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it has no Acceptance, and a ticket reaches %s only with one", to)}
	}
	if to == Review && strings.TrimSpace(ticket.Log) == "" {
		return RefusedError{Ticket: ticket.ID, Why: "its Log is empty, and a ticket reaches review with at least one entry"}
	}
	if to == Review && board.LogToReview > 0 {
		runes, err := m.evidenceRunes(ticket.ID)
		if err != nil {
			return err
		}
		if runes < board.LogToReview {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("its evidence entries have %d characters, and a ticket reaches review on %s with at least %d: log them with ticket_log kind evidence or tofu boardy log --evidence", runes, board.Key, board.LogToReview)}
		}
	}
	if to == NeedsDecision {
		if err := decidable(ticket); err != nil {
			return err
		}
	}
	if to == Done {
		if gate := m.doneGate(ticket.Owns); gate.Refusal() != "" {
			return RefusedError{Ticket: ticket.ID, Why: gate.Refusal(), Done: &gate}
		}
	}
	if to == Review || to == Done {
		children, _ := filepath.Glob(filepath.Join(board.Dir, TicketsDirName, ticket.ID, "*.md"))
		var live []string
		for _, path := range children {
			child, err := frontOf(path)
			if err != nil {
				return err
			}
			if child.Status.Live() {
				live = append(live, child.ID)
			}
		}
		if len(live) > 0 {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("its sub-tickets %s are still live, and a parent reaches %s only once they are done or dropped", strings.Join(live, ", "), to)}
		}
	}
	var err error
	if to == Doing || (!ticket.Status.Live() && to.Live()) {
		err = m.referencesResolve(ticket)
	}
	if !board.Holding(ticket.Status) {
		err = errors.Join(err, m.ownsFree(board.Key, ticket.Front, ticket.Owns, to, ""))
	}
	return err
}

func (m Managed) doneGate(owns []string) DoneGate {
	if m.head == nil {
		return checkDone(m.Store.Root, owns)
	}
	return m.head.owning(m.Store.Root, owns)
}

func (m Managed) referencesResolve(ticket Ticket) error {
	for _, id := range slices.Concat(ticket.Depends, ticket.Blocks, ticket.Relates, ticket.Duplicates) {
		if _, err := m.Store.Get(id); err != nil {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it names %s, which does not resolve: %v", id, err)}
		}
	}
	return nil
}

func (m Managed) ownsFree(key string, ticket Front, owns []string, status Status, replaces string) error {
	id := ticket.ID
	for _, glob := range owns {
		if err := unreadableOwns(glob); err != nil {
			return RefusedError{Ticket: id, Why: err.Error()}
		}
	}
	if ticket.Parent != "" {
		parent, err := m.Store.Get(ticket.Parent)
		if err != nil {
			return err
		}
		if len(owns) > 0 && len(parent.Owns) == 0 {
			return RefusedError{Ticket: id, Why: fmt.Sprintf("its parent %s owns no paths, and a parent's owns are the ceiling for its sub-tickets: create it without a parent, or widen %s first", parent.ID, parent.ID)}
		}
		if glob := outsideOwns(owns, parent.Owns); glob != "" {
			return RefusedError{Ticket: id, Why: fmt.Sprintf("%s is outside the owns of its parent %s (%s), and a parent's owns are the ceiling for its sub-tickets", glob, parent.ID, strings.Join(parent.Owns, ", "))}
		}
	}
	boards, err := m.Store.Boards()
	if err != nil {
		return err
	}
	if len(owns) == 0 || !slices.ContainsFunc(boards, func(b Board) bool { return b.Key == key && b.Holding(status) }) {
		return nil
	}
	for _, board := range boards {
		fronts, err := m.Store.Fronts(board.Key)
		if err != nil {
			return err
		}
		for _, held := range fronts {
			if held.ID == id || held.ID == replaces || family(ticket, held) || !board.Holding(held.Status) {
				continue
			}
			if glob, other, found := ownsMeet(owns, held.Owns); found {
				return RefusedError{Ticket: id, Why: fmt.Sprintf("%s overlaps %s, held by %s (%s)", glob, other, held.ID, held.Status)}
			}
		}
	}
	return nil
}

func (s Store) logLine(actor, text string) string {
	return "- " + time.Now().Format(time.DateOnly) + " " + s.authorOf(actor).ID + ": " + text
}
