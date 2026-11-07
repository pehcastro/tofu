package transform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func expandShort(input string) ([]byte, error) {
	syntax := fmt.Errorf("transform %q is not name($VAR, key=value, ...)", input)
	fn, rest, ok := strings.Cut(strings.TrimSpace(input), "(")
	if !ok {
		return nil, syntax
	}
	source, rest, ok := strings.Cut(strings.TrimRight(rest, ")"), ",")
	if !ok {
		return nil, syntax
	}
	args := map[string]any{"source": strings.TrimSpace(source)}
	rest = strings.TrimSpace(rest)
	for rest != "" {
		key, next, ok := strings.Cut(rest, "=")
		if !ok {
			return nil, syntax
		}
		key, next = strings.TrimSpace(key), strings.TrimLeft(next, " \t\n")
		if next == "" {
			return nil, syntax
		}
		width := len(next)
		switch next[0] {
		case '\'', '"', '[':
			closing := next[0]
			if closing == '[' {
				closing = ']'
			}
			width = strings.IndexByte(next[1:], closing) + 2
			if width == 1 {
				return nil, syntax
			}
		default:
			if comma := strings.IndexByte(next, ','); comma >= 0 {
				width = comma
			}
		}
		value, err := shortValue(key, next[:width])
		if err != nil {
			return nil, err
		}
		args[key] = value
		rest = strings.TrimSpace(strings.TrimLeft(strings.TrimLeft(next[width:], " \t\n"), ","))
	}
	return json.Marshal(map[string]any{strings.TrimSpace(fn): args})
}

func shortValue(key, raw string) (any, error) {
	switch key {
	case "startChar", "endChar":
		return strconv.Atoi(strings.TrimSpace(raw))
	case "separatedBy":
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "["), "]"))
		items := []string{}
		for item := range strings.SplitSeq(inner, ",") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, unquote(item))
			}
		}
		return items, nil
	}
	return unquote(raw), nil
}

func unquote(raw string) string {
	if len(raw) < 2 || raw[0] != raw[len(raw)-1] {
		return strings.TrimSpace(raw)
	}
	switch raw[0] {
	case '\'':
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
	case '"':
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return unquoted
		}
	}
	return strings.TrimSpace(raw)
}
