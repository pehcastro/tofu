package session

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/sys"
)

const (
	IDPrefix        = "turn-"
	TurnMark        = "@"
	headerName      = "session.json"
	eventsName      = "events.jsonl"
	headName        = "HEAD"
	attachmentsName = "attachments"
)

type Store struct {
	dir      string
	project  string
	settings Settings
	readFile func(path string) ([]byte, error)
	rename   func(from, to string) error
	parsed   map[string][]Event
}

func (s *Store) ReadOnce() *Store {
	once := *s
	once.parsed = map[string][]Event{}
	return &once
}

func (s *Store) ForgetRead() { s.parsed = nil }

func NewStore(dir string) *Store {
	return &Store{dir: dir, settings: DefaultSettings(), readFile: os.ReadFile, rename: os.Rename}
}

func (s *Store) Use(settings Settings) { s.settings = settings }

func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

func (s *Store) Project() string { return s.project }

func (s *Store) State() string { return filepath.Dir(s.dir) }

func (s *Store) AttachmentDir(id string) string {
	return filepath.Join(filepath.Dir(s.dir), attachmentsName, id)
}

func AttachmentPath(id, name string) string { return attachmentsName + "/" + id + "/" + name }

type Shape string

const (
	ShapeEvents     Shape = "events"
	ShapeSingleFile Shape = "single file"
)

func (s *Store) Shape(id string) Shape {
	if _, err := os.Stat(filepath.Join(s.dir, id+legacySuffix)); err == nil {
		return ShapeSingleFile
	}
	return ShapeEvents
}

func SessionsDir(state string) string { return filepath.Join(state, "sessions") }

func OpenAt(state string) *Store { return NewStore(SessionsDir(state)) }

func Open() (*Store, error) { return OpenIn(".") }

func OpenIn(project string) (*Store, error) {
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		return nil, err
	}
	store := OpenAt(state)
	store.project, err = filepath.Abs(project)
	return store, err
}

type LegacyError struct {
	ID string
}

func (e LegacyError) Error() string {
	return "session: " + e.ID + " was recorded in the layout before one folder per session, and tofu migrate converts it"
}

func (s *Store) Write(header Header, events []Event) error {
	if header.ID == "" {
		return errors.New("session: a record with no id has nowhere to be written")
	}
	if s.legacy(header.ID) {
		return LegacyError{ID: header.ID}
	}
	converted := convertTurns(header, events)
	log, err := s.Open(header)
	if err != nil {
		return err
	}
	header.Name = cmp.Or(header.Name, log.header.Name)
	header.Head, log.header = log.header.Head, s.fresh(header)
	for _, event := range converted {
		if _, err := log.Append(event, nil); err != nil {
			return errors.Join(err, log.Close())
		}
	}
	return log.Close()
}

func (s *Store) AppendEvent(id string, kind EventKind, body any) error {
	log, err := s.Open(Header{ID: id})
	if err != nil {
		return err
	}
	_, err = log.Append(Event{Kind: kind}, body)
	return errors.Join(err, log.Close())
}

func (s *Store) writeHeader(header Header, redact sys.KeyRedactor) error {
	body, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return err
	}
	return s.writeWhole(filepath.Join(s.Dir(header.ID), headerName), []byte(redact.Redact(string(body))))
}

func appendLines(path string, lines []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, written := file.Write(lines)
	return cmp.Or(written, file.Close())
}

