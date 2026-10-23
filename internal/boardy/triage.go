package boardy

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/rule"
	"tofu/internal/sys"
)

const (
	TriageDirName     = "triage"
	triageCounterName = "triage-counter"
)

type Request struct {
	ID     string    `json:"id"`
	By     Author    `json:"by"`
	At     time.Time `json:"at"`
	Ticket Ticket    `json:"ticket"`
}

func (r *Request) UnmarshalJSON(raw []byte) error {
	type stored Request
	var shape struct {
		stored
		By json.RawMessage `json:"by"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return err
	}
	*r = Request(shape.stored)
	var text string
	if len(shape.By) == 0 || json.Unmarshal(shape.By, &text) == nil {
		r.By = unknownAuthor(text)
		return nil
	}
	return json.Unmarshal(shape.By, &r.By)
}

func (s Store) requestPath(key, id string) string {
	return filepath.Join(s.BoardDir(key), TriageDirName, id+".json")
}

func (m Managed) request(key string, ticket Ticket, actor string) (Ticket, error) {
	if err := m.referencesResolve(ticket); err != nil {
		return Ticket{}, err
	}
	ticket.ID, ticket.Status = "", Triage
	s := subject{event: rule.HookTriageRequest, key: key, ticket: ticket, to: Triage, actor: actor}
	if err := m.gate(&s); err != nil {
		return Ticket{}, err
	}
	req := Request{By: *m.Store.authorOf(actor), At: time.Now().UTC().Truncate(time.Second), Ticket: ticket}
	err := m.Store.locked(key, triageCounterName, func() error {
		dir := filepath.Join(m.Store.BoardDir(key), TriageDirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(dir, triageCounterName))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		last, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		req.ID = "T" + strconv.Itoa(last+1)
		data, err := json.MarshalIndent(req, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(m.Store.requestPath(key, req.ID), append(data, '\n')); err != nil {
			return err
		}
		return writeAtomic(filepath.Join(dir, triageCounterName), []byte(strconv.Itoa(last+1)+"\n"))
	})
	if err != nil {
		return Ticket{}, err
	}
	ticket.Reason = fmt.Sprintf("request %s waits in the triage of %s for a manager", req.ID, key)
	return ticket, errors.Join(m.Store.Record(key, Event{Kind: Requested, Actor: actor, To: Triage, Text: req.ID + " " + ticket.Title}), m.after(s))
}

func (s Store) Requests(key string) ([]Request, error) {
	tickets, err := s.Tickets(key)
	var requests []Request
	for _, ticket := range tickets {
		if ticket.Status == Triage {
			requests = append(requests, Request{ID: ticket.ID, By: unknownAuthor(""), At: ticket.Created, Ticket: ticket})
		}
	}
	names, listErr := sys.ListFiles(filepath.Join(s.BoardDir(key), TriageDirName), ".json")
	if !errors.Is(listErr, fs.ErrNotExist) {
		err = errors.Join(err, listErr)
	}
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		req, readErr := s.readRequest(key, strings.TrimSuffix(name, ".json"))
		err = errors.Join(err, readErr)
		requests = append(requests, req)
	}
	slices.SortFunc(requests, func(a, b Request) int { return cmp.Or(cmp.Compare(len(a.ID), len(b.ID)), strings.Compare(a.ID, b.ID)) })
	return requests, err
}

func (m Managed) namesTriagedTicket(key, id string) (bool, error) {
	ref, err := SplitID(id)
	if err != nil {
		return false, nil
	}
	if ref.Key != key {
		return true, fmt.Errorf("%s is on %s, not in the triage of %s", id, ref.Key, key)
	}
	ticket, err := m.Store.Get(id)
	if err == nil && ticket.Status != Triage {
		err = fmt.Errorf("%s is %s, not in the triage of %s", id, ticket.Status, key)
	}
	return true, err
}

func (s Store) readRequest(key, id string) (Request, error) {
	var req Request
	raw, err := os.ReadFile(s.requestPath(key, id))
	if err != nil {
		return req, fmt.Errorf("triage request %s of %s: %w", id, key, err)
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, fmt.Errorf("triage request %s of %s: %w", id, key, err)
	}
	return req, nil
}

func (m Managed) Accept(key, id, actor string) (Ticket, error) {
	return m.AcceptPlanned(key, id, "", "", actor)
}

func (m Managed) AcceptPlanned(key, id, epic, sprint, actor string) (Ticket, error) {
	if err := m.Manages(key, actor, "accept a triage request"); err != nil {
		return Ticket{}, err
	}
	if err := m.Store.planned(key, Front{Epic: epic, Sprint: sprint}); err != nil {
		return Ticket{}, err
	}
	if isTicket, err := m.namesTriagedTicket(key, id); isTicket {
		if err != nil {
			return Ticket{}, err
		}
		if _, err := m.Store.Edit(id, func(t *Ticket) error {
			t.Epic, t.Sprint = cmp.Or(epic, t.Epic), cmp.Or(sprint, t.Sprint)
			return nil
		}); err != nil {
			return Ticket{}, err
		}
		return m.Move(id, Backlog, "accepted from triage", actor)
	}
	var accepted Ticket
	err := m.Store.locked(key, "triage-"+id, func() error {
		req, err := m.Store.readRequest(key, id)
		if err != nil {
			return err
		}
		ticket := req.Ticket
		ticket.Status, ticket.Reason, ticket.Epic, ticket.Sprint = Backlog, "", cmp.Or(epic, ticket.Epic), cmp.Or(sprint, ticket.Sprint)
		ticket.Log = strings.TrimSpace(ticket.Log + "\n" + m.Store.logLine(actor, fmt.Sprintf("accepted triage request %s from %s", id, cmp.Or(req.By.ID, "an unknown actor"))))
		accepting := subject{event: rule.HookTriageAccept, key: key, ticket: ticket, from: Triage, to: Backlog, actor: actor}
		if err := m.gate(&accepting); err != nil {
			return err
		}
		if accepted, err = m.create(key, accepting.ticket, actor, ""); err != nil {
			return err
		}
		return os.Remove(m.Store.requestPath(key, id))
	})
	if err != nil {
		return accepted, err
	}
	err = m.Store.Record(key, Event{Ticket: accepted.ID, Kind: Accepted, Actor: actor, From: Triage, To: accepted.Status, Text: id})
	done := m.subjectOf(rule.HookTriageAccept, key, accepted, accepted.Status, actor, 0)
	done.from = Triage
	return accepted, errors.Join(err, m.after(done))
}

func (m Managed) Drop(key, id, reason, actor string) error {
	if err := m.Manages(key, actor, "drop a triage request"); err != nil {
		return err
	}
	if isTicket, err := m.namesTriagedTicket(key, id); isTicket {
		if err == nil {
			_, err = m.Move(id, Dropped, cmp.Or(reason, "dropped from triage"), actor)
		}
		return err
	}
	err := m.Store.locked(key, "triage-"+id, func() error {
		if _, err := m.Store.readRequest(key, id); err != nil {
			return err
		}
		return os.Remove(m.Store.requestPath(key, id))
	})
	if err != nil {
		return err
	}
	return m.Store.Record(key, Event{Kind: RequestDropped, Actor: actor, From: Triage, Text: strings.TrimSpace(id + " " + reason)})
}
