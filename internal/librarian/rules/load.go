package rules

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"tofu/internal/agent/subagents"
)

type loadedFile struct {
	rule   Rule
	isRule bool
	err    error
}

func LoadFS(shipped fs.FS, root string) ([]Rule, error) {
	var names []string
	var contents [][]byte
	err := fs.WalkDir(shipped, ".", func(name string, entry fs.DirEntry, err error) error {
		dir := path.Dir(name)
		if err != nil || entry.IsDir() || (dir != "." && path.Base(dir) != "rules") || !strings.HasSuffix(name, ".yaml") {
			return err
		}
		data, err := fs.ReadFile(shipped, name)
		names, contents = append(names, name), append(contents, data)
		return err
	})
	if err != nil {
		return nil, err
	}
	files := make([]loadedFile, len(names))
	var parsing sync.WaitGroup
	workers := min(runtime.GOMAXPROCS(0), len(names))
	for first := range workers {
		parsing.Go(func() {
			for i := first; i < len(names); i += workers {
				files[i] = loadFile(contents[i], root, names[i])
			}
		})
	}
	parsing.Wait()
	var loaded []Rule
	for _, file := range files {
		if file.err != nil {
			return nil, file.err
		}
		if file.isRule {
			loaded = append(loaded, file.rule)
		}
	}
	return loaded, nil
}

func loadFile(data []byte, root, name string) loadedFile {
	var kind, when, overrides string
	_ = scanKV(data, "", func(key, value string, _ int) error {
		switch {
		case key == "kind" && kind == "":
			kind = value
		case key == "when" && when == "":
			when = value
		case key == "overrides" && overrides == "":
			overrides = value
		}
		return nil
	})
	if kind == ThresholdKind || when != "" {
		return loadedFile{}
	}
	one, err := parseOverriding(data, path.Join(root, name), overrides)
	if dir := path.Dir(name); err != nil && dir != "." {
		err = fmt.Errorf("%w (%s was read as a rule because its directory is named %q)", err, path.Join(root, dir), "rules")
	}
	return loadedFile{one, true, err}
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

func parseFile(data []byte, file string) (Rule, error) {
	return parseOverriding(data, file, declared(data, "overrides"))
}

func parseOverriding(data []byte, file, overrides string) (Rule, error) {
	parse := parseRule
	if overrides != "" {
		parse = parseOverride
	}
	r, err := parse(data, file)
	if err != nil {
		return Rule{}, err
	}
	r.Version = 1
	if _, version, named := strings.Cut(strings.TrimSuffix(path.Base(file), ".yaml"), "@"); named {
		if r.Version, err = strconv.Atoi(version); err != nil || r.Version < 1 {
			return Rule{}, fmt.Errorf("%s: a rule file is named <id>@<version>.yaml, and %q is not a version", file, version)
		}
	}
	return r, nil
}

func parseRule(data []byte, path string) (Rule, error) {
	r := Rule{File: path}
	var declares declaredTrigger
	err := scanKV(data, path, func(key, value string, line int) error {
		switch key {
		case "on":
			declares.on = value
		case "to":
			declares.to = value
		case "condition":
			declares.condition = value
		case "unless":
			declares.unless = value
		case "touches":
			declares.touches = value
		case "scope":
			declares.scope = value
		case "language":
			declares.language = value
		case "framework":
			declares.framework = value
		case "task":
			declares.task = value
		case "role":
			declares.role = value
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
	if r.AlsoReaches != ReachQAOnly && r.Domain != DomainQA {
		return Rule{}, fmt.Errorf("%s: rule %q is domain %s and declares also_reaches, and only a %s rule reaches beyond its own agents", path, r.ID, r.Domain, DomainQA)
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
	if (r.Checker == CheckerPattern) != (r.Pattern != nil) || (r.Paths != nil || r.ExceptPaths != nil) && r.Pattern == nil {
		return Rule{}, fmt.Errorf("%s: rule %q: pattern, paths and except_paths go with checker %s, and that checker needs a pattern", path, r.ID, CheckerPattern)
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
	if (r.Checker == CheckerCommand) != (r.Command != "") {
		return Rule{}, fmt.Errorf("%s: rule %q: a command goes with checker %s, and that checker needs a command", path, r.ID, CheckerCommand)
	}
	return r, nil
}

func HumanRuleFile(id string, concern Concern, mode Mode, text string) ([]byte, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" || strings.ContainsAny(text, "\r\n") {
		return nil, fmt.Errorf("rule %q needs its text on one line", id)
	}
	data := fmt.Appendf(nil, "id: %s\ndomain: %s\nkind: %s\nconcern: %s\nmode: %s\ntext: %s\n", id, DomainGeneral, KindHuman, concern, mode, text)
	_, err := parseRule(data, id+"@1.yaml")
	return data, err
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

func Layer(below, layer []Rule) []Rule {
	placed := make(map[int]Rule, len(layer))
	var added []Rule
	for _, one := range layer {
		at, overrides, stale := target(below, one)
		switch {
		case stale:
		case overrides:
			placed[at] = one.over(below[at])
		default:
			added = append(slices.DeleteFunc(added, func(r Rule) bool { return r.ID == one.ID }), one)
		}
	}
	kept := make([]Rule, 0, len(below)+len(added))
	for i, one := range below {
		over, found := placed[i]
		switch {
		case !found:
			kept = append(kept, one)
		case over.Mode != ModeOff:
			kept = append(kept, over)
		}
	}
	for _, one := range added {
		if one.Mode != ModeOff {
			kept = append(kept, one)
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
	case "also_reaches":
		if Reach(value) != ReachWorkOnTests {
			return fmt.Errorf("%s:%d: also_reaches is %q, found %q", path, line, ReachWorkOnTests, value)
		}
		r.AlsoReaches = ReachWorkOnTests
	case "shapes":
		r.Shapes = Shape(value)
		if r.Shapes != ShapeWriting && r.Shapes != ShapeDesign {
			return fmt.Errorf("%s:%d: shapes is %q or %q, found %q", path, line, ShapeWriting, ShapeDesign, value)
		}
	case "text":
		r.Text = value
	case "command":
		r.Command = value
	case "pattern":
		pattern, err := regexp.Compile(value)
		if err != nil {
			return fmt.Errorf("%s:%d: the pattern %q is not a regular expression: %v", path, line, value, err)
		}
		r.Pattern = pattern
	case "paths", "except_paths":
		var globs []string
		for one := range strings.SplitSeq(value, ",") {
			glob := strings.TrimSpace(one)
			if _, err := subagents.Matches("a/path/the/globs/are/tested/against", []string{glob}); err != nil {
				return fmt.Errorf("%s:%d: %s names %q, which is not a path glob: %v", path, line, key, glob, err)
			}
			globs = append(globs, glob)
		}
		if key == "paths" {
			r.Paths = globs
		} else {
			r.ExceptPaths = globs
		}
	case "notes":
		r.Notes = value
	case "reason", "by", "at":
		return fmt.Errorf("%s:%d: %s belongs to an override, whose overrides: field names the rule it changes", path, line, key)
	default:
		return fmt.Errorf("%s:%d: unknown field %q", path, line, key)
	}
	return nil
}

func scanKV(data []byte, path string, fn func(key, value string, line int) error) error {
	return scanIndented(data, path, func(_ int, key, value string, line int) error { return fn(key, value, line) })
}

func scanIndented(data []byte, path string, fn func(indent int, key, value string, line int) error) error {
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
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
		if err := fn(len(raw)-len(strings.TrimLeft(raw, " \t")), key, value, line); err != nil {
			return err
		}
	}
	return nil
}
