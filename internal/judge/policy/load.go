package policy

import (
	"fmt"
	"io/fs"
	"regexp"
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

func (o Origin) String() string {
	switch o {
	case OriginProject:
		return "the project"
	case OriginBinary:
		return "the binary"
	}
	panic("policy: unknown origin " + string(o))
}

func LoadPoint(shipped fs.FS, ref string, set question.Set) (Policy, Origin, error) {
	catalogDir, err := sys.CatalogDir()
	if err != nil {
		return Policy{}, "", err
	}
	name := ref + ".yaml"
	path := sys.Join(catalogDir, "policy", name)
	present, err := sys.Exists(path)
	if err != nil {
		return Policy{}, "", err
	}
	var pol Policy
	origin := OriginBinary
	if present {
		origin = OriginProject
		pol, err = Load(path)
	} else {
		pol, err = LoadFS(shipped, name)
	}
	if err != nil {
		return Policy{}, "", fmt.Errorf("the policy %s from %s is unusable: %w", ref, origin, err)
	}
	if pol.Schema != SchemaGate {
		return pol, origin, nil
	}
	findings := Lint(pol, set)
	if len(findings) == 0 {
		return pol, origin, nil
	}
	msgs := make([]string, len(findings))
	for i, finding := range findings {
		msgs[i] = finding.String()
	}
	return Policy{}, "", fmt.Errorf("the policy %s from %s fails its own lint: %s", ref, origin, strings.Join(msgs, "; "))
}

func LoadFS(shipped fs.FS, name string) (Policy, error) {
	data, err := fs.ReadFile(shipped, name)
	if err != nil {
		return Policy{}, err
	}
	return parse(data, "catalog/policy/"+name)
}

func Load(path string) (Policy, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Policy{}, err
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
	case "name", "schema", "policy_version", "questions", "questions_version", "mode", "sample_floor", "notes":
		return true
	}
	return false
}

func parse(data []byte, path string) (Policy, error) {
	schema, err := declaredSchema(data, path)
	if err != nil {
		return Policy{}, err
	}
	pol := Policy{File: path, Schema: schema}
	foreign := schema != SchemaGate
	if foreign {
		pol.ForeignThresholds = map[string]float64{}
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
				return pol.setField(key, value, path, line)
			case inThresholds:
				n, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return fmt.Errorf("%s:%d: %q is a number, found %q", path, line, key, value)
				}
				pol.ForeignThresholds[key] = n
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
			return pol.setField(key, value, path, line)
		case inThresholds:
			return pol.Thresholds.setField(key, value, path, line)
		default:
			return fmt.Errorf("%s:%d: an indent where a top-level key was expected", path, line)
		}
	})
	if err != nil {
		return Policy{}, err
	}
	if pol.Name == "" {
		return Policy{}, fmt.Errorf("%s: the policy declares no name", path)
	}
	pol.ModeDeclared = pol.Mode != ""
	if !pol.ModeDeclared {
		pol.Mode = ModeShadow
	}
	return pol, nil
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

func (p *Policy) setField(key, value, path string, line int) error {
	switch key {
	case "name":
		p.Name = value
	case "schema":
		p.Schema = value
	case "policy_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: policy_version is a whole number, found %q", path, line, value)
		}
		p.PolicyVersion = n
	case "questions":
		p.Questions = value
	case "questions_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: questions_version is a whole number, found %q", path, line, value)
		}
		p.QuestionsVersion = n
	case "risk_question":
		p.RiskQuestion = value
	case "approval_question":
		p.ApprovalQuestion = value
	case "user_requested_question":
		p.UserRequestedQuestion = value
	case "from_untrusted_question":
		p.FromUntrustedQuestion = value
	case "mode":
		m := Mode(value)
		if m != ModeShadow && m != ModeEnforced {
			return fmt.Errorf("%s:%d: mode is %q or %q, found %q", path, line, ModeShadow, ModeEnforced, value)
		}
		p.Mode = m
	case "sample_floor":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: sample_floor is a whole number, found %q", path, line, value)
		}
		p.SampleFloor = n
	case "notes":
		p.Notes = value
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
