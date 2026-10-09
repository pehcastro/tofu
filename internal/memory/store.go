package memory

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/memtree"
)

const (
	entriesFile = "entries.jsonl"
	logFile     = "log.jsonl"
	removedKind = "removed"
	localTrees  = "local"
	blockHead   = "remembered for you, by scope. Where two disagree, the first of %s wins. A line is id+n|kind: [memory#id] text, and an older line sums up n entries:\n"
)

type record struct {
	ID      string    `json:"id"`
	Kind    Kind      `json:"kind,omitempty"`
	Text    string    `json:"text,omitempty"`
	Said    string    `json:"said,omitempty"`
	Session string    `json:"session,omitempty"`
	At      time.Time `json:"at"`
	By      By        `json:"by,omitempty"`
	Author  string    `json:"author"`
	Removed bool      `json:"removed,omitempty"`
}

func (m *Memory) keep(e Entry, removed bool) (Entry, error) {
	root := m.home
	if e.Scope.local() {
		root = m.repo
	}
	author, err := m.author(root, true)
	if err != nil {
		return Entry{}, err
	}
	if e.File == "" {
		dir := m.shelf(e.Scope).Dir
		if e.Scope == UserLocal {
			dir = filepath.Join(dir, author)
		}
		e.File = filepath.Join(dir, entriesFile)
	}
	line := record{ID: e.ID, Text: e.Text, At: time.Now(), Author: author, Removed: true}
	if !removed {
		e.Author, e.Yours = author, true
		line = record{ID: e.ID, Kind: e.Kind, Text: e.Text, Said: e.Said, Session: e.Session, At: e.At, By: e.By, Author: author}
	}
	tree := m.treeDir(filepath.Dir(e.File))
	for _, dir := range []string{filepath.Dir(e.File), tree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Entry{}, err
		}
	}
	store, err := memtree.Open(filepath.Join(tree, logFile))
	if err != nil {
		return Entry{}, err
	}
	err = appendRecord(e.File, line)
	if err == nil {
		err = project(store, e.File, filepath.Join(tree, logFile))
	}
	return e, errors.Join(err, store.Close())
}

func (m *Memory) treeDir(entries string) string {
	inside, err := filepath.Rel(m.repo, entries)
	if err != nil || strings.HasPrefix(inside, "..") {
		return entries
	}
	return filepath.Join(m.state, localTrees, inside)
}

func project(store *memtree.Store, entries, log string) error {
	records, err := readRecords(entries)
	if err != nil {
		return err
	}
	projected, err := os.ReadFile(log)
	if err != nil {
		return err
	}
	for _, r := range records[min(bytes.Count(projected, []byte{'\n'}), len(records)):] {
		item := memtree.Item{Kind: string(r.Kind), Text: Entry{ID: r.ID, Text: r.Text}.Line()}
		if r.Removed {
			item.Kind = removedKind
		}
		if err := store.Append(item); err != nil {
			return err
		}
	}
	return nil
}

func appendRecord(file string, value record) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	log, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	content, err := io.ReadAll(log)
	whole := bytes.LastIndexByte(content, '\n') + 1
	if tail := bytes.TrimSpace(content[whole:]); len(tail) > 0 && json.Valid(tail) {
		line, whole = append([]byte{'\n'}, line...), len(content)
	}
	if err == nil {
		err = log.Truncate(int64(whole))
	}
	if err == nil {
		_, err = log.WriteAt(append(line, '\n'), int64(whole))
	}
	return errors.Join(err, log.Close())
}

func readRecords(file string) ([]record, error) {
	content, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []record
	at := 0
	for line := range bytes.Lines(content) {
		at++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			if !bytes.HasSuffix(line, []byte{'\n'}) {
				break
			}
			return nil, fmt.Errorf("%s line %d: %w", file, at, err)
		}
		records = append(records, r)
	}
	return records, nil
}

func (m *Memory) read(shelf *Shelf) error {
	shelf.stores = []string{shelf.Dir}
	mine := ""
	if _, missing := os.Stat(shelf.Dir); missing == nil && shelf.Scope.local() {
		author, err := m.author(m.repo, false)
		if err != nil {
			return err
		}
		mine = author
	}
	if shelf.Scope == UserLocal {
		shelf.stores = nil
		if mine != "" {
			shelf.stores = []string{filepath.Join(shelf.Dir, mine)}
		}
		if m.allUsers {
			found, _ := filepath.Glob(filepath.Join(shelf.Dir, "*", entriesFile))
			for _, file := range found {
				if !slices.Contains(shelf.stores, filepath.Dir(file)) {
					shelf.stores = append(shelf.stores, filepath.Dir(file))
				}
			}
		}
	}
	for _, store := range shelf.stores {
		records, err := readRecords(filepath.Join(store, entriesFile))
		if err != nil {
			return err
		}
		var entries []Entry
		for _, r := range records {
			entries = slices.DeleteFunc(entries, func(e Entry) bool { return e.ID == r.ID })
			if !r.Removed {
				entries = append(entries, Entry{ID: r.ID, Scope: shelf.Scope, Kind: r.Kind, Text: r.Text, Said: r.Said, Session: r.Session, At: r.At, By: r.By,
					Author: r.Author, Yours: !shelf.Scope.local() || r.Author == mine, File: filepath.Join(store, entriesFile)})
			}
		}
		shelf.Entries = append(shelf.Entries, entries...)
	}
	slices.SortStableFunc(shelf.Entries, func(a, b Entry) int { return cmp.Compare(a.number(), b.number()) })
	return nil
}

func (m Memory) Block() (string, error) {
	var precedence []string
	for _, shelf := range m.all() {
		precedence = append(precedence, string(shelf.Scope))
	}
	mine, err := m.author(m.repo, false)
	if err != nil {
		return "", err
	}
	var block strings.Builder
	for _, shelf := range []Shelf{m.Global, m.Project, m.UserLocal, m.ProjectLocal} {
		for _, store := range shelf.stores {
			view, err := m.storeView(store, shelf.Weight.ViewBytes)
			if err != nil {
				return "", err
			}
			head := string(shelf.Scope)
			if shelf.Scope == UserLocal && filepath.Base(store) != mine {
				head += ", another author"
			}
			if view != "" {
				block.WriteString(head + ":\n" + view)
			}
		}
	}
	if block.Len() == 0 {
		return "", nil
	}
	return fmt.Sprintf(blockHead, strings.Join(precedence, ", ")) + block.String(), nil
}

func (m Memory) storeView(dir string, budget int) (string, error) {
	entries, tree := filepath.Join(dir, entriesFile), m.treeDir(dir)
	if _, err := os.Stat(entries); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err := os.MkdirAll(tree, 0o755); err != nil {
		return "", err
	}
	store, err := memtree.Open(filepath.Join(tree, logFile))
	if err != nil {
		return "", err
	}
	err = project(store, entries, filepath.Join(tree, logFile))
	if err == nil {
		_, err = store.Build(context.Background(), func(context.Context, string, string, string) (string, error) {
			return "", errors.New("a memory summary is written between turns, never while the block is read")
		})
	}
	if err == nil {
		err = store.Advance(budget, budget/2)
	}
	return store.View(), errors.Join(err, store.Close())
}
