package rule

import (
	"fmt"
	"io/fs"
	"os"
	"path"
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

func LoadFS(shipped fs.FS, root string) ([]Rule, error) {
	var loaded []Rule
	err := fs.WalkDir(shipped, ".", func(name string, entry fs.DirEntry, err error) error {
		dir := path.Dir(name)
		if err != nil || entry.IsDir() || (dir != "." && path.Base(dir) != "rules") || !strings.HasSuffix(name, ".yaml") {
			return err
		}
		data, err := fs.ReadFile(shipped, name)
		if err != nil {
			return err
		}
		if declared(data, "kind") == ThresholdKind {
			return nil
		}
		one, err := parseRule(data, path.Join(root, name))
		if err != nil {
			return err
		}
		loaded = append(loaded, one)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

func declared(data []byte, want string) string {
	found := ""
	_ = scanKV(data, "", func(key, value string, _ int) error {
		if key == want && found == "" {
			found = value
		}
		return nil
	})
	return found
}

func parseRule(data []byte, path string) (Rule, error) {
	r := Rule{File: path}
	var declares declaredTrigger
	err := scanKV(data, path, func(key, value string, line int) error {
		switch key {
		case "condition":
			declares.condition = value
		case "scope":
			declares.scope = value
		case "language":
			declares.language = value
		case "task":
			declares.task = value
		default:
			return r.setField(key, value, path, line)
		}
		return nil
	})
	if err != nil {
		return Rule{}, err
	}
	if r.ID == "" {
		return Rule{}, fmt.Errorf("%s: the rule declares no id", path)
	}
	if r.Domain == "" {
		return Rule{}, fmt.Errorf("%s: the rule declares no domain, and a domain is %q, %q, %q or a tool name", path, DomainDev, DomainQA, DomainGeneral)
	}
	if !r.Kind.valid() {
		return Rule{}, fmt.Errorf("%s: kind is %q, %q, %q or %q, found %q", path, KindStructural, KindDecision, KindHuman, KindMeasured, r.Kind)
	}
	if r.Concern == "" {
		return Rule{}, fmt.Errorf("%s: rule %q declares no concern, and a concern is one of %s", path, r.ID, concernNames(allConcerns()))
	}
	if !r.Concern.valid() {
		return Rule{}, fmt.Errorf("%s: rule %q declares the concern %q, and a concern is one of %s", path, r.ID, r.Concern, concernNames(allConcerns()))
	}
	if !r.ModeDeclared {
		r.Mode = ModeShadow
	}
	r.Trigger, err = newTrigger(declares, path, r.ID)
	if err != nil {
		return Rule{}, err
	}
	if r.Concern.neverConditional() && !r.Trigger.AlwaysOn() {
		return Rule{}, fmt.Errorf("%s: rule %q is concern %s and declares a trigger, and %s are never conditional", path, r.ID, r.Concern, concernNames(neverConditionalConcerns()))
	}
	if r.Mode == ModeEnforced && (r.Kind == KindMeasured || r.Kind == KindHuman) {
		return Rule{}, fmt.Errorf("%s: rule %q is kind %s and declares mode %s, and only a rule naming a checker can block: use %s to drop it or leave the mode out", path, r.ID, r.Kind, ModeEnforced, ModeOff)
	}
	if r.Kind == KindMeasured {
		if r.Checker != "" {
			return Rule{}, fmt.Errorf("%s: rule %q is kind %s and declares checker %q, a measured rule names a measurement instead", path, r.ID, r.Kind, r.Checker)
		}
		for _, required := range [][2]string{{"measurement", r.Measurement}, {"source", r.Source}, {"evidence", r.Evidence}} {
			if required[1] == "" {
				return Rule{}, fmt.Errorf("%s: rule %q is kind %s and declares no %s", path, r.ID, r.Kind, required[0])
			}
		}
		return r, nil
	}
	if r.Measurement != "" {
		return Rule{}, fmt.Errorf("%s: rule %q is kind %s and declares measurement %q, only a %s rule is measured", path, r.ID, r.Kind, r.Measurement, KindMeasured)
	}
	if r.Kind == KindHuman {
		if r.Text == "" {
			return Rule{}, fmt.Errorf("%s: rule %q is kind %s and declares no text, and text is what the model reads", path, r.ID, r.Kind)
		}
		return r, nil
	}
	if r.Checker == "" {
		return Rule{}, fmt.Errorf("%s: rule %q is kind %s and declares no checker", path, r.ID, r.Kind)
	}
	return r, nil
}

func LoadDir(dir string) ([]Rule, error) {
	loaded, err := LoadFS(os.DirFS(dir), dir)
	if err != nil {
		return nil, err
	}
	for i := range loaded {
		loaded[i].File = filepath.FromSlash(loaded[i].File)
	}
	return loaded, nil
}

func Layer(shipped, project []Rule) []Rule {
	pending := make(map[string]Rule, len(project))
	for _, one := range project {
		pending[one.ID] = one
	}
	kept := make([]Rule, 0, len(shipped)+len(project))
	keep := func(one Rule) {
		if one.Mode != ModeOff {
			kept = append(kept, one)
		}
	}
	for _, one := range shipped {
		if override, replaced := pending[one.ID]; replaced {
			delete(pending, one.ID)
			keep(override)
			continue
		}
		keep(one)
	}
	for _, one := range project {
		if _, unused := pending[one.ID]; unused {
			delete(pending, one.ID)
			keep(one)
		}
	}
	return kept
}

func (r *Rule) setField(key, value, path string, line int) error {
	switch key {
	case "id":
		r.ID = value
	case "kind":
		r.Kind = Kind(value)
	case "concern":
		r.Concern = Concern(value)
	case "domain":
		r.Domain = value
	case "checker":
		r.Checker = value
	case "measurement":
		r.Measurement = value
	case "source":
		r.Source = value
	case "evidence":
		r.Evidence = value
	case "mode":
		m := Mode(value)
		if m != ModeShadow && m != ModeEnforced && m != ModeOff {
			return fmt.Errorf("%s:%d: mode is %q, %q or %q, found %q", path, line, ModeShadow, ModeEnforced, ModeOff, value)
		}
		r.Mode = m
		r.ModeDeclared = true
	case "except":
		e := Exception(value)
		if !e.valid() {
			return fmt.Errorf("%s:%d: except is %q, found %q", path, line, ExceptionQuoted, value)
		}
		r.Except = e
	case "text":
		r.Text = value
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
