package memtree

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type Item struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func (i Item) rendered() string { return i.Kind + ": " + i.Text }

type Ref struct {
	Level int `json:"l"`
	Index int `json:"i"`
}

func (r Ref) ID() int   { return r.Index << r.Level }
func (r Ref) Span() int { return 1 << r.Level }

type node struct {
	Ref
	Text string `json:"text"`
}

type savedView struct {
	T       int   `json:"t"`
	Merging bool  `json:"merging"`
	Lines   []Ref `json:"lines"`
}

type Store struct {
	base  string
	lock  *os.File
	log   *os.File
	tree  *os.File
	items []Item
	nodes map[Ref]string
	view  savedView
	ready []Ref
}

func Open(logPath string) (*Store, error) {
	base := strings.TrimSuffix(logPath, ".jsonl")
	lock, err := takeLock(base + ".lock")
	if err != nil {
		return nil, err
	}
	s := &Store{base: base, lock: lock, nodes: map[Ref]string{}}
	if err := s.load(logPath); err != nil {
		return nil, errors.Join(err, s.Close())
	}
	s.queueUnbuilt()
	return s, nil
}

func (s *Store) load(logPath string) error {
	var err error
	if s.log, err = os.OpenFile(logPath, os.O_RDWR|os.O_CREATE, 0o644); err != nil {
		return err
	}
	if s.tree, err = os.OpenFile(s.base+".tree.jsonl", os.O_RDWR|os.O_CREATE, 0o644); err != nil {
		return err
	}
	if s.items, err = sealedLines[Item](s.log); err != nil {
		return err
	}
	nodes, err := sealedLines[node](s.tree)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		s.nodes[n.Ref] = n.Text
	}
	raw, err := os.ReadFile(s.base + ".view.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &s.view)
	}
	if err != nil {
		return err
	}
	covered := 0
	for _, ref := range s.view.Lines {
		if ref.ID() != covered {
			return fmt.Errorf("%s.view.json skips or repeats items at %d, and a view is never rebuilt from the log", s.base, covered)
		}
		covered += ref.Span()
	}
	if covered != s.view.T || covered > len(s.items) {
		return fmt.Errorf("%s.view.json covers %d items and says %d, of %d in the log", s.base, covered, s.view.T, len(s.items))
	}
	return nil
}

func sealedLines[T any](file *os.File) ([]T, error) {
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	whole := bytes.LastIndexByte(content, '\n') + 1
	if tail := bytes.TrimSpace(content[whole:]); json.Valid(tail) {
		_, err = file.WriteString("\n")
		whole = len(content)
	} else if len(tail) > 0 {
		err = file.Truncate(int64(whole))
	}
	if err != nil {
		return nil, err
	}
	var values []T
	at := 0
	for line := range bytes.Lines(content[:whole]) {
		at++
		if line = bytes.TrimSpace(line); len(line) == 0 {
			continue
		}
		var value T
		if err := json.Unmarshal(line, &value); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", file.Name(), at, err)
		}
		values = append(values, value)
	}
	_, err = file.Seek(0, io.SeekEnd)
	return values, err
}

func (s *Store) Close() error {
	var errs []error
	for _, file := range []*os.File{s.log, s.tree, s.lock} {
		if file != nil {
			errs = append(errs, file.Close())
		}
	}
	return errors.Join(errs...)
}

func (s *Store) Append(item Item) error {
	if err := appendLine(s.log, item); err != nil {
		return err
	}
	s.items = append(s.items, item)
	s.ready = append(s.ready, Ref{Index: len(s.items) - 1})
	return nil
}

func appendLine(file *os.File, value any) error {
	line, err := json.Marshal(value)
	if err == nil {
		_, err = file.Write(append(line, '\n'))
	}
	return err
}
