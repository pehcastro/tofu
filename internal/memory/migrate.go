package memory

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const shelfSuffix = ".yaml"

func (m *Memory) migrate(oldGlobal, oldProject string) error {
	var moving []Entry
	for _, dir := range []string{oldGlobal, oldProject} {
		names, err := filepath.Glob(filepath.Join(dir, "*"+shelfSuffix))
		if err != nil {
			return err
		}
		for _, name := range names {
			e, err := readShelfEntry(name)
			if err != nil {
				return err
			}
			e.Scope = Project
			if dir == oldGlobal || e.Kind == KindPerson {
				e.Scope = Global
			}
			moving = append(moving, e)
		}
	}
	slices.SortStableFunc(moving, func(a, b Entry) int { return cmp.Compare(a.number(), b.number()) })
	for _, e := range moving {
		from := e.File
		e.File = ""
		if _, err := m.keep(e, false); err != nil {
			return err
		}
		if err := os.Remove(from); err != nil {
			return err
		}
		m.Notices = append(m.Notices, "moved memory "+e.ID+", a "+string(e.Kind)+" entry, from "+from+" to "+string(e.Scope))
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
