package session

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
)

type Recorder struct {
	store *Store
	id    string
	body  *os.File
}

func (s *Store) Begin(header Header) (*Recorder, error) {
	if err := s.Write(header, nil); err != nil {
		return nil, err
	}
	body, err := os.OpenFile(filepath.Join(s.Dir(header.ID), bodyName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Recorder{store: s, id: header.ID, body: body}, nil
}

func (r *Recorder) Append(kind EventKind, body any) error {
	if r == nil {
		return nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	events, err := r.store.settings.withReads([]Event{{Kind: kind, Body: raw}})
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
	_, err = r.body.Write(lines)
	return err
}

func (r *Recorder) End(header Header, outcome any) error {
	if r == nil {
		return nil
	}
	appended := r.Append(EventOutcome, outcome)
	return cmp.Or(appended, r.body.Close(), r.store.Write(header, nil))
}
