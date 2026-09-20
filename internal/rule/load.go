package rule

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"tofu/internal/sys"
)

func Load(path string) (Rule, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Rule{}, err
	}
	return parseRule(data, path)
}

func LoadFS(shipped fs.FS) ([]Rule, error) {
	names, err := fs.Glob(shipped, "*.yaml")
	if err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(shipped, name)
		if err != nil {
			return nil, err
		}
		r, err := parseRule(data, "catalog/rules/"+name)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func parseRule(data []byte, path string) (Rule, error) {
	r := Rule{File: path}
	err := scanKV(data, path, func(key, value string, line int) error {
		return r.setField(key, value, path, line)
	})
	if err != nil {
		return Rule{}, err
	}
	if r.ID == "" {
		return Rule{}, fmt.Errorf("%s: the rule declares no id", path)
	}
	if !r.Kind.valid() {
		return Rule{}, fmt.Errorf("%s: kind is %q, %q or %q, found %q", path, KindStructural, KindDecision, KindHuman, r.Kind)
	}
	if r.Checker == "" {
		return Rule{}, fmt.Errorf("%s: the rule declares no checker", path)
	}
	if !r.ModeDeclared {
		r.Mode = ModeShadow
	}
	return r, nil
}

func LoadDir(dir string) ([]Rule, error) {
	names, err := sys.ListFiles(dir, ".yaml")
	if err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(names))
	for _, name := range names {
		r, err := Load(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func (r *Rule) setField(key, value, path string, line int) error {
	switch key {
	case "id":
		r.ID = value
	case "kind":
		r.Kind = Kind(value)
	case "checker":
		r.Checker = value
	case "mode":
		m := Mode(value)
		if m != ModeShadow && m != ModeEnforced {
			return fmt.Errorf("%s:%d: mode is %q or %q, found %q", path, line, ModeShadow, ModeEnforced, value)
		}
		r.Mode = m
		r.ModeDeclared = true
	case "except":
		e := Exception(value)
		if !e.valid() {
			return fmt.Errorf("%s:%d: except is %q, found %q", path, line, ExceptionQuoted, value)
		}
		r.Except = e
	case "notes":
		r.Notes = value
	default:
		return fmt.Errorf("%s:%d: unknown field %q", path, line, key)
	}
	return nil
}

func scanKV(data []byte, path string, fn func(key, value string, line int) error) error {
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		at := strings.IndexByte(trimmed, ':')
		if at < 0 {
			return fmt.Errorf("%s:%d: expected key: value, found %q", path, line, trimmed)
		}
		key := strings.TrimSpace(trimmed[:at])
		value := strings.Trim(strings.TrimSpace(trimmed[at+1:]), `"`)
		if key == "" {
			return fmt.Errorf("%s:%d: expected key: value, found %q", path, line, trimmed)
		}
		if err := fn(key, value, line); err != nil {
			return err
		}
	}
	return nil
}
