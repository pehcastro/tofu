package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	table        []Spec
	paths        [2]string
	values       [2]map[string]int
	texts        [2]map[string]string
	snapshot     map[string]int
	textSnapshot map[string]string
}

func Open(globalPath, projectPath string) (*Store, error) {
	return OpenWith(Default(), globalPath, projectPath)
}

func OpenWith(table []Spec, globalPath, projectPath string) (*Store, error) {
	if err := checkTable(table); err != nil {
		return nil, err
	}
	store := &Store{table: table, paths: [2]string{globalPath, projectPath}}
	for scope := Global; scope <= Project; scope++ {
		ints, texts, err := readValues(store.paths[scope])
		if err != nil {
			return nil, err
		}
		store.values[scope] = ints
		store.texts[scope] = texts
	}
	store.Snapshot()
	return store, nil
}

func readValues(path string) (map[string]int, map[string]string, error) {
	if path == "" {
		return map[string]int{}, map[string]string{}, nil
	}
	present, err := sys.Exists(path)
	if err != nil || !present {
		return map[string]int{}, map[string]string{}, err
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	ints := map[string]int{}
	texts := map[string]string{}
	for key, value := range raw {
		var asInt int
		if err := json.Unmarshal(value, &asInt); err == nil {
			ints[key] = asInt
			continue
		}
		var asText string
		if err := json.Unmarshal(value, &asText); err == nil {
			texts[key] = asText
			continue
		}
		return nil, nil, fmt.Errorf("settings: %s: %s is neither a number nor a string", path, key)
	}
	return ints, texts, nil
}

func (s *Store) Table() []Spec { return s.table }

func (s *Store) Path(scope Scope) string { return s.paths[scope] }

func (s *Store) Snapshot() {
	s.snapshot = map[string]int{}
	s.textSnapshot = map[string]string{}
	for _, spec := range s.table {
		if !spec.Restart {
			continue
		}
		if spec.Kind == Text {
			s.textSnapshot[spec.Key] = s.Text(spec.Key)
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
	for _, scope := range []Scope{Project, Global} {
		if value, present := s.values[scope][key]; present {
			return value, scope, true
		}
	}
	return spec.Default, Global, false
}

func (s *Store) resolveText(key string) (string, Scope, bool) {
	spec, known := s.specFor(key)
	if !known {
		return "", Global, false
	}
	for _, scope := range []Scope{Project, Global} {
		if value, present := s.texts[scope][key]; present && spec.allows(value) {
			return value, scope, true
		}
	}
	return spec.DefaultText, Global, false
}

func (s *Store) Bool(key string) bool { value, _, _ := s.resolve(key); return value != 0 }

func (s *Store) Int(key string) int { value, _, _ := s.resolve(key); return value }

func (s *Store) Text(key string) string { value, _, _ := s.resolveText(key); return value }

func (s *Store) Source(key string) (scope Scope, fromFile bool) {
	if spec, known := s.specFor(key); known && spec.Kind == Text {
		_, scope, fromFile = s.resolveText(key)
		return scope, fromFile
	}
	_, scope, fromFile = s.resolve(key)
	return scope, fromFile
}

func (s *Store) Set(scope Scope, key string, value int) error {
	spec, known := s.specFor(key)
	if !known {
		return fmt.Errorf("settings: %q is not a declared setting", key)
	}
	if err := spec.refuses(value); err != nil {
		return err
	}
	if s.values[scope] == nil {
		s.values[scope] = map[string]int{}
	}
	s.values[scope][key] = value
	return s.write(scope)
}

func (s *Store) SetText(scope Scope, key, value string) error {
	spec, known := s.specFor(key)
	if !known {
		return fmt.Errorf("settings: %q is not a declared setting", key)
	}
	switch {
	case spec.allows(value):
	case spec.ListOf != nil:
		return fmt.Errorf("settings: %s takes a list of %s, each once, got %q", key, strings.Join(spec.ListOf, ", "), value)
	default:
		return fmt.Errorf("settings: %s takes one of %s, got %q", key, strings.Join(spec.Choices, ", "), value)
	}
	if s.texts[scope] == nil {
		s.texts[scope] = map[string]string{}
	}
	s.texts[scope][key] = value
	return s.write(scope)
}

func (s *Store) write(scope Scope) error {
	path := s.paths[scope]
	if path == "" {
		return fmt.Errorf("settings: no path is set for the %s scope", scope)
	}
	merged := make(map[string]any, len(s.values[scope])+len(s.texts[scope]))
	for key, value := range s.values[scope] {
		merged[key] = value
	}
	for key, value := range s.texts[scope] {
		merged[key] = value
	}
	data, err := json.MarshalIndent(merged, "", "  ")
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
		if spec.Kind == Text {
			if s.Text(spec.Key) != s.textSnapshot[spec.Key] {
				pending = append(pending, spec.Key)
			}
			continue
		}
		if s.Int(spec.Key) != s.snapshot[spec.Key] {
			pending = append(pending, spec.Key)
		}
	}
	sort.Strings(pending)
	return pending
}
