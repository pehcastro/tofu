package transform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/replace"
)

type Trans struct {
	Source metavar.Var
	op     op
}

type op interface{ apply(text string) string }

type substring struct{ start, end *int }

type replacement struct {
	pattern *regexp.Regexp
	by      string
}

type conversion struct {
	to         Case
	separators []Separator
}

func (s substring) apply(text string) string {
	chars := []rune(text)
	n := len(chars)
	start, end := resolveChar(s.start, 0, n), resolveChar(s.end, n, n)
	if start > end || start >= n {
		return ""
	}
	return string(chars[start:end])
}

func resolveChar(at *int, fallback, n int) int {
	c := fallback
	if at != nil {
		c = *at
	}
	switch {
	case c >= n:
		return n
	case c >= 0:
		return c
	case n+c < 0:
		return 0
	}
	return n + c
}

func (r replacement) apply(text string) string { return r.pattern.ReplaceAllString(text, r.by) }

func (c conversion) apply(text string) string { return c.to.Apply(text, c.separators) }

type spec struct {
	Substring *struct {
		Source    string `json:"source"`
		StartChar *int   `json:"startChar"`
		EndChar   *int   `json:"endChar"`
	} `json:"substring"`
	Replace *struct {
		Source  string  `json:"source"`
		Replace *string `json:"replace"`
		By      *string `json:"by"`
	} `json:"replace"`
	Convert *struct {
		Source      string      `json:"source"`
		ToCase      *Case       `json:"toCase"`
		SeparatedBy []Separator `json:"separatedBy"`
	} `json:"convert"`
}

func (t *Trans) UnmarshalJSON(data []byte) error {
	var short string
	if json.Unmarshal(data, &short) == nil {
		expanded, err := expandShort(short)
		if err != nil {
			return err
		}
		data = expanded
	}
	var s spec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return err
	}
	kinds := 0
	for _, given := range []bool{s.Substring != nil, s.Replace != nil, s.Convert != nil} {
		if given {
			kinds++
		}
	}
	if kinds != 1 {
		return errors.New("a transform is exactly one of substring, replace or convert")
	}
	var source string
	switch {
	case s.Substring != nil:
		source, t.op = s.Substring.Source, substring{start: s.Substring.StartChar, end: s.Substring.EndChar}
	case s.Replace != nil:
		if s.Replace.Replace == nil || s.Replace.By == nil {
			return errors.New("replace needs both replace and by")
		}
		pattern, err := regexp.Compile(*s.Replace.Replace)
		if err != nil {
			return fmt.Errorf("replace: %w", err)
		}
		source, t.op = s.Replace.Source, replacement{pattern: pattern, by: *s.Replace.By}
	default:
		if s.Convert.ToCase == nil {
			return errors.New("convert needs toCase")
		}
		source, t.op = s.Convert.Source, conversion{to: *s.Convert.ToCase, separators: s.Convert.SeparatedBy}
	}
	v, ok := metavar.Extract(source, '$')
	if !ok || v.Kind != metavar.Capture && v.Kind != metavar.MultiCapture {
		return fmt.Errorf("transform source %q is not a named $ variable", source)
	}
	t.Source = v
	return nil
}

func (t Trans) Compute(env *metavar.Env) (string, error) {
	text, _, err := t.sourceText(env)
	if err != nil {
		return "", err
	}
	return t.op.apply(text), nil
}

func (t Trans) sourceText(env *metavar.Env) (string, int, error) {
	if t.Source.Kind == metavar.MultiCapture {
		nodes, ok := env.Multi(t.Source.Name)
		if !ok {
			return "", 0, fmt.Errorf("source $$$%s is not captured by the pattern", t.Source.Name)
		}
		if len(nodes) == 0 {
			return "", 0, nil
		}
		source, start := nodes[0].Source(), nodes[0].Start()
		return string(source[start:nodes[len(nodes)-1].End()]), replace.IndentAt(source[:start]), nil
	}
	if node, ok := env.Single(t.Source.Name); ok {
		return node.Text(), replace.IndentAt(node.Source()[:node.Start()]), nil
	}
	if text, ok := env.Transformed(t.Source.Name); ok {
		return text, 0, nil
	}
	return "", 0, fmt.Errorf("source $%s is not captured by the pattern", t.Source.Name)
}

type Named struct {
	Name  string
	Trans Trans
}

type Set []Named

func NewSet(transforms map[string]Trans) (Set, error) {
	var set Set
	done := map[string]bool{}
	var visit func(name string) error
	visit = func(name string) error {
		finished, seen := done[name]
		if seen && !finished {
			return fmt.Errorf("transform %s has a cyclic dependency", name)
		}
		t, defined := transforms[name]
		if seen || !defined {
			return nil
		}
		done[name] = false
		if err := visit(t.Source.Name); err != nil {
			return err
		}
		done[name] = true
		set = append(set, Named{Name: name, Trans: t})
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(transforms)) {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func (s Set) Apply(env *metavar.Env) error {
	for _, n := range s {
		_, single := env.Single(n.Name)
		_, multi := env.Multi(n.Name)
		if single || multi {
			return fmt.Errorf("transform %s is already captured by the pattern", n.Name)
		}
		text, indent, err := n.Trans.sourceText(env)
		if err != nil {
			return fmt.Errorf("transform %s: %w", n.Name, err)
		}
		env.InsertTransformed(n.Name, replace.Reindent(n.Trans.op.apply(text), indent, 0))
	}
	return nil
}
