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
	IDPrefix         = "turn-"
	headerName       = "header.json"
	bodyName         = "body.jsonl"
	headName         = "HEAD"
	singleFileSuffix = ".json"
)

type Store struct {
	dir      string
	settings Settings
	readFile func(path string) ([]byte, error)
	rename   func(from, to string) error
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, settings: DefaultSettings(), readFile: os.ReadFile, rename: os.Rename}
}

func (s *Store) Use(settings Settings) { s.settings = settings }

func Open() (*Store, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return nil, err
	}
	return NewStore(filepath.Join(state, "sessions")), nil
}

func (s *Store) Write(header Header, events []Event) error {
	if header.ID == "" {
		return errors.New("session: a record with no id has nowhere to be written")
	}
	if header.Root == "" {
		return errors.New("session: " + header.ID + " names no root, and the first session of a lineage is its own root")
	}
	header.Schema = SchemaVersion
	if header.Name == nil {
		header.Name = s.keptOrNewName(header.ID)
	}
	dir := filepath.Join(s.dir, header.ID)
	recorded, err := s.settings.withReads(events)
	if err != nil {
		return err
	}
	if err := appendEvents(filepath.Join(dir, bodyName), recorded); err != nil {
		return err
	}
	body, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return err
	}
	return s.writeWhole(filepath.Join(dir, headerName), body)
}

func appendEvents(path string, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	var lines bytes.Buffer
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		lines.Write(line)
		lines.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, written := file.Write(lines.Bytes())
	return cmp.Or(written, file.Close())
}

func (s *Store) writeWhole(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".boji-*")
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

func (s *Store) keptOrNewName(id string) *string {
	if kept, err := s.read(id); err == nil && kept.Name != nil {
		return kept.Name
	}
	name := newName()
	return &name
}

func (s *Store) SetName(id, given string) (Header, error) {
	name, err := slugOf(given)
	if err != nil {
		return Header{}, err
	}
	header, err := s.Header(id)
	if err != nil {
		return Header{}, err
	}
	header.Name = &name
	return header, s.Write(header, nil)
}

func (s *Store) End(id string, reason EndReason, at time.Time) (Header, error) {
	switch reason {
	case EndedByNew, EndedByClose, EndedByFork:
	default:
		return Header{}, fmt.Errorf("session: %q is not a reason a session ends", string(reason))
	}
	header, err := s.Header(id)
	if err != nil {
		return Header{}, err
	}
	if header.Ended() {
		return header, nil
	}
	header.EndedAt, header.EndReason = &at, reason
	return header, s.Write(header, nil)
}

func (s *Store) Resolve(handle string) ([]Header, error) {
	if handle == "" {
		return nil, errors.New("session: which session? an id or a name names one")
	}
	for _, id := range []string{handle, IDPrefix + handle} {
		if header, err := s.Header(id); err == nil {
			return []Header{header}, nil
		}
	}
	listing, err := s.Listing()
	if err != nil {
		return nil, err
	}
	var named []Header
	for _, header := range listing.Sessions {
		if header.Name != nil && *header.Name == handle {
			named = append(named, header)
		}
	}
	if len(named) == 0 {
		return nil, fmt.Errorf("session: nothing here is called %s: %w", handle, fs.ErrNotExist)
	}
	return named, nil
}

func (s *Store) Header(id string) (Header, error) {
	header, err := s.read(id)
	if err != nil {
		return Header{}, err
	}
	if header.Root != "" {
		return header, nil
	}
	lineage, err := s.Lineage(id)
	if err != nil {
		return Header{}, err
	}
	return lineage[len(lineage)-1], nil
}

func (s *Store) Body(id string) ([]Event, error) {
	raw, err := s.readFile(filepath.Join(s.dir, id, bodyName))
	if err != nil {
		_, events, singleErr := s.singleFile(id)
		if singleErr != nil {
			return nil, fmt.Errorf("session: %s has no body in either shape: %w", id, errors.Join(err, singleErr))
		}
		return events, nil
	}
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
			return nil, fmt.Errorf("session: line %d of the body of %s does not parse: %w", len(events)+1, id, err)
		}
		events = append(events, event)
	}
}

