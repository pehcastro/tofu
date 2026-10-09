package boardy

import (
	"cmp"
	"fmt"
	"strings"
)

func (m Managed) Hand(id, toKey string, to Store, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if _, err := m.manages(board.Key, actor, "hand a ticket to another board"); err != nil {
		return ticket, err
	}
	if !ticket.Status.Live() {
		return ticket, RefusedError{Ticket: id, Why: fmt.Sprintf("it is %s, and only a live ticket is handed: %s", ticket.Status, ticket.Reason)}
	}
	if to.Dir == m.Store.Dir && toKey == board.Key {
		return ticket, fmt.Errorf("%s is already on %s", id, toKey)
	}
	there := Managed{Local: Local{Store: to}, Person: m.Person}
	moved := ticket
	moved.Log = strings.TrimSpace(ticket.Log + "\n" + logLine(actor, "handed from "+id))
	moved.Depends, moved.Blocks, moved.Relates, moved.Duplicates = nil, nil, nil, nil
	if to.Dir == m.Store.Dir {
		moved.Relates = []string{id}
	}
	target, err := to.Board(toKey)
	if err != nil {
		return ticket, err
	}
	var landed Ticket
	if there.role(target, actor) == RoleManager {
		landed, err = there.create(toKey, moved, actor, id)
	} else {
		landed, err = there.request(toKey, moved, actor)
	}
	if err != nil {
		return landed, err
	}
	link := cmp.Or(landed.ID, "the triage of "+toKey)
	if to.Dir != m.Store.Dir {
		link += " in " + to.Dir
	}
	_, err = m.Store.Edit(id, func(t *Ticket) error {
		t.Status, t.Reason = Dropped, "handed to "+link
		t.Log = strings.TrimSpace(t.Log + "\n" + logLine(actor, "handed to "+link))
		return nil
	})
	if err != nil {
		return landed, err
	}
	return landed, m.Store.Record(board.Key, Event{Ticket: id, Kind: Handed, Actor: actor, From: ticket.Status, To: Dropped, Text: link})
}
