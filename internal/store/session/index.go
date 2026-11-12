package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	indexName = "index.jsonl"
	partsName = "parts.jsonl"
)

type brief struct {
	Name         *string   `json:"name,omitempty"`
	At           time.Time `json:"started_at"`
	Parent       string    `json:"parent,omitempty"`
	Root         string    `json:"root,omitempty"`
	Generation   int       `json:"generation,omitempty"`
	BranchedFrom *Carried  `json:"branched_from,omitempty"`
	Kind         Kind      `json:"kind,omitempty"`
	ForkedInto   string    `json:"forked_into,omitempty"`
}

type indexLine struct {
	ID         string    `json:"id"`
	At         fileStamp `json:"at,omitzero"`
	Brief      *brief    `json:"brief,omitempty"`
	Checked    bool      `json:"checked,omitempty"`
	Events     fileStamp `json:"events,omitzero"`
	Steps      int       `json:"steps,omitempty"`
	Last       time.Time `json:"last,omitzero"`
	Unreadable string    `json:"unreadable,omitempty"`
}

type partsLine struct {
	ID     string    `json:"id"`
	Events fileStamp `json:"events"`
	Parts  []string  `json:"parts"`
}

func briefOf(header Header) *brief {
	return &brief{Name: header.Name, At: header.At, Parent: header.Parent, Root: header.Root, Generation: header.Generation,
		BranchedFrom: header.BranchedFrom, Kind: header.Kind, ForkedInto: header.ForkedInto}
}

func (b brief) header(id string) Header {
	return Header{ID: id, Name: b.Name, At: b.At, Parent: b.Parent, Root: b.Root, Generation: b.Generation, BranchedFrom: b.BranchedFrom, Kind: b.Kind, ForkedInto: b.ForkedInto}
}

func decodedLines[T any](s *Store, name string, keep func(T)) int {
	raw, err := s.readFile(filepath.Join(s.dir, name))
	if err != nil {
		return 0
	}
	lines := 0
	for line := range bytes.Lines(raw) {
		var read T
		if json.Unmarshal(line, &read) == nil {
			lines++
			keep(read)
		}
	}
	return lines
}

func jsonLines[T any](lines []T) []byte {
	var body []byte
	for _, line := range lines {
		body = append(append(body, marshalled(line)...), '\n')
	}
	return body
}

func (s *Store) appendIndex(line indexLine) error {
	return appendLines(filepath.Join(s.dir, indexName), jsonLines([]indexLine{line}))
}

func (s *Store) Briefed(check func(id string) error) (*Store, Listing, error) {
	held := map[string]indexLine{}
	lines := decodedLines(s, indexName, func(read indexLine) {
		kept := held[read.ID]
		if read.Brief != nil {
			kept.At, kept.Brief = read.At, read.Brief
		}
		if read.Checked {
			kept.Checked, kept.Events, kept.Steps, kept.Last, kept.Unreadable = true, read.Events, read.Steps, read.Last, read.Unreadable
		}
		kept.ID = read.ID
		held[read.ID] = kept
	})
	var mu sync.Mutex
	current, refreshed := map[string]indexLine{}, map[string]bool{}
	listing, err := s.listingBy(func(entry sessionEntry) (Header, error) {
		line, read := held[entry.id], false
		if at := s.headerStamp(entry); !at.found() || line.At != at || line.Brief == nil {
			header, err := s.readEntry(entry)
			if err != nil {
				return header, err
			}
			line.At, line.Brief, read = at, briefOf(header), true
		}
		line.ID = entry.id
		mu.Lock()
		if current[entry.id] = line; read {
			refreshed[entry.id] = true
		}
		mu.Unlock()
		return line.Brief.header(entry.id), nil
	}, func(_ string, header Header) Header { return header })
	if err != nil {
		return nil, Listing{}, err
	}
	var unchecked []indexLine
	for _, family := range listing.Families() {
		if line := current[family.Head().ID]; !line.Checked || line.Events != s.eventsStamp(line.ID) {
			unchecked = append(unchecked, line)
		}
	}
	inParallel(len(unchecked), func(i int) { unchecked[i] = s.checked(unchecked[i], check) })
	for _, line := range unchecked {
		current[line.ID], refreshed[line.ID] = line, true
	}
	if lines+len(refreshed)-len(current) > len(current) {
		_ = s.writeWhole(filepath.Join(s.dir, indexName), jsonLines(slices.Collect(maps.Values(current))))
	} else if len(refreshed) > 0 {
		var appended []indexLine
		for id := range refreshed {
			appended = append(appended, current[id])
		}
		_ = appendLines(filepath.Join(s.dir, indexName), jsonLines(appended))
	}
	briefed := *s
	briefed.briefs = current
	return &briefed, listing, nil
}

func (s *Store) Steps(id string) (int, time.Time, error) {
	line := s.briefs[id]
	if line.Unreadable != "" {
		return 0, time.Time{}, errors.New(line.Unreadable)
	}
	return line.Steps, line.Last, nil
}

func (s *Store) checked(line indexLine, check func(id string) error) indexLine {
	line.Checked, line.Events, line.Unreadable = true, s.eventsStamp(line.ID), ""
	err := check(line.ID)
	if err == nil {
		line.Steps, line.Last, err = s.countSteps(line.ID)
	}
	if err != nil {
		line.Steps, line.Last, line.Unreadable = 0, time.Time{}, err.Error()
	}
	return line
}

func (s *Store) countSteps(id string) (steps int, last time.Time, err error) {
	err = s.EachEventHead(id, func(head EventHead) {
		if head.Agent == "" && (head.Kind == EventRequest || head.Kind == EventStep) {
			steps++
		}
		if head.At.After(last) {
			last = head.At
		}
	})
	return steps, last, err
}

func (s *Store) headerStamp(entry sessionEntry) fileStamp {
	single := filepath.Join(s.dir, entry.id+legacySuffix)
	if entry.singleFile {
		return stampOf(single)
	}
	return firstStamp(filepath.Join(s.Dir(entry.id), headerName), filepath.Join(s.Dir(entry.id), legacyHeaderName), single)
}

func (s *Store) eventsStamp(id string) fileStamp {
	return firstStamp(s.EventsPath(id), filepath.Join(s.Dir(id), legacyBodyName), filepath.Join(s.dir, id+legacySuffix))
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
	held := map[string]partsLine{}
	decodedLines(s, partsName, func(read partsLine) { held[read.ID] = read })
	var read []partsLine
	defer func() {
		if len(read) > 0 {
			_ = appendLines(filepath.Join(s.dir, partsName), jsonLines(read))
		}
	}()
	for _, header := range listing.Sessions {
		line := held[header.ID]
		if at := s.eventsStamp(header.ID); !at.found() || line.Events != at {
			parts, err := s.partsOf(header.ID)
			if err != nil {
				continue
			}
			line = partsLine{ID: header.ID, Events: at, Parts: parts}
			if at.found() && header.Ended() {
				read = append(read, line)
			}
		}
		if slices.Contains(line.Parts, id) {
			return header.ID, id
		}
	}
	return id, ""
}

func (s *Store) partsOf(id string) ([]string, error) {
	var parts []string
	err := s.EachEventHead(id, func(head EventHead) {
		for _, part := range []string{head.Turn, head.Agent} {
			if part != "" && !slices.Contains(parts, part) {
				parts = append(parts, part)
			}
		}
	})
	return parts, err
}
