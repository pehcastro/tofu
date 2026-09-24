package gate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"tofu/internal/judge/question"
	"tofu/internal/sys"
)

type Origin string

const (
	OriginProject Origin = "project"
	OriginBinary  Origin = "binary"
)

const libraryRoot = "library"

func (o Origin) String() string {
	switch o {
	case OriginProject:
		return "the project"
	case OriginBinary:
		return "the binary"
	}
	panic("gate: unknown origin " + string(o))
}

func LoadPoint(shipped fs.FS, ref string, set question.Set, dir string) (Rule, Origin, error) {
	libraryDir := sys.Join(dir, libraryRoot)
	if dir == "" {
		var err error
		if libraryDir, err = sys.LibraryDir(); err != nil {
			return Rule{}, "", err
		}
	}
	isDir, err := sys.IsDir(libraryDir)
	if err != nil {
		return Rule{}, "", err
	}
	found := ""
	if isDir {
		if found, err = FindRule(os.DirFS(libraryDir), ref); err != nil {
			return Rule{}, "", err
		}
	}
	var r Rule
	origin := OriginBinary
	if found != "" {
		origin = OriginProject
		r, err = Load(sys.Join(libraryDir, filepath.FromSlash(found)))
	} else {
		r, err = LoadFS(shipped, ref)
	}
	if err != nil {
		return Rule{}, "", fmt.Errorf("the rule %s from %s is unusable: %w", ref, origin, err)
	}
	if r.Schema != SchemaGate {
		return r, origin, nil
	}
	findings := Lint(r, set)
	if len(findings) == 0 {
		return r, origin, nil
	}
	msgs := make([]string, len(findings))
	for i, finding := range findings {
		msgs[i] = finding.String()
	}
	return Rule{}, "", fmt.Errorf("the rule %s from %s fails its own lint: %s", ref, origin, strings.Join(msgs, "; "))
}

func FindRule(library fs.FS, ref string) (string, error) {
	found := ""
	err := walkThresholds(library, func(name, base string) {
		if base == ref && found == "" {
			found = name
		}
	})
	return found, err
}

func Refs(library fs.FS) ([]string, error) {
	var refs []string
	err := walkThresholds(library, func(_, base string) { refs = append(refs, base) })
	sort.Strings(refs)
	return refs, err
}

func walkThresholds(library fs.FS, found func(name, base string)) error {
	return fs.WalkDir(library, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || !strings.HasSuffix(name, ".yaml") {
			return err
		}
		if dir := path.Dir(name); dir != "." && path.Base(dir) != "rules" {
			return nil
		}
		if entry.IsDir() {
			return fmt.Errorf("%s is a directory where a rule file was expected", name)
		}
		data, err := fs.ReadFile(library, name)
		if err != nil {
			return err
		}
		if declaredKind(data) != ThresholdKind {
			return nil
		}
		found(name, strings.TrimSuffix(path.Base(name), ".yaml"))
		return nil
	})
}

func declaredKind(data []byte) string {
	kind := ""
	_ = scanBytes(data, "", func(indent int, key, value string, _ int) error {
		if indent == 0 && key == "kind" && kind == "" {
			kind = value
		}
		return nil
	})
	return kind
}

func LoadFS(shipped fs.FS, ref string) (Rule, error) {
	name, err := FindRule(shipped, ref)
	if err != nil {
		return Rule{}, err
	}
	if name == "" {
		return Rule{}, fmt.Errorf("no rule named %s ships in the binary", ref)
	}
	data, err := fs.ReadFile(shipped, name)
	if err != nil {
		return Rule{}, err
	}
	r, err := parse(data, path.Join(libraryRoot, name))
	if err != nil {
		if dir := path.Dir(name); dir != "." {
			return Rule{}, fmt.Errorf("%w (%s was read as a rule because its directory is named %q)", err, path.Join(libraryRoot, dir), "rules")
		}
		return Rule{}, err
	}
	return r, nil
}

func Load(path string) (Rule, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Rule{}, err
	}
	return parse(data, path)
}

var schemaName = regexp.MustCompile(`^[a-z][a-z_0-9]*$`)

