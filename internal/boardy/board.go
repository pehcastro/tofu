package boardy

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"tofu/internal/sys"
)

const (
	BoardsDirName  = "boards"
	TicketsDirName = "tickets"
	BoardFileName  = "board.toml"
	EventsFileName = "events.jsonl"
)

func Collections() []string { return []string{"epics", "milestones", "sprints"} }

type Board struct {
	Key      string    `json:"key"`
	Name     string    `json:"name"`
	Managers []string  `json:"managers,omitempty"`
	Created  time.Time `json:"created"`
	Dir      string    `json:"dir"`
}

type Store struct {
	Dir string
}

func OpenStore(project string) (Store, error) {
	state, err := sys.ProjectStateDirAt(project)
	return Store{Dir: filepath.Join(state, BoardsDirName)}, err
}

var boardKey = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

func (s Store) BoardDir(key string) string { return filepath.Join(s.Dir, key) }

func (s Store) TicketPath(id string) (string, error) {
	key, _, err := SplitID(id)
	return filepath.Join(s.BoardDir(key), TicketsDirName, id+".md"), err
}

func (s Store) Init(key, name string) (Board, error) {
	if !boardKey.MatchString(key) {
		return Board{}, fmt.Errorf("%q is not a board key: two to ten capital letters or digits, starting with a letter", key)
	}
	board := Board{Key: key, Name: name, Created: time.Now().UTC().Truncate(time.Second), Dir: s.BoardDir(key)}
	err := s.locked(key, "board", func() error {
		if found, err := sys.Exists(filepath.Join(board.Dir, BoardFileName)); found || err != nil {
			return errors.Join(err, fmt.Errorf("the board %s already exists at %s", key, board.Dir))
		}
		for _, dir := range append(Collections(), TicketsDirName) {
			if err := os.MkdirAll(filepath.Join(board.Dir, dir), 0o755); err != nil {
				return err
			}
		}
		return writeAtomic(filepath.Join(board.Dir, BoardFileName), board.toml())
	})
	return board, err
}

func (s Store) Boards() ([]Board, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var boards []Board
	for _, entry := range entries {
		if !entry.IsDir() || !boardKey.MatchString(entry.Name()) {
			continue
		}
		board, readErr := s.Board(entry.Name())
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}
		err = errors.Join(err, readErr)
		boards = append(boards, board)
	}
	return boards, err
}

func (s Store) Board(key string) (Board, error) {
	raw, err := os.ReadFile(filepath.Join(s.BoardDir(key), BoardFileName))
	if err != nil {
		return Board{}, fmt.Errorf("the board %s: %w", key, err)
	}
	board, err := parseBoard(raw)
	board.Dir = s.BoardDir(key)
	if err == nil && board.Key != key {
		err = fmt.Errorf("the board in folder %s says its key is %s", key, board.Key)
	}
	return board, err
}

func (s Store) SaveBoard(board Board) error {
	return s.locked(board.Key, "board", func() error {
		return writeAtomic(filepath.Join(s.BoardDir(board.Key), BoardFileName), board.toml())
	})
}

func (b Board) toml() []byte {
	quote := func(value any) string { raw, _ := json.Marshal(value); return string(raw) }
	return fmt.Appendf(nil, "key = %s\nname = %s\nmanagers = %s\ncreated = %s\n",
		quote(b.Key), quote(b.Name), quote(append([]string{}, b.Managers...)), quote(b.Created.UTC().Format(time.RFC3339)))
}

func parseBoard(raw []byte) (Board, error) {
	var board Board
	var created string
	fields := map[string]any{"key": &board.Key, "name": &board.Name, "managers": &board.Managers, "created": &created}
	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		into, known := fields[key]
		if !found || !known {
			return board, fmt.Errorf("%s line %q is not key = value with a known key", BoardFileName, line)
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(value)), into); err != nil {
			return board, fmt.Errorf("%s %s: %w", BoardFileName, key, err)
		}
	}
	var err error
	board.Created, err = time.Parse(time.RFC3339, created)
	return board, err
}

func (s Store) Get(id string) (Ticket, error) {
	path, err := s.TicketPath(id)
	if err != nil {
		return Ticket{}, err
	}
	return ReadTicket(path)
}

func ReadTicket(path string) (Ticket, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		id := strings.TrimSuffix(filepath.Base(path), ".md")
		key, _, _ := SplitID(id)
		return Ticket{}, fmt.Errorf("no ticket %s on %s", id, key)
	}
	if err != nil {
		return Ticket{}, err
	}
	ticket, err := ParseTicket(raw)
	if err != nil {
		return ticket, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if want := strings.TrimSuffix(filepath.Base(path), ".md"); ticket.ID != want {
		return ticket, fmt.Errorf("%s: its front matter says id %s", filepath.Base(path), ticket.ID)
	}
	return ticket, nil
}

func (s Store) TicketFiles(key string) ([]string, error) {
	dir := filepath.Join(s.BoardDir(key), TicketsDirName)
	names, err := sys.ListFiles(dir, ".md")
	paths := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.HasPrefix(name, ".") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	slices.SortFunc(paths, func(a, b string) int { return ticketNumber(a) - ticketNumber(b) })
	return paths, err
}

func ticketNumber(path string) int {
	_, number, _ := SplitID(strings.TrimSuffix(filepath.Base(path), ".md"))
	return number
}

func (s Store) Tickets(key string) ([]Ticket, error) {
	if _, err := s.Board(key); err != nil {
		return nil, err
	}
	paths, err := s.TicketFiles(key)
	tickets := make([]Ticket, 0, len(paths))
	for _, path := range paths {
		ticket, readErr := ReadTicket(path)
		if readErr != nil {
			err = errors.Join(err, readErr)
			continue
		}
		tickets = append(tickets, ticket)
	}
	return tickets, err
}

func (s Store) Create(key string, ticket Ticket) (Ticket, error) {
	if _, err := s.Board(key); err != nil {
		return Ticket{}, err
	}
	number, err := s.next(key)
	if err != nil {
		return Ticket{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	ticket.ID, ticket.Created, ticket.Updated = fmt.Sprintf("%s-%d", key, number), now, now
	ticket.Status, ticket.Type, ticket.Priority = cmp.Or(ticket.Status, Backlog), cmp.Or(ticket.Type, Task), cmp.Or(ticket.Priority, DefaultPriority)
	path, _ := s.TicketPath(ticket.ID)
	err = s.locked(key, ticket.ID, func() error {
		if found, err := sys.Exists(path); found || err != nil {
			return errors.Join(err, fmt.Errorf("%s already exists, and the counter of %s is behind its tickets", ticket.ID, key))
		}
		return writeAtomic(path, ticket.Markdown())
	})
	return ticket, err
}

func (s Store) Edit(id string, edit func(*Ticket) error) (Ticket, error) {
	key, _, err := SplitID(id)
	if err != nil {
		return Ticket{}, err
	}
	var ticket Ticket
	err = s.locked(key, id, func() error {
		path, _ := s.TicketPath(id)
		if ticket, err = ReadTicket(path); err != nil {
			return err
		}
		if err := edit(&ticket); err != nil {
			return err
		}
		ticket.ID, ticket.Updated = id, time.Now().UTC().Truncate(time.Second)
		return writeAtomic(path, ticket.Markdown())
	})
	return ticket, err
}
