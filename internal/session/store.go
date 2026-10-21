package session

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/safe"
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

const (
	settleWait  time.Duration = 500 * time.Millisecond
	settleRetry time.Duration = 2 * time.Millisecond
)

type Store struct {
	dir      string
	project  string
	settings Settings
	readFile func(path string) ([]byte, error)
	rename   func(from, to string) error
	parsed   map[string][]Event
	headers  map[string]Header
	redactor func() sys.KeyRedactor
}

func (s *Store) ReadOnce() *Store {
	once := *s
	once.parsed, once.headers = map[string][]Event{}, map[string]Header{}
	return &once
}

func (s *Store) ForgetRead() { s.parsed, s.headers = nil, nil }

func NewStore(dir string) *Store {
	return &Store{dir: dir, settings: DefaultSettings(), readFile: os.ReadFile, rename: os.Rename, redactor: sync.OnceValue(sys.LoadKeyRedactor)}
}

func (s *Store) Use(settings Settings) { s.settings = settings }

func (s *Store) Redacts(redactor func() sys.KeyRedactor) { s.redactor = redactor }

func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

func (s *Store) Project() string { return s.project }

func (s *Store) State() string { return filepath.Dir(s.dir) }

func (s *Store) crashed(p safe.Panic) { p.Saved(s.State()) }

func (s *Store) AttachmentDir(id string) string {
	return filepath.Join(filepath.Dir(s.dir), attachmentsName, id)
}

func AttachmentPath(id, name string) string { return attachmentsName + "/" + id + "/" + name }

func (s *Store) AttachmentFile(recorded string) string {
	return filepath.Join(filepath.Dir(s.dir), filepath.FromSlash(recorded))
}

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
	delete(s.headers, header.ID)
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
		err = settled(func() error { return s.rename(name, path) })
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

func settled(try func() error) error {
	deadline := time.Now().Add(settleWait)
	for {
		err := try()
		if err == nil || errors.Is(err, fs.ErrNotExist) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(settleRetry)
	}
}

func (s *Store) readSettled(path string) (raw []byte, err error) {
	err = settled(func() error {
		raw, err = s.readFile(path)
		return err
	})
	return raw, err
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
	return header, s.writeHeader(header, s.redactor())
}

