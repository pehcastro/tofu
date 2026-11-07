package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	outlineEndsAtTask  = "task"
	outlineEndsAtTurns = "turns"
)

type Outlined struct {
	Family
	Touched time.Time
	Working bool
}

func (s *Store) Outline() ([]Outlined, []Skip, error) {
	listing, err := s.listingBy(s.outlineOf, func(_ string, header Header) Header { return header })
	if err != nil {
		return nil, nil, err
	}
	families := listing.Families()
	turn, turning, _ := heldLock(filepath.Join(s.State(), turnLockName))
	outlined, holders := make([]Outlined, len(families)), make([]int, len(families))
	inParallel(len(families), func(i int) {
		head := families[i].Head()
		outlined[i] = Outlined{Family: families[i], Touched: head.LastAt()}
		if info, err := os.Stat(s.EventsPath(head.ID)); err == nil && info.ModTime().After(outlined[i].Touched) {
			outlined[i].Touched = info.ModTime()
		}
		if !turning {
			return
		}
		if holder, held, _ := heldLock(filepath.Join(s.Dir(head.ID), lockName)); held {
			holders[i] = holder.PID
		}
	})
	working := -1
	for i, pid := range holders {
		if pid != 0 && pid == turn.PID && (working < 0 || outlined[i].Touched.After(outlined[working].Touched)) {
			working = i
		}
	}
	if working >= 0 {
		outlined[working].Working = true
	}
	slices.SortStableFunc(outlined, func(a, b Outlined) int { return b.Touched.Compare(a.Touched) })
	return outlined, listing.Skipped, nil
}

func (s *Store) outlineOf(entry sessionEntry) (Header, error) {
	if entry.singleFile || namesOneSession(entry.id) != nil {
		return s.readEntry(entry)
	}
	var file *os.File
	err := settled(func() (err error) {
		file, err = os.Open(filepath.Join(s.Dir(entry.id), headerName))
		return err
	})
	if errors.Is(err, fs.ErrNotExist) {
		return s.legacyHeader(entry.id)
	}
	if err != nil {
		return Header{}, err
	}
	defer func() { _ = file.Close() }()
	var header Header
	if err := readOutline(file, &header); err != nil {
		return Header{}, fmt.Errorf("session: the session.json of %s does not parse: %w", entry.id, err)
	}
	if header.CarriedFrom != nil {
		header.Parent = header.CarriedFrom.Session
	}
	return header, nil
}

func readOutline(from io.Reader, into *Header) error {
	decoder := json.NewDecoder(from)
	if open, err := decoder.Token(); err != nil || open != json.Delim('{') {
		return errors.Join(err, fmt.Errorf("it holds %v where an object belongs", open))
	}
	kept := []byte{'{'}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		if key == outlineEndsAtTask || key == outlineEndsAtTurns {
			break
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		quoted, _ := json.Marshal(key)
		if len(kept) > 1 {
			kept = append(kept, ',')
		}
		kept = append(append(append(kept, quoted...), ':'), value...)
	}
	return json.Unmarshal(append(kept, '}'), into)
}
