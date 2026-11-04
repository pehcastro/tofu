package boardy

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/agent/subagents"
)

func frontOf(path string) (Front, error) {
	file, err := os.Open(path)
	if err != nil {
		return Front{}, err
	}
	defer func() { _ = file.Close() }()
	lines := bufio.NewScanner(file)
	opened := lines.Scan() && strings.TrimSuffix(lines.Text(), "\r") == "---"
	var front strings.Builder
	for opened && lines.Scan() {
		line := strings.TrimSuffix(lines.Text(), "\r")
		if line != "---" {
			front.WriteString(line + "\n")
			continue
		}
		var ticket Ticket
		if err := ticket.readFront(front.String()); err != nil {
			return Front{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		return ticket.Front, namedFor(path, ticket.ID)
	}
	return Front{}, errors.Join(lines.Err(), fmt.Errorf("%s: no front matter between two --- lines", filepath.Base(path)))
}

func namedFor(path, id string) error {
	if want := strings.TrimSuffix(filepath.Base(path), ".md"); id != want {
		return fmt.Errorf("%s: its front matter says id %s", filepath.Base(path), id)
	}
	if ref, _ := SplitID(id); filepath.Base(filepath.Dir(path)) != cmp.Or(ref.Parent(), TicketsDirName) {
		return fmt.Errorf("%s: a ticket lives in %s, and a sub-ticket in the folder of its parent", path, TicketsDirName)
	}
	return nil
}

func (s Store) Fronts(key string) ([]Front, error) {
	paths, err := s.TicketFiles(key)
	fronts := make([]Front, 0, len(paths))
	for _, path := range paths {
		front, readErr := frontOf(path)
		if readErr != nil {
			err = errors.Join(err, readErr)
			continue
		}
		fronts = append(fronts, front)
	}
	return fronts, err
}

type Hold struct {
	Path   string `json:"path"`
	Held   bool   `json:"held"`
	Ticket string `json:"ticket,omitempty"`
	Status Status `json:"status,omitempty"`
	Glob   string `json:"glob,omitempty"`
}

func (s Store) Held(path string) (Hold, error) {
	target := heldGlob(path)
	boards, err := s.Boards()
	for _, board := range boards {
		fronts, frontsErr := s.Fronts(board.Key)
		err = errors.Join(err, frontsErr)
		for _, front := range fronts {
			if !board.Holding(front.Status) {
				continue
			}
			for _, glob := range front.Owns {
				matched, matchErr := subagents.Matches(target, []string{heldGlob(glob)})
				if matched {
					return Hold{Path: path, Held: true, Ticket: front.ID, Status: front.Status, Glob: glob}, err
				}
				err = errors.Join(err, matchErr)
			}
		}
	}
	return Hold{Path: path}, err
}
