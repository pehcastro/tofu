package settings

import (
	"encoding/json"
	"fmt"
	"sort"

	"tofu/internal/sys"
)

const FileName = "settings.json"

type Scope int

const (
	Global Scope = iota
	Project
)

func (s Scope) String() string {
	if s == Project {
		return "project"
	}
	return "global"
}

type Store struct {
	table    []Spec
	paths    [2]string
	values   [2]map[string]int
	snapshot map[string]int
}

func Open(globalPath, projectPath string) (*Store, error) {
	return OpenWith(Default(), globalPath, projectPath)
}

func OpenWith(table []Spec, globalPath, projectPath string) (*Store, error) {
	store := &Store{table: table, paths: [2]string{globalPath, projectPath}}
	for scope := Global; scope <= Project; scope++ {
		values, err := readValues(store.paths[scope])
		if err != nil {
			return nil, err
		}
		store.values[scope] = values
	}
	store.Snapshot()
	return store, nil
}

func readValues(path string) (map[string]int, error) {
	if path == "" {
		return map[string]int{}, nil
	}
	present, err := sys.Exists(path)
	if err != nil || !present {
		return map[string]int{}, err
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]int{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	return values, nil
}

func (s *Store) Table() []Spec { return s.table }

func (s *Store) Path(scope Scope) string { return s.paths[scope] }

func (s *Store) Snapshot() {
	s.snapshot = map[string]int{}
	for _, spec := range s.table {
		if !spec.Restart {
			continue
		}
		s.snapshot[spec.Key] = s.Int(spec.Key)
	}
}

func (s *Store) specFor(key string) (Spec, bool) {
	for _, spec := range s.table {
		if spec.Key == key {
			return spec, true
		}
	}
	return Spec{}, false
}

func (s *Store) resolve(key string) (int, Scope, bool) {
	spec, known := s.specFor(key)
	if !known {
		return 0, Global, false
	}
	if value, present := s.values[Project][key]; present {
		return value, Project, true
	}
	if value, present := s.values[Global][key]; present {
		return value, Global, true
	}
	return spec.Default, Global, false
}

func (s *Store) Bool(key string) bool { value, _, _ := s.resolve(key); return value != 0 }

func (s *Store) Int(key string) int { value, _, _ := s.resolve(key); return value }

func (s *Store) Source(key string) (scope Scope, fromFile bool) {
	_, scope, fromFile = s.resolve(key)
	return scope, fromFile
}

func (s *Store) Set(scope Scope, key string, value int) error {
	if _, known := s.specFor(key); !known {
		return fmt.Errorf("settings: %q is not a declared setting", key)
	}
	if s.values[scope] == nil {
		s.values[scope] = map[string]int{}
	}
	s.values[scope][key] = value
	return s.write(scope)
}

func (s *Store) write(scope Scope) error {
	path := s.paths[scope]
	if path == "" {
		return fmt.Errorf("settings: no path is set for the %s scope", scope)
	}
	data, err := json.MarshalIndent(s.values[scope], "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFile(path, data, 0o600)
}

func (s *Store) RestartPending() []string {
	var pending []string
	for _, spec := range s.table {
		if !spec.Restart {
			continue
		}
		if s.Int(spec.Key) != s.snapshot[spec.Key] {
			pending = append(pending, spec.Key)
		}
	}
	sort.Strings(pending)
	return pending
}
