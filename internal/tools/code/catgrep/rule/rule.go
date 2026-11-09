package rule

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/pattern"
)

const (
	ruleKeys     = "pattern, kind, regex, all, any, not, inside, has"
	relationKeys = ruleKeys + ", stopBy, field"
)

type compiler struct {
	lang    lang.Lang
	grammar *gotreesitter.Language
}

func New(raw []byte, l lang.Lang) (Rule, error) {
	return compiler{lang: l, grammar: l.Grammar()}.rule(raw)
}

func (c compiler) rule(raw json.RawMessage) (Rule, error) {
	fields, err := object(raw)
	if err != nil {
		return Rule{}, err
	}
	return c.fields(fields, ruleKeys)
}

func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("a rule is an object with keys from %s, not %s", ruleKeys, raw)
	}
	return fields, nil
}

func (c compiler) fields(fields map[string]json.RawMessage, keys string) (Rule, error) {
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(strings.Split(keys, ", "), key) {
			return Rule{}, fmt.Errorf("rule key %q is not one of %s", key, keys)
		}
	}
	var parts []matcher
	for _, key := range strings.Split(ruleKeys, ", ") {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		part, err := c.part(key, raw)
		if err != nil {
			return Rule{}, fmt.Errorf("%s: %w", key, err)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return Rule{}, fmt.Errorf("a rule needs one of %s", ruleKeys)
	}
	return newRule(parts), nil
}

func (c compiler) part(key string, raw json.RawMessage) (matcher, error) {
	switch key {
	case "pattern":
		src, err := text(raw)
		if err != nil {
			return nil, err
		}
		p, err := pattern.New(src, c.lang)
		return patternRule{p}, err
	case "kind":
		kind, err := text(raw)
		if err != nil {
			return nil, err
		}
		symbol, known := c.grammar.SymbolByName(kind)
		if !known {
			return nil, fmt.Errorf("%q is not a node kind in %s", kind, c.lang)
		}
		return kindRule(c.grammar.PublicSymbolForNamedness(symbol, true)), nil
	case "regex":
		expr, err := text(raw)
		if err != nil {
			return nil, err
		}
		re, err := regexp.Compile(expr)
		return regexRule{re: re}, err
	case "all", "any":
		var raws []json.RawMessage
		if err := json.Unmarshal(raw, &raws); err != nil {
			return nil, fmt.Errorf("is a list of rules, not %s", raw)
		}
		subs := make([]matcher, len(raws))
		for i, sub := range raws {
			var err error
			if subs[i], err = c.rule(sub); err != nil {
				return nil, fmt.Errorf("%d: %w", i, err)
			}
		}
		if key == "any" {
			return anyRule(subs), nil
		}
		return newRule(subs), nil
	case "not":
		inner, err := c.rule(raw)
		return notRule{inner: inner}, err
	case "inside":
		rel, err := c.relation(raw)
		return insideRule{rel}, err
	case "has":
		rel, err := c.relation(raw)
		return hasRule{rel}, err
	}
	panic("rule: unknown key " + key)
}

func text(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("is a string, not %s", raw)
	}
	return s, nil
}

func (c compiler) relation(raw json.RawMessage) (relation, error) {
	fields, err := object(raw)
	if err != nil {
		return relation{}, err
	}
	rel := relation{stop: neighbor}
	if stop, ok := fields["stopBy"]; ok {
		delete(fields, "stopBy")
		var name string
		switch {
		case json.Unmarshal(stop, &name) != nil:
			rel.stop = untilRule
			if rel.until, err = c.rule(stop); err != nil {
				return relation{}, fmt.Errorf("stopBy: %w", err)
			}
		case name == "end":
			rel.stop = toEnd
		case name != "neighbor":
			return relation{}, fmt.Errorf("stopBy %q is not neighbor, end or a rule object", name)
		}
	}
	if field, ok := fields["field"]; ok {
		delete(fields, "field")
		if rel.field, err = text(field); err != nil {
			return relation{}, fmt.Errorf("field: %w", err)
		}
		if _, known := c.grammar.FieldByName(rel.field); !known {
			return relation{}, fmt.Errorf("field %q is not a field in %s", rel.field, c.lang)
		}
	}
	rel.rule, err = c.fields(fields, relationKeys)
	return rel, err
}