func declaredSchema(data []byte, path string) (string, error) {
	schema := SchemaGate
	err := scanBytes(data, path, func(indent int, key, value string, _ int) error {
		if indent == 0 && key == "schema" {
			schema = value
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if !schemaName.MatchString(schema) {
		return "", fmt.Errorf("%s: schema is %q and a schema name is lower case letters, digits and underscores", path, schema)
	}
	return schema, nil
}

func sharedField(key string) bool {
	switch key {
	case "name", "domain", "kind", "schema", "rule_version", "questions", "questions_version", "mode", "sample_floor", "notes":
		return true
	}
	return false
}

func parse(data []byte, path string) (Rule, error) {
	schema, err := declaredSchema(data, path)
	if err != nil {
		return Rule{}, err
	}
	r := Rule{File: path, Schema: schema}
	foreign := schema != SchemaGate
	if foreign {
		r.ForeignThresholds = map[string]float64{}
	}
	inThresholds := false
	err = scanBytes(data, path, func(indent int, key, value string, line int) error {
		if foreign {
			switch {
			case indent == 0:
				inThresholds = key == "thresholds"
				if inThresholds || !sharedField(key) {
					return nil
				}
				return r.setField(key, value, path, line)
			case inThresholds:
				n, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return fmt.Errorf("%s:%d: %q is a number, found %q", path, line, key, value)
				}
				r.ForeignThresholds[key] = n
				return nil
			default:
				return nil
			}
		}
		switch {
		case indent == 0:
			inThresholds = key == "thresholds"
			if inThresholds {
				return nil
			}
			return r.setField(key, value, path, line)
		case inThresholds:
			return r.Thresholds.setField(key, value, path, line)
		default:
			return fmt.Errorf("%s:%d: an indent where a top-level key was expected", path, line)
		}
	})
	if err != nil {
		return Rule{}, err
	}
	if r.Name == "" {
		return Rule{}, fmt.Errorf("%s: the rule declares no name", path)
	}
	if r.Domain == "" {
		return Rule{}, fmt.Errorf("%s: the rule declares no domain", path)
	}
	if r.Kind != ThresholdKind {
		return Rule{}, fmt.Errorf("%s: the rule declares no kind, and a rule a decision point reads is kind %q", path, ThresholdKind)
	}
	r.ModeDeclared = r.Mode != ""
	if !r.ModeDeclared {
		r.Mode = ModeShadow
	}
	return r, nil
}

func scanKV(path string, fn func(indent int, key, value string, line int) error) error {
	data, err := sys.ReadFile(path)
	if err != nil {
		return err
	}
	return scanBytes(data, path, fn)
}

func scanBytes(data []byte, path string, fn func(indent int, key, value string, line int) error) error {
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := i + 1
		if strings.ContainsRune(raw, '\t') {
			return fmt.Errorf("%s:%d: a tab in the indent", path, line)
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		key, value, ok := splitKV(strings.TrimSpace(raw))
		if !ok {
			return fmt.Errorf("%s:%d: expected key: value, found %q", path, line, strings.TrimSpace(raw))
		}
		if err := fn(indent, key, value, line); err != nil {
			return err
		}
	}
	return nil
}

func splitKV(line string) (string, string, bool) {
	at := strings.IndexByte(line, ':')
	if at < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:at])
	value := strings.TrimSpace(line[at+1:])
	value = strings.Trim(value, `"`)
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func (r *Rule) setField(key, value, path string, line int) error {
	switch key {
	case "name":
		r.Name = value
	case "domain":
		r.Domain = value
	case "kind":
		if value != ThresholdKind {
			return fmt.Errorf("%s:%d: kind is %q, found %q", path, line, ThresholdKind, value)
		}
		r.Kind = value
	case "schema":
		r.Schema = value
	case "rule_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: rule_version is a whole number, found %q", path, line, value)
		}
		r.RuleVersion = n
	case "questions":
		r.Questions = value
	case "questions_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: questions_version is a whole number, found %q", path, line, value)
		}
		r.QuestionsVersion = n
	case "risk_question":
		r.RiskQuestion = value
	case "approval_question":
		r.ApprovalQuestion = value
	case "user_requested_question":
		r.UserRequestedQuestion = value
	case "from_untrusted_question":
		r.FromUntrustedQuestion = value
	case "mode":
		m := Mode(value)
		if m != ModeShadow && m != ModeEnforced {
			return fmt.Errorf("%s:%d: mode is %q or %q, found %q", path, line, ModeShadow, ModeEnforced, value)
		}
		r.Mode = m
	case "sample_floor":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: sample_floor is a whole number, found %q", path, line, value)
		}
		r.SampleFloor = n
	case "notes":
		r.Notes = value
	default:
		return fmt.Errorf("%s:%d: unknown field %q", path, line, key)
	}
	return nil
}

func (t *Thresholds) setField(key, value, path string, line int) error {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("%s:%d: %q is a number, found %q", path, line, key, value)
	}
	switch key {
	case "risk_ask_at":
		t.RiskAskAt = n
	case "risk_deny_at":
		t.RiskDenyAt = n
	case "user_requested_relax_at":
		t.UserRequestedRelaxAt = n
	case "approval_relax_at":
		t.ApprovalRelaxAt = n
	case "from_untrusted_block_at":
		t.FromUntrustedBlockAt = n
	default:
		return fmt.Errorf("%s:%d: unknown threshold %q", path, line, key)
	}
	return nil
}