func (s *Store) SetName(id, given string) (Header, error) {
	name, err := slugOf(s.redactor().Redact(given))
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

type Generation struct {
	Header  Header
	Events  []Event
	Unread  bool
	readFor string
}

const (
	agentsTracedSince = 4
	lineageLogEnv     = "TOFU_LINEAGE_LOG"
)

func untraced(header Header) string {
	if header.Schema < agentsTracedSince {
		return "schema " + strconv.Itoa(header.Schema) + " lists no agents"
	}
	return ""
}

func (s *Store) Lineage(id string) ([]Generation, error) {
	head, err := s.Events(id)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	named := namesIn(head)
	lineage, err := s.lineage(id, func(header Header) string {
		for _, run := range header.Agents {
			switch {
			case run.EndedAt == nil:
				return run.Agent + " has no end"
			case named[run.Agent] || named[run.SpawnCall]:
				return "the head names " + run.Agent
			}
		}
		return untraced(header)
	})
	if err == nil {
		logLineage(id, lineage)
	}
	return lineage, err
}

func (s *Store) LineageNaming(id, name string) ([]Generation, error) {
	lineage, err := s.lineage(id, untraced)
	if err != nil {
		return nil, err
	}
	for i := range lineage {
		generation := &lineage[i]
		if !generation.Unread {
			continue
		}
		raw, err := s.readFile(s.EventsPath(generation.Header.ID))
		if errors.Is(err, fs.ErrNotExist) || err == nil && !bytes.Contains(raw, []byte(name)) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if generation.Events, err = parseEvents(raw, generation.Header.ID); err != nil {
			return nil, err
		}
		generation.Unread, generation.readFor = false, "its events name "+name
	}
	logLineage(id, lineage)
	return lineage, nil
}

func (s *Store) lineage(id string, needed func(Header) string) ([]Generation, error) {
	head, err := s.read(id)
	if err != nil {
		return nil, err
	}
	ancestors, err := s.Ancestors(id)
	if err != nil {
		return nil, err
	}
	var lineage []Generation
	for _, header := range slices.Backward(append([]Header{head}, ancestors...)) {
		generation := Generation{Header: header, Unread: true}
		if header.ID != id {
			generation.readFor = needed(header)
		}
		if header.ID == id || generation.readFor != "" {
			events, err := s.Events(header.ID)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			generation.Events, generation.Unread = events, err != nil
		}
		lineage = append(lineage, generation)
	}
	return lineage, nil
}

func logLineage(head string, lineage []Generation) {
	path := os.Getenv(lineageLogEnv)
	if path == "" {
		return
	}
	var lines strings.Builder
	read := 0
	for _, generation := range lineage {
		if generation.readFor != "" {
			read++
			fmt.Fprintf(&lines, "lineage of %s: read %s, generation %d: %s\n", head, generation.Header.ID, generation.Header.Generation, generation.readFor)
		}
	}
	fmt.Fprintf(&lines, "lineage of %s: read %d of %d older generations\n", head, read, len(lineage)-1)
	_ = appendLines(path, []byte(lines.String()))
}

func (s *Store) Unread(id string) { delete(s.parsed, id) }

func (s *Store) Spawns(id string) ([]Event, error) {
	raw, err := s.readFile(s.EventsPath(id))
	if err != nil {
		return nil, err
	}
	var spawns []Event
	for line := range bytes.Lines(raw) {
		var event Event
		if bytes.Contains(line, []byte(`"kind":"`+EventSpawn+`"`)) && json.Unmarshal(line, &event) == nil && event.Kind == EventSpawn {
			spawns = append(spawns, event)
		}
	}
	return spawns, nil
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
	return s.settings.shown(partOf(events, part), withSteps, s.crashed)
}

func (s *Store) Said(events []Event) (said []Event, steps int, err error) {
	kept := partOf(events, "")
	for _, event := range kept {
		if event.Kind == EventRequest || event.Kind == EventStep {
			steps++
		}
	}
	said, err = s.settings.shown(kept, withoutSteps, s.crashed)
	return said, steps, err
}

func partOf(events []Event, part string) []Event {
	var kept []Event
	for _, event := range events {
		if event.Agent == part || (part != "" && event.Agent == "" && event.Turn == part) {
			kept = append(kept, event)
		}
	}
	return kept
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
		event, err := parseLine[Event](line, id, len(events)+1)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
}

func parseLine[T any](line []byte, id string, number int) (T, error) {
	var parsed T
	if err := json.Unmarshal(line, &parsed); err != nil {
		return parsed, fmt.Errorf("session: line %d of the events of %s does not parse: %w", number, id, err)
	}
	return parsed, nil
}

type EventHead struct {
	At    time.Time `json:"at"`
	Turn  string    `json:"turn,omitempty"`
	Agent string    `json:"agent,omitempty"`
	Kind  EventKind `json:"kind"`
}

func (s *Store) EachEventHead(id string, visit func(EventHead)) error {
	return s.scanEvents(id, func(event Event) bool {
		visit(EventHead{At: event.At, Turn: event.Turn, Agent: event.Agent, Kind: event.Kind})
		return true
	}, func(line []byte, number int) (bool, error) {
		head, err := parseLine[EventHead](line, id, number)
		if err == nil {
			visit(head)
		}
		return err == nil, err
	})
}

func (s *Store) EachEvent(id string, wanted func(agent string) bool, visit func(Event) bool) error {
	return s.scanEvents(id, func(event Event) bool { return !wanted(event.Agent) || visit(event) }, func(line []byte, number int) (bool, error) {
		var agent struct {
			Agent string `json:"agent"`
		}
		if json.Unmarshal(line, &agent) == nil && !wanted(agent.Agent) {
			return true, nil
		}
		event, err := parseLine[Event](line, id, number)
		return err == nil && visit(event), err
	})
}

func (s *Store) scanEvents(id string, held func(Event) bool, scanned func(line []byte, number int) (bool, error)) error {
	if err := namesOneSession(id); err != nil {
		return err
	}
	if events, read := s.parsed[id]; read || s.legacy(id) {
		if !read {
			var err error
			if events, err = s.legacyEvents(id); err != nil {
				return err
			}
		}
		for _, event := range events {
			if !held(event) {
				return nil
			}
		}
		return nil
	}
	file, err := os.Open(s.EventsPath(id))
	if err != nil {
		return fmt.Errorf("session: %s has no events: %w", id, err)
	}
	defer func() { _ = file.Close() }()
	reader, line := bufio.NewReader(file), []byte(nil)
	for number := 0; ; {
		chunk, err := reader.ReadSlice('\n')
		line = append(line[:0], chunk...)
		for errors.Is(err, bufio.ErrBufferFull) {
			chunk, err = reader.ReadSlice('\n')
			line = append(line, chunk...)
		}
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return err
		case len(bytes.TrimSpace(line)) == 0:
			continue
		}
		number++
		if more, err := scanned(line, number); !more {
			return err
		}
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
		if _, logged := os.Stat(s.EventsPath(id)); errors.Is(err, fs.ErrNotExist) && errors.Is(logged, fs.ErrNotExist) {
			continue
		}
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
	raw, err := s.readSettled(filepath.Join(s.dir, headName))
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
		if _, side, _ := s.Side(header.ID); header.ForkedInto == "" && !side {
			return Head{ID: header.ID, Derived: true}, nil
		}
	}
	return Head{}, errors.New("session: nothing under " + s.dir + " is a session to continue")
}