func (s *Store) writeWhole(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tofu-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(body)
	err = cmp.Or(err, tmp.Close())
	if err == nil {
		err = s.rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

func (s *Store) edit(id string, change func(*Header)) (Header, error) {
	if s.legacy(id) {
		return Header{}, LegacyError{ID: id}
	}
	header, err := s.read(id)
	if err != nil {
		return Header{}, err
	}
	change(&header)
	return header, s.writeHeader(header, sys.LoadKeyRedactor())
}

func (s *Store) SetName(id, given string) (Header, error) {
	name, err := slugOf(sys.LoadKeyRedactor().Redact(given))
	if err != nil {
		return Header{}, err
	}
	listing, err := s.Listing()
	if err != nil {
		return Header{}, err
	}
	family, _, err := listing.FamilyOf(id)
	if err != nil {
		return Header{}, err
	}
	var named Header
	for _, generation := range slices.Backward(family.Generations) {
		edited, err := s.edit(generation.ID, func(header *Header) { header.Name = &name })
		if err != nil {
			return Header{}, err
		}
		if edited.ID == id {
			named = edited
		}
	}
	return named, nil
}

func (s *Store) End(id string, reason EndReason, at time.Time) (Header, error) {
	switch reason {
	case EndedByNew, EndedByClose, EndedByFork:
	default:
		return Header{}, fmt.Errorf("session: %q is not a reason a session ends", string(reason))
	}
	return s.edit(id, func(header *Header) {
		if !header.Ended() {
			header.EndedAt, header.EndReason = &at, reason
		}
	})
}

type EscapingHandleError struct {
	Handle string
}

func (e EscapingHandleError) Error() string {
	return fmt.Sprintf("session: %s is a path rather than the id or the name of a session here: refused, not followed", e.Handle)
}

func namesOneSession(handle string) error {
	if handle == "." || handle == ".." || strings.ContainsAny(handle, `/\`) || filepath.IsAbs(handle) {
		return EscapingHandleError{Handle: handle}
	}
	return nil
}

func (s *Store) Resolve(handle string) ([]Header, error) {
	if handle == "" {
		return nil, errors.New("session: which session? an id or a name names one")
	}
	if err := namesOneSession(handle); err != nil {
		return nil, err
	}
	for _, id := range []string{handle, IDPrefix + handle} {
		if header, err := s.read(id); err == nil {
			return []Header{header}, nil
		}
	}
	listing, err := s.Listing()
	if err != nil {
		return nil, err
	}
	named := resolveIn(listing.Families(), handle)
	if len(named) == 0 {
		return nil, fmt.Errorf("session: nothing here is called %s: %w", handle, fs.ErrNotExist)
	}
	return named, nil
}

func (s *Store) EventsPath(id string) string { return filepath.Join(s.Dir(id), eventsName) }

func (s *Store) Header(handle string) (Header, error) {
	id, _ := s.holding(handle)
	return s.read(id)
}

func (s *Store) Ancestors(id string) ([]Header, error) {
	var chain []Header
	header, err := s.read(id)
	for err == nil && header.Parent != "" && header.Parent != id && !slices.ContainsFunc(chain, func(seen Header) bool { return seen.ID == header.Parent }) {
		if header, err = s.read(header.Parent); err == nil {
			chain = append(chain, header)
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return chain, nil
	}
	return chain, err
}

func (s *Store) holding(handle string) (string, string) {
	id, part, _ := strings.Cut(handle, TurnMark)
	if _, err := s.read(id); part != "" || err == nil {
		return id, part
	}
	listing, err := s.Listing()
	if err != nil {
		return id, ""
	}
	for _, header := range listing.Sessions {
		events, err := s.Events(header.ID)
		if err != nil {
			continue
		}
		for _, event := range events {
			if event.Turn == id || event.Agent == id {
				return header.ID, id
			}
		}
	}
	return id, ""
}

func (s *Store) Events(id string) ([]Event, error) {
	if events, read := s.parsed[id]; read {
		return events, nil
	}
	events, err := s.events(id)
	if err == nil && s.parsed != nil {
		s.parsed[id] = events
	}
	return events, err
}

func (s *Store) events(id string) ([]Event, error) {
	if err := namesOneSession(id); err != nil {
		return nil, err
	}
	if s.legacy(id) {
		return s.legacyEvents(id)
	}
	raw, err := s.readFile(filepath.Join(s.Dir(id), eventsName))
	if err != nil {
		return nil, fmt.Errorf("session: %s has no events: %w", id, err)
	}
	return parseEvents(raw, id)
}

func (s *Store) Body(handle string) ([]Event, error) {
	id, part := s.holding(handle)
	events, err := s.Events(id)
	if err != nil {
		return nil, err
	}
	return s.Part(events, part)
}

func (s *Store) Part(events []Event, part string) ([]Event, error) {
	var kept []Event
	for _, event := range events {
		if event.Agent == part || (part != "" && event.Agent == "" && event.Turn == part) {
			kept = append(kept, event)
		}
	}
	return s.settings.view(kept)
}

func parseEvents(raw []byte, id string) ([]Event, error) {
	var events []Event
	for {
		line, rest, terminated := bytes.Cut(raw, []byte{'\n'})
		if !terminated {
			return events, nil
		}
		raw = rest
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("session: line %d of the events of %s does not parse: %w", len(events)+1, id, err)
		}
		events = append(events, event)
	}
}

func (s *Store) Listing() (Listing, error) {
	ids, err := entryIDs(s.dir)
	if err != nil {
		return Listing{}, err
	}
	var listing Listing
	for _, id := range ids {
		header, err := s.read(id)
		if err != nil {
			listing.Skipped = append(listing.Skipped, Skip{ID: id, Reason: err})
			continue
		}
		listing.Sessions = append(listing.Sessions, header)
	}
	slices.SortFunc(listing.Sessions, func(a, b Header) int { return b.At.Compare(a.At) })
	return listing, nil
}

func (s *Store) Head() (Head, error) {
	raw, err := s.readFile(filepath.Join(s.dir, headName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Head{}, err
	}
	if err == nil {
		named := strings.TrimSpace(string(raw))
		if _, err := s.read(named); err == nil {
			return Head{ID: named}, nil
		}
	}
	listing, err := s.Listing()
	if err != nil {
		return Head{}, err
	}
	for _, header := range listing.Sessions {
		if header.ForkedInto == "" {
			return Head{ID: header.ID, Derived: true}, nil
		}
	}
	return Head{}, errors.New("session: nothing under " + s.dir + " is a session to continue")
}

func (s *Store) SetHead(id string) error {
	if id == "" {
		return errors.New("session: the head has to name a session")
	}
	return s.writeWhole(filepath.Join(s.dir, headName), []byte(id+"\n"))
}

func (s *Store) read(id string) (Header, error) {
	if err := namesOneSession(id); err != nil {
		return Header{}, err
	}
	raw, err := s.readFile(filepath.Join(s.Dir(id), headerName))
	if err == nil {
		var header Header
		if err := json.Unmarshal(raw, &header); err != nil {
			return Header{}, fmt.Errorf("session: the session.json of %s does not parse: %w", id, err)
		}
		if header.CarriedFrom != nil {
			header.Parent = header.CarriedFrom.Session
		}
		return header, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Header{}, err
	}
	return s.legacyHeader(id)
}
