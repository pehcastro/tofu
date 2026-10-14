package guard

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/boardy"
	"tofu/internal/sys"
)

type place int

const (
	placeOther place = iota
	placeOpen
	placeDoing
	placeReview
	placeClosed
)

type ticket struct {
	ID         string
	Status     string
	Place      place
	Owns       []string
	Owner      string
	Acceptance bool
	HasLog     bool
	Log        string
}

type ticketSource interface {
	list(places ...place) ([]ticket, error)
	find(id string) (ticket, bool, error)
	where(id string) string
}

func ticketsAt(env Env) ticketSource {
	if env.Boardy {
		store, err := boardy.OpenStore(env.Root)
		if err == nil {
			if boards, err := store.Boards(); err == nil && len(boards) > 0 {
				return boardTickets{store: store, boards: boards}
			}
		}
	}
	return folderTickets{dir: filepath.Join(env.Root, ".local", "boji", "tickets")}
}

type folderTickets struct{ dir string }

type ticketFolder struct {
	name  string
	place place
}

var ticketFolders = []ticketFolder{{"open", placeOpen}, {"doing", placeDoing}, {"review", placeReview}, {"done", placeClosed}, {"dropped", placeClosed}}

func (f folderTickets) list(places ...place) ([]ticket, error) {
	var tickets []ticket
	for _, folder := range ticketFolders {
		if !slices.Contains(places, folder.place) {
			continue
		}
		entries, _ := os.ReadDir(filepath.Join(f.dir, folder.name))
		for _, entry := range entries {
			if !entry.IsDir() && foldSuffix(entry.Name(), ".md") {
				tickets = append(tickets, readFolderTicket(filepath.Join(f.dir, folder.name, entry.Name())))
			}
		}
	}
	return tickets, nil
}

func (f folderTickets) find(id string) (ticket, bool, error) {
	var found string
	_ = filepath.WalkDir(f.dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.EqualFold(entry.Name(), id+".md") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return ticket{}, false, nil
	}
	return readFolderTicket(found), true, nil
}

func (folderTickets) where(string) string { return ".local/boji/tickets/" }

var (
	acceptanceHeading = sys.LazyRegexp(`(?im)^##\s*Acceptance`)
	ownerField        = sys.LazyRegexp(`(?im)^owner:\s*(\S+)\s*$`)
)

func readFolderTicket(path string) ticket {
	data, _ := os.ReadFile(path)
	text := strings.TrimPrefix(string(data), byteOrderMark)
	found := ticket{
		ID:         strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Status:     filepath.Base(filepath.Dir(path)),
		Owns:       ownsGlobs(newline().Split(text, -1)),
		Acceptance: acceptanceHeading().MatchString(text),
	}
	if at := slices.IndexFunc(ticketFolders, func(known ticketFolder) bool { return strings.EqualFold(known.name, found.Status) }); at >= 0 {
		found.Place = ticketFolders[at].place
	}
	if owner := ownerField().FindStringSubmatch(text); owner != nil {
		found.Owner = owner[1]
	}
	if at := strings.Index(text, "\n## Log"); at >= 0 {
		found.HasLog, found.Log = true, strings.TrimSpace(text[at+len("\n## Log"):])
	}
	return found
}

type boardTickets struct {
	store  boardy.Store
	boards []boardy.Board
}

func (b boardTickets) list(places ...place) ([]ticket, error) {
	var tickets []ticket
	var err error
	for _, board := range b.boards {
		fronts, readErr := b.store.Fronts(board.Key)
		err = errors.Join(err, readErr)
		for _, front := range fronts {
			if found := frontTicket(front); slices.Contains(places, found.Place) {
				tickets = append(tickets, found)
			}
		}
	}
	return tickets, err
}

func (b boardTickets) find(id string) (ticket, bool, error) {
	path, err := b.store.TicketPath(id)
	if _, statErr := os.Stat(path); err != nil || errors.Is(statErr, fs.ErrNotExist) {
		return ticket{}, false, nil
	}
	full, err := b.store.Get(id)
	if err != nil {
		return ticket{}, false, err
	}
	found := frontTicket(full.Front)
	found.Acceptance = slices.ContainsFunc(full.Acceptance, func(line string) bool { return !blank(line) })
	found.HasLog, found.Log = true, strings.TrimSpace(full.Log)
	return found, true, nil
}

func (boardTickets) where(id string) string {
	key, _, _ := strings.Cut(id, "-")
	return "the " + strings.ToUpper(key) + " board"
}

func frontTicket(front boardy.Front) ticket {
	found := ticket{ID: front.ID, Status: string(front.Status), Owns: front.Owns, Owner: front.Assignee}
	switch front.Status {
	case boardy.Backlog, boardy.Todo:
		found.Place = placeOpen
	case boardy.Doing:
		found.Place = placeDoing
	case boardy.Review:
		found.Place = placeReview
	case boardy.Done, boardy.Dropped:
		found.Place = placeClosed
	case boardy.Triage, boardy.Blocked:
		found.Place = placeOther
	}
	return found
}
