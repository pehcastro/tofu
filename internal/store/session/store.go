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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/credential"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
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
	redactor func() credential.KeyRedactor
	recent   *Recent
	briefs   map[string]indexLine
}

type Recent struct {
	mu      sync.Mutex
	kept    []*recalled
	lineage map[string]bool
	walked  map[string]bool
}

type recalled struct {
	id       string
	events   fileStamp
	parsed   []Event
	blobs    fileStamp
	settings Settings
	said     *Reading
	steps    int
}

type fileStamp struct {
	Size int64 `json:"size"`
	Mod  int64 `json:"mod"`
}

func (f fileStamp) found() bool { return f.Mod != 0 }

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{Size: info.Size(), Mod: info.ModTime().UnixNano()}
}

func firstStamp(paths ...string) fileStamp {
	for _, path := range paths {
		if at := stampOf(path); at.found() {
			return at
		}
	}
	return fileStamp{}
}

func (s *Store) Recalls(recent *Recent) { s.recent = recent }

func (r *Recent) slot(id string) *recalled {
	at := slices.IndexFunc(r.kept, func(kept *recalled) bool { return kept.id == id })
	if at < 0 {
		return nil
	}
	kept := r.kept[at]
	r.kept = slices.Insert(slices.Delete(r.kept, at, at+1), 0, kept)
	return kept
}

func (r *Recent) recall(id string, at fileStamp) ([]Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kept := r.slot(id); kept != nil && at.found() && kept.events == at {
		return kept.parsed, true
	}
	return nil, false
}

func (r *Recent) keep(id string, at fileStamp, events []Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kept = slices.Insert(slices.DeleteFunc(r.kept, func(kept *recalled) bool { return kept.id == id }), 0, &recalled{id: id, events: at, parsed: events})
	r.prune()
}

func (r *Recent) holdLineage(id string, ancestors []Header, read []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	walkedBack := r.walked[id]
	r.walked, r.lineage = map[string]bool{}, map[string]bool{}
	for _, ancestor := range ancestors {
		r.walked[ancestor.ID] = true
	}
	for _, held := range read {
		r.lineage[held] = !walkedBack
	}
	r.prune()
}

func (r *Recent) prune() {
	held := r.kept[:0]
	for at, kept := range r.kept {
		if at < konst.RecalledNewestGenerations || r.lineage[kept.id] {
			held = append(held, kept)
		}
	}
	clear(r.kept[len(held):])
	r.kept = held
}

func (r *Recent) saidOf(id string, events []Event, blobs fileStamp, settings Settings) (Reading, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.slot(id)
	if kept == nil || kept.said == nil || kept.blobs != blobs || kept.settings != settings || !sameEvents(kept.parsed, events) {
		return Reading{}, 0, false
	}
	return *kept.said, kept.steps, true
}

func (r *Recent) keepSaid(id string, events []Event, blobs fileStamp, settings Settings, said Reading, steps int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kept := r.slot(id); kept != nil && sameEvents(kept.parsed, events) {
		kept.blobs, kept.settings, kept.said, kept.steps = blobs, settings, &said, steps
	}
}

func sameEvents(a, b []Event) bool {
	return len(a) > 0 && len(a) == len(b) && &a[0] == &b[0]
}

func (s *Store) ReadOnce() *Store {
	once := *s
	once.parsed = map[string][]Event{}
	return &once
}

func (s *Store) ForgetRead() { s.parsed = nil }

func NewStore(dir string) *Store {
	return &Store{dir: dir, settings: DefaultSettings(), readFile: os.ReadFile, rename: os.Rename, redactor: sync.OnceValue(credential.LoadKeyRedactor)}
}

func (s *Store) Use(settings Settings) { s.settings = settings }

func (s *Store) Redacts(redactor func() credential.KeyRedactor) { s.redactor = redactor }

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
	state, err := platformproject.ProjectStateDirAt(project)
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

func (s *Store) writeHeader(header Header, redact credential.KeyRedactor) (indexLine, error) {
	body, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return indexLine{}, err
	}
	redacted, path := []byte(redact.Redact(string(body))), filepath.Join(s.Dir(header.ID), headerName)
	if err := s.writeWhole(path, redacted); err != nil {
		return indexLine{}, err
	}
	return indexLine{ID: header.ID, At: stampOf(path), Brief: briefOf(header)}, nil
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
	line, err := s.writeHeader(header, s.redactor())
	if err == nil {
		err = s.appendIndex(line)
	}
	return header, err
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

const HeadHandle = "head"