func (s *Store) SetHead(id string) error {
	if id == "" {
		return errors.New("session: the head has to name a session")
	}
	if _, side, err := s.Side(id); side || err != nil {
		return err
	}
	return s.writeWhole(filepath.Join(s.dir, headName), []byte(id+"\n"))
}

func (s *Store) Side(id string) (Header, bool, error) {
	header, err := s.read(id)
	if errors.Is(err, fs.ErrNotExist) {
		return Header{}, false, nil
	}
	if err != nil {
		return Header{}, false, err
	}
	chain := []Header{header}
	root, err := s.read(header.Root)
	if header.Generation > 0 && err == nil {
		chain = append(chain, root)
	} else {
		var ancestors []Header
		ancestors, err = s.Ancestors(id)
		chain = append(chain, ancestors...)
	}
	for _, generation := range chain {
		if generation.Kind == KindSide {
			return generation, true, nil
		}
	}
	return Header{}, false, err
}

func (s *Store) SetAccess(id string, owns []string, preset string) (Header, error) {
	side, found, err := s.Side(id)
	if err == nil && !found {
		err = fmt.Errorf("session: %s is not a side chat, and only a side chat has an access to change", id)
	}
	if err != nil {
		return Header{}, err
	}
	return s.edit(side.ID, func(header *Header) { header.Owns, header.Preset = owns, preset })
}

func (s *Store) SetEffort(id, effort string) (Header, error) {
	return s.edit(id, func(header *Header) { header.Effort = effort })
}

func (s *Store) read(id string) (Header, error) {
	if header, kept := s.headers[id]; kept {
		return header, nil
	}
	if err := namesOneSession(id); err != nil {
		return Header{}, err
	}
	raw, err := s.readSettled(filepath.Join(s.Dir(id), headerName))
	if err == nil {
		var header Header
		if err := json.Unmarshal(raw, &header); err != nil {
			return Header{}, fmt.Errorf("session: the session.json of %s does not parse: %w", id, err)
		}
		if header.CarriedFrom != nil {
			header.Parent = header.CarriedFrom.Session
		}
		if s.headers != nil {
			s.headers[id] = header
		}
		return header, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Header{}, err
	}
	return s.legacyHeader(id)
}
