package session

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"tofu/internal/sys"
)

type Log struct {
	store   *Store
	mu      sync.Mutex
	file    *os.File
	lock    *os.File
	header  Header
	seq     int
	last    map[string]string
	prompts map[string]string
	redact  sys.KeyRedactor
	blobs   map[string]bool
	side    map[string]*os.File
	watch   func(Event)
}

func (l *Log) Observe(watch func(Event)) {
	l.mu.Lock()
	l.watch = watch
	l.mu.Unlock()
}

func (s *Store) Open(header Header) (log *Log, err error) {
	if header.ID == "" {
		header.ID = NewEventID()
	}
	if err := namesOneSession(header.ID); err != nil {
		return nil, err
	}
	if s.legacy(header.ID) {
		header.CarriedFrom, header.ID = &Carried{Session: header.ID}, NewEventID()
	}
	if err := os.MkdirAll(s.Dir(header.ID), 0o755); err != nil {
		return nil, err
	}
	lock, err := s.lock(header.ID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && lock != nil {
			err = errors.Join(err, unlock(lock))
		}
	}()
	log = &Log{store: s, lock: lock, last: map[string]string{}, prompts: map[string]string{}, redact: sys.LoadKeyRedactor(), side: map[string]*os.File{}}
	kept, err := s.read(header.ID)
	switch {
	case err == nil:
		log.header = kept
		if err := log.resume(); err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		log.header = s.fresh(header)
	default:
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.Dir(log.header.ID), eventsName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	log.file = file
	if err := log.save(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return log, nil
}

func (s *Store) fresh(header Header) Header {
	header.Schema = SchemaVersion
	header.Project = cmp.Or(header.Project, s.project)
	if header.Name == nil {
		name := newName()
		header.Name = &name
	}
	if header.Root == "" {
		header.Root = header.ID
	}
	if header.At.IsZero() {
		header.At = time.Now()
	}
	return header
}

func (l *Log) resume() error {
	path := filepath.Join(l.store.Dir(l.header.ID), eventsName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if whole := bytes.LastIndexByte(raw, '\n') + 1; whole < len(raw) {
		if err := os.Truncate(path, int64(whole)); err != nil {
			return err
		}
		raw = raw[:whole]
	}
	events, err := parseEvents(raw, l.header.ID)
	if err != nil {
		return err
	}
	for _, event := range events {
		l.seq = max(l.seq, event.Seq)
		l.last[event.Agent] = event.ID
		if event.Kind == EventPrompt {
			l.prompts[event.Agent] = string(event.Body)
		}
	}
	return nil
}

func (l *Log) Prompted(agent string, body any) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prompts[agent] == string(marshalled(body))
}

func (l *Log) ID() string { return l.header.ID }

func (l *Log) Header() Header {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.header
}

func (l *Log) Append(event Event, body any) (Event, error) {
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return Event{}, err
		}
		event.Body = raw
	}
	if event.Body != nil {
		event.Body = json.RawMessage(l.redact.Redact(string(event.Body)))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	event.Seq = l.seq
	event.ID = cmp.Or(event.ID, NewEventID())
	event.Parent = cmp.Or(event.Parent, l.last[event.Agent])
	if event.At.IsZero() {
		event.At = time.Now()
	}
	line, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	if _, err := l.file.Write(append(line, '\n')); err != nil {
		return Event{}, err
	}
	l.last[event.Agent], l.header.Head = event.ID, event.ID
	if event.Kind == EventPrompt {
		l.prompts[event.Agent] = string(event.Body)
	}
	if l.watch != nil {
		bodiless := event
		bodiless.Body = nil
		l.watch(bodiless)
	}
	return event, nil
}

func (l *Log) Spent(agent, model string, usage Usage, cost float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.header.Usage, l.header.CostUSD = l.header.Usage.Plus(usage), l.header.CostUSD+cost
	if model != "" && !slices.Contains(l.header.Models, model) {
		l.header.Models = append(l.header.Models, model)
	}
	for i := range l.header.Agents {
		if l.header.Agents[i].Agent == agent {
			l.header.Agents[i].Usage, l.header.Agents[i].CostUSD = l.header.Agents[i].Usage.Plus(usage), l.header.Agents[i].CostUSD+cost
		}
	}
}

func (l *Log) Edit(change func(*Header)) error {
	l.mu.Lock()
	change(&l.header)
	l.mu.Unlock()
	return l.save()
}

func (l *Log) Close() error {
	l.mu.Lock()
	side := l.closeSide()
	l.mu.Unlock()
	closed := errors.Join(l.save(), l.file.Close(), side)
	if l.lock == nil {
		return closed
	}
	return errors.Join(closed, unlock(l.lock))
}

func (l *Log) save() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if kept, err := l.store.read(l.header.ID); err == nil && kept.Name != nil {
		l.header.Name = kept.Name
	}
	return l.store.writeHeader(l.header, l.redact)
}
