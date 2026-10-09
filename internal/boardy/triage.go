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

	"tofu/internal/sys"
)

const (
	TriageDirName     = "triage"
	triageCounterName = "triage-counter"
)

type Request struct {
	ID     string    `json:"id"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Ticket Ticket    `json:"ticket"`
}

func (s Store) requestPath(key, id string) string {
	return filepath.Join(s.BoardDir(key), TriageDirName, id+".json")
}

func (m Managed) request(key string, ticket Ticket, actor string) (Ticket, error) {
	if err := m.referencesResolve(ticket); err != nil {
		return Ticket{}, err
	}
	ticket.ID, ticket.Status = "", Triage
	req := Request{By: actor, At: time.Now().UTC().Truncate(time.Second), Ticket: ticket}
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
	return ticket, m.Store.Record(key, Event{Kind: Requested, Actor: actor, To: Triage, Text: req.ID + " " + ticket.Title})
}

func (s Store) Requests(key string) ([]Request, error) {
	dir := filepath.Join(s.BoardDir(key), TriageDirName)
	names, err := sys.ListFiles(dir, ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	requests := make([]Request, 0, len(names))
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
	if _, err := m.manages(key, actor, "accept a triage request"); err != nil {
		return Ticket{}, err
	}
	var accepted Ticket
	err := m.Store.locked(key, "triage-"+id, func() error {
		req, err := m.Store.readRequest(key, id)
		if err != nil {
			return err
		}
		ticket := req.Ticket
		ticket.Status, ticket.Reason = Backlog, ""
		ticket.Log = strings.TrimSpace(ticket.Log + "\n" + logLine(actor, fmt.Sprintf("accepted triage request %s from %s", id, req.By)))
		if accepted, err = m.create(key, ticket, actor, ""); err != nil {
			return err
		}
		return os.Remove(m.Store.requestPath(key, id))
	})
	if err != nil {
		return accepted, err
	}
	return accepted, m.Store.Record(key, Event{Ticket: accepted.ID, Kind: Accepted, Actor: actor, From: Triage, To: accepted.Status, Text: id})
}

func (m Managed) Drop(key, id, reason, actor string) error {
	if _, err := m.manages(key, actor, "drop a triage request"); err != nil {
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
