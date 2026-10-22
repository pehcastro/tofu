package boardy

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type EventKind string

const (
	Created   EventKind = "created"
	Moved     EventKind = "moved"
	Commented EventKind = "commented"
	Evidence  EventKind = "evidence"
	Spawned   EventKind = "spawned"
	Messaged  EventKind = "messaged"
	Reported  EventKind = "reported"
)

func (EventKind) Enum() []string {
	return enum(Created, Moved, Commented, Evidence, RuleFired, Waiting, Requested, Accepted, RequestDropped, Handed, Widened, ManagersChanged, Assigned, Asked, Answered, Spawned, Messaged, Reported, HookChanged, HookFailed)
}

type Event struct {
	At      time.Time `json:"at"`
	Ticket  string    `json:"ticket"`
	Kind    EventKind `json:"kind"`
	Actor   string    `json:"actor,omitempty"`
	Author  *Author   `json:"author,omitempty"`
	From    Status    `json:"from,omitempty"`
	To      Status    `json:"to,omitempty"`
	Text    string    `json:"text,omitempty"`
	Session string    `json:"session,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	Call    string    `json:"call,omitempty"`
	Files   []string  `json:"files,omitempty"`
}

func (s Store) RecordTicket(event Event) error {
	ref, err := SplitID(event.Ticket)
	if err != nil {
		return err
	}
	return s.Record(ref.Key, event)
}

func (s Store) Thread(id string) ([]Event, error) {
	ref, err := SplitID(id)
	if err != nil {
		return nil, err
	}
	all, err := s.Events(ref.Key, 0)
	thread := []Event{}
	for _, event := range all {
		if event.Ticket == id {
			thread = append(thread, event)
		}
	}
	return thread, err
}

func (s Store) Record(key string, event Event) error {
	event.At = cmp.Or(event.At, time.Now().UTC().Truncate(time.Second))
	if event.Author == nil && event.Actor != "" {
		event.Author = s.authorOf(event.Actor)
	}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.locked(key, "events", func() error {
		file, err := os.OpenFile(filepath.Join(s.BoardDir(key), EventsFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = file.Write(append(line, '\n'))
		return errors.Join(err, file.Close())
	})
}

func (s Store) Events(key string, from int) ([]Event, error) {
	file, err := os.Open(filepath.Join(s.BoardDir(key), EventsFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var lines [][]byte
	var skipped int
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		if skipped < from {
			skipped++
			continue
		}
		lines = append(lines, slices.Clone(scanner.Bytes()))
	}
	events := make([]Event, len(lines))
	decodeErrs := s.eachInParallel(len(lines), func(at int) error { return json.Unmarshal(lines[at], &events[at]) })
	decoded, err := len(lines), scanner.Err()
	if at := slices.IndexFunc(decodeErrs, func(err error) bool { return err != nil }); at >= 0 {
		decoded, err = at, fmt.Errorf("%s line %d: %w", EventsFileName, skipped+at+1, decodeErrs[at])
	}
	if decoded == 0 {
		return nil, err
	}
	return events[:decoded], err
}
