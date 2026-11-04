package entries

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/fs"
)

const (
	shelfSuffix   = ".yaml"
	copiedShelves = "copied-shelves"
)

func (m *Memory) migrate(oldGlobal, oldProject string) (err error) {
	if err := os.MkdirAll(oldGlobal, 0o755); err != nil {
		return err
	}
	lock := filepath.Join(oldGlobal, "migrate.lock")
	file, held, err := fs.TryLock(lock)
	for deadline := time.Now().Add(konst.MemtreeLockWaitMillis * time.Millisecond); err == nil && held && time.Now().Before(deadline); {
		time.Sleep(konst.MemtreeLockRetryMillis * time.Millisecond)
		file, held, err = fs.TryLock(lock)
	}
	if err != nil {
		return err
	}
	if held {
		return fmt.Errorf("%s is held by another tofu past %d ms, and copying the 0.5.8 shelves takes it", lock, konst.MemtreeLockWaitMillis)
	}
	defer func() { err = errors.Join(err, fs.Unlock(file)) }()
	var copying []Entry
	markers := map[string][]string{}
	for _, dir := range []string{oldGlobal, oldProject} {
		marker := filepath.Join(dir, copiedShelves)
		done, err := os.ReadFile(marker)
		listed := strings.Fields(string(done))
		if errors.Is(err, os.ErrNotExist) {
			markers[marker] = listed
			store := m.Global.Dir
			if dir == oldProject {
				store = m.Project.Dir
			}
			if _, err := os.Stat(filepath.Join(store, entriesFile)); err == nil {
				m.Copied = append(m.Copied, "the memory shelves in "+dir+" were moved by an earlier build, so "+store+" is the only copy and tofu 0.5.8 no longer sees it")
			}
		} else if err != nil {
			return err
		}
		names, err := filepath.Glob(filepath.Join(dir, "*"+shelfSuffix))
		if err != nil {
			return err
		}
		for _, name := range names {
			if slices.Contains(listed, filepath.Base(name)) {
				continue
			}
			e, err := readShelfEntry(name)
			if err != nil {
				return err
			}
			e.Scope = Project
			if dir == oldGlobal || e.Kind == KindPerson {
				e.Scope = Global
			}
			copying = append(copying, e)
			listed = append(listed, filepath.Base(name))
			markers[marker] = listed
		}
	}
	slices.SortStableFunc(copying, func(a, b Entry) int { return cmp.Compare(a.number(), b.number()) })
	var said []string
	for _, e := range copying {
		e.File = ""
		if _, err := m.keep(e, false); err != nil {
			return err
		}
		said = append(said, e.ID+" to "+string(e.Scope))
	}
	for marker, names := range markers {
		if err := fs.WriteFile(marker, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	if len(said) > 0 {
		m.Copied = append(m.Copied, "copied memory "+strings.Join(said, ", ")+" from the 0.5.8 shelves, which stay for an older tofu; nothing is copied twice")
	}
	for _, line := range m.Copied {
		fmt.Fprintln(os.Stderr, line)
	}
	return nil
}

func readShelfEntry(file string) (Entry, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{File: file}
	for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			value, err = strconv.Unquote(value)
		}
		if !found || err != nil {
			return Entry{}, fmt.Errorf("%s:%d: expected key: value", file, i+1)
		}
		switch strings.TrimSpace(key) {
		case "id":
			e.ID = value
		case "kind":
			e.Kind = Kind(value)
		case "text":
			e.Text = value
		case "said":
			e.Said = value
		case "session":
			e.Session = value
		case "at":
			e.At, err = time.Parse(time.RFC3339, value)
		case "by":
			e.By = By(value)
		default:
			return Entry{}, fmt.Errorf("%s:%d: unknown field %q", file, i+1, strings.TrimSpace(key))
		}
		if err != nil {
			return Entry{}, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
	}
	if want := strings.TrimSuffix(filepath.Base(file), shelfSuffix); e.ID != want || e.Text == "" || !e.Kind.valid() {
		return Entry{}, fmt.Errorf("%s: an entry needs id %s, a kind of %s, %s or %s, and its text", file, want, KindPerson, KindProject, KindReference)
	}
	return e, nil
}