func (s *Store) Resolve(handle string) ([]Header, error) {
	if handle == "" {
		return nil, errors.New("session: which session? an id or a name names one")
	}
	if handle == HeadHandle {
		head, err := s.Head()
		if err != nil {
			return nil, err
		}
		header, err := s.read(head.ID)
		return []Header{header}, err
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
	read := []string{id}
	for _, header := range slices.Backward(append([]Header{head}, ancestors...)) {
		generation := Generation{Header: header, Unread: true}
		if header.ID != id {
			generation.readFor = needed(header)
		}
		if generation.readFor != "" {
			read = append(read, header.ID)
		}
		lineage = append(lineage, generation)
	}
	if s.recent != nil {
		s.recent.holdLineage(id, ancestors, read)
	}
	for i := range lineage {
		generation := &lineage[i]
		if generation.Header.ID != id && generation.readFor == "" {
			continue
		}
		events, err := s.Events(generation.Header.ID)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		generation.Events, generation.Unread = events, err != nil
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
	path := filepath.Join(s.Dir(id), eventsName)
	var at fileStamp
	if s.recent != nil {
		at = stampOf(path)
		if events, found := s.recent.recall(id, at); found {
			return events, nil
		}
	}
	raw, err := s.readFile(path)
	if err != nil {
		return nil, fmt.Errorf("session: %s has no events: %w", id, err)
	}
	events, err := parseEvents(raw, id)
	if err == nil && at.found() {
		s.recent.keep(id, at, events)
	}
	return events, err
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

func (s *Store) ReadSaid(id string, events []Event) (Reading, int, error) {
	var blobs fileStamp
	if s.recent != nil {
		blobs = stampOf(filepath.Join(s.Dir(id), blobsName))
		if reading, steps, found := s.recent.saidOf(id, events, blobs, s.settings); found {
			return reading, steps, nil
		}
	}
	said, steps, err := s.Said(id, events)
	reading, unread := ReadEvents(said)
	if err = cmp.Or(err, unread); err == nil && s.recent != nil {
		s.recent.keepSaid(id, events, blobs, s.settings, reading, steps)
	}
	return reading, steps, err
}

func (s *Store) Said(id string, events []Event) (said []Event, steps int, err error) {
	kept := partOf(events, "")
	changed := -1
	for i, event := range kept {
		switch event.Kind {
		case EventRequest, EventStep:
			steps++
		case EventListChange:
			changed = i
		}
	}
	var carried []Event
	if changed >= 0 {
		if carried, err = s.Carried(id, kept[changed]); err != nil {
			return nil, 0, err
		}
		kept = kept[changed+1:]
	}
	if said, err = s.settings.shown(kept, withoutSteps, s.crashed); err != nil {
		return nil, 0, err
	}
	err = s.WithPictureData(id, said)
	return append(carried, said...), steps, err
}

func (s *Store) WithPictureData(id string, said []Event) error {
	pictured := map[int]MessageBody{}
	var wanted []string
	for i, event := range said {
		var message MessageBody
		if event.Kind == EventMessage && bytes.Contains(event.Body, []byte(`"pictures":`)) && json.Unmarshal(event.Body, &message) == nil {
			pictured[i] = message
			for _, picture := range message.Pictures {
				wanted = append(wanted, picture.Blob)
			}
		}
	}
	blobs, err := s.blobsNamed(id, wanted)
	for i, message := range pictured {
		for p, picture := range message.Pictures {
			if held, found := blobs[picture.Blob]; found && err == nil {
				err = json.Unmarshal(held, &message.Pictures[p].Data)
			}
		}
		said[i].Body = marshalled(message)
	}
	return err
}

func (s *Store) Carried(id string, change Event) ([]Event, error) {
	var body ListChangeBody
	if err := json.Unmarshal(change.Body, &body); err != nil {
		return nil, fmt.Errorf("the list change %s does not parse: %w", change.ID, err)
	}
	if body.Kind == ListChangeFork {
		return nil, nil
	}
	blobs, err := s.blobsNamed(id, body.After)
	if err != nil {
		return nil, err
	}
	var carried []Event
	for _, hash := range body.After {
		var message MessageBody
		if err := json.Unmarshal(blobs[hash], &message); err != nil {
			return nil, fmt.Errorf("the message %s that the %s %s kept is not held: %w", hash, body.Kind, change.ID, err)
		}
		if message.Role != RoleSystem {
			carried = append(carried, Event{ID: hash, Author: AuthorOrchestrator, Attempt: FirstAttempt, Kind: EventMessage, Body: blobs[hash]})
		}
	}
	return carried, s.WithPictureData(id, carried)
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
	for number := 1; ; number++ {
		line, rest, terminated := bytes.Cut(raw, []byte{'\n'})
		if !terminated {
			return events, nil
		}
		raw = rest
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		event, err := parseLine[Event](line, id, number)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
}

type kindedLine interface {
	eventKind() EventKind
}

func (e Event) eventKind() EventKind { return e.Kind }

func (e EventHead) eventKind() EventKind { return e.Kind }

func parseLine[T kindedLine](line []byte, id string, number int) (T, error) {
	var parsed T
	if err := json.Unmarshal(line, &parsed); err != nil {
		return parsed, fmt.Errorf("session: line %d of the events of %s does not parse: %w", number, id, err)
	}
	if parsed.eventKind() == "" {
		return parsed, fmt.Errorf("session: line %d of the events of %s has no kind", number, id)
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
		head, err := parseLine[EventHead](line, id, number)
		if err != nil || !wanted(head.Agent) {
			return err == nil, err
		}
		event, err := parseLine[Event](line, id, number)
		return err == nil && visit(event), err
	})
}

func (s *Store) EventsOf(id string, kinds ...EventKind) ([]Event, error) {
	var kept []Event
	keep := func(event Event) bool {
		if slices.Contains(kinds, event.Kind) {
			kept = append(kept, event)
		}
		return true
	}
	err := s.scanEvents(id, keep, func(line []byte, number int) (bool, error) {
		_, fromKind, found := bytes.Cut(line, []byte(`"kind":"`))
		kind, _, _ := bytes.Cut(fromKind, []byte(`"`))
		if !found {
			head, err := parseLine[EventHead](line, id, number)
			if err != nil {
				return false, err
			}
			kind = []byte(head.Kind)
		}
		if !slices.Contains(kinds, EventKind(kind)) {
			return true, nil
		}
		event, err := parseLine[Event](line, id, number)
		return err == nil && keep(event), err
	})
	return kept, err
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
	reader, line := bufio.NewReaderSize(file, bufio.MaxScanTokenSize), []byte(nil)
	for number := 1; ; number++ {
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
		if more, err := scanned(line, number); !more {
			return err
		}
	}
}

func (s *Store) Listing() (Listing, error) {
	return s.listingBy(s.readEntry, func(_ string, header Header) Header { return header })
}

func (s *Store) listingBy(read func(sessionEntry) (Header, error), keep func(id string, header Header) Header) (Listing, error) {
	entries, err := sessionEntries(s.dir)
	if err != nil {
		return Listing{}, err
	}
	headers, failures, gone := make([]Header, len(entries)), make([]error, len(entries)), make([]bool, len(entries))
	inParallel(len(entries), func(i int) {
		headers[i], failures[i] = read(entries[i])
		if errors.Is(failures[i], fs.ErrNotExist) {
			_, logged := os.Stat(s.EventsPath(entries[i].id))
			gone[i] = errors.Is(logged, fs.ErrNotExist)
		}
	})
	var listing Listing
	for i, entry := range entries {
		switch {
		case gone[i]:
		case failures[i] != nil:
			listing.Skipped = append(listing.Skipped, Skip{ID: entry.id, Reason: failures[i]})
		default:
			listing.Sessions = append(listing.Sessions, keep(entry.id, headers[i]))
		}
	}
	slices.SortFunc(listing.Sessions, func(a, b Header) int { return b.At.Compare(a.At) })
	return listing, nil
}

func inParallel(n int, do func(i int)) {
	next := make(chan int)
	var workers sync.WaitGroup
	for range min(max(runtime.GOMAXPROCS(0)/konst.CPUsPerReadWorker, 1), n) {
		workers.Go(func() {
			for i := range next {
				do(i)
			}
		})
	}
	for i := range n {
		next <- i
	}
	close(next)
	workers.Wait()
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
	return Head{}, fmt.Errorf("session: nothing under %s is a session to continue: %w", s.dir, fs.ErrNotExist)
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

func (s *Store) SetModel(id, wire, model string) (Header, error) {
	return s.edit(id, func(header *Header) {
		header.Wire, header.Model, header.Chosen = wire, model, &Chosen{Wire: wire, Model: model}
	})
}

func (s *Store) read(id string) (Header, error) {
	if s.briefs == nil {
		return s.readEntry(sessionEntry{id: id})
	}
	if line, found := s.briefs[id]; found {
		return line.Brief.header(id), nil
	}
	return Header{}, fmt.Errorf("session: %s is not listed here: %w", id, fs.ErrNotExist)
}

func (s *Store) readEntry(entry sessionEntry) (Header, error) {
	id := entry.id
	if err := namesOneSession(id); err != nil {
		return Header{}, err
	}
	if entry.singleFile {
		header, _, err := s.singleFile(id)
		return header, err
	}
	raw, err := s.readSettled(filepath.Join(s.Dir(id), headerName))
	if err == nil {
		return headerOf(raw, id)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Header{}, err
	}
	return s.legacyHeader(id)
}

func headerOf(raw []byte, id string) (Header, error) {
	var header Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return Header{}, fmt.Errorf("session: the session.json of %s does not parse: %w", id, err)
	}
	if header.CarriedFrom != nil {
		header.Parent = header.CarriedFrom.Session
	}
	return header, nil
}
