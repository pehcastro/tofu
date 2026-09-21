package session

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
)

type Recorder struct {
	store  *Store
	id     string
	body   *os.File
	author string
	last   string
}

func (s *Store) Begin(header Header, author string) (*Recorder, error) {
	if err := s.Write(header, nil); err != nil {
		return nil, err
	}
	body, err := os.OpenFile(filepath.Join(s.Dir(header.ID), bodyName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Recorder{store: s, id: header.ID, body: body, author: author}, nil
}

func (r *Recorder) Append(kind EventKind, body any) error {
	return r.AppendAttempt(kind, "", FirstAttempt, body)
}

func (r *Recorder) AppendAttempt(kind EventKind, id string, attempt int, body any) error {
	if r == nil {
		return nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if id == "" {
		id = NewEventID()
	}
	events, err := r.store.settings.withReads([]Event{{ID: id, Parent: r.last, Author: r.author, Attempt: max(attempt, FirstAttempt), Kind: kind, Body: raw}})
	if err != nil {
		return err
	}
	var lines []byte
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		lines = append(append(lines, line...), '\n')
	}
	if _, err := r.body.Write(lines); err != nil {
		return err
	}
	if len(events) > 0 {
		r.last = events[len(events)-1].ID
	}
	return nil
}

func (r *Recorder) End(header Header, outcome any) error {
	if r == nil {
		return nil
	}
	appended := r.Append(EventOutcome, outcome)
	return cmp.Or(appended, r.body.Close(), r.store.Write(header, nil))
}