func (s *Store) Lineage(id string) ([]Header, error) {
	walked := map[string]bool{}
	var chain []Header
	for id != "" {
		if walked[id] {
			return nil, fmt.Errorf("session: the lineage through %s comes back to itself", id)
		}
		walked[id] = true
		header, err := s.read(id)
		if err != nil {
			return nil, err
		}
		chain = append(chain, header)
		id = header.Parent
	}
	slices.Reverse(chain)
	for i := range chain {
		if chain[i].Root == "" {
			chain[i].Root = chain[0].ID
		}
	}
	return chain, nil
}

func (s *Store) Listing() (Listing, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Listing{}, nil
	}
	if err != nil {
		return Listing{}, err
	}
	listed := map[string]bool{}
	var listing Listing
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() {
			if filepath.Ext(id) != singleFileSuffix {
				continue
			}
			id = strings.TrimSuffix(id, singleFileSuffix)
		}
		if listed[id] {
			continue
		}
		listed[id] = true
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
	raw, err := s.readFile(filepath.Join(s.dir, id, headerName))
	if err == nil {
		var header Header
		if err := json.Unmarshal(raw, &header); err != nil {
			return Header{}, fmt.Errorf("session: the header of %s does not parse: %w", id, err)
		}
		return header, nil
	}
	header, _, singleErr := s.singleFile(id)
	if singleErr == nil {
		return header, nil
	}
	if errors.Is(singleErr, fs.ErrNotExist) {
		return Header{}, fmt.Errorf("session: %s reads as neither shape: %w", id, errors.Join(err, singleErr))
	}
	return Header{}, singleErr
}

type singleFileRow struct {
	ID           string            `json:"id"`
	At           time.Time         `json:"at"`
	Task         string            `json:"task"`
	Model        string            `json:"model"`
	ForkedFrom   string            `json:"forked_from"`
	ForkedInto   string            `json:"forked_into"`
	ForkKind     string            `json:"fork_kind"`
	Outcome      json.RawMessage   `json:"outcome"`
	TotalCostUSD float64           `json:"total_cost_usd"`
	Steps        []json.RawMessage `json:"steps"`
}

func outcomeName(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name, nil
	}
	var ordinal int
	if err := json.Unmarshal(raw, &ordinal); err != nil {
		return "", fmt.Errorf("the outcome %s is neither a name nor the number an earlier schema wrote", raw)
	}
	if ordinal < int(OutcomeUnset) || ordinal >= int(outcomeCount) {
		return "", fmt.Errorf("the outcome %d is not one this build has a name for", ordinal)
	}
	return Outcome(ordinal).String(), nil
}

func (s *Store) singleFile(id string) (Header, []Event, error) {
	raw, err := s.readFile(filepath.Join(s.dir, id+singleFileSuffix))
	if err != nil {
		return Header{}, nil, err
	}
	var row singleFileRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return Header{}, nil, fmt.Errorf("session: %s is not a turn row: %w", id, err)
	}
	outcome, err := outcomeName(row.Outcome)
	if err != nil {
		return Header{}, nil, fmt.Errorf("session: %s: %w", id, err)
	}
	header := Header{
		ID:         cmp.Or(row.ID, id),
		At:         row.At,
		Task:       row.Task,
		Model:      row.Model,
		Parent:     row.ForkedFrom,
		ForkedInto: row.ForkedInto,
		ForkKind:   row.ForkKind,
		Outcome:    outcome,
		CostUSD:    row.TotalCostUSD,
	}
	if header.Parent == "" {
		header.Root = header.ID
	}
	events := make([]Event, 0, len(row.Steps)+1)
	for _, step := range row.Steps {
		events = append(events, Event{Kind: EventStep, Body: step})
	}
	return header, append(events, Event{Kind: EventOutcome, Body: raw}), nil
}
