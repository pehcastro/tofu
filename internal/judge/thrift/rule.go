package thrift

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

type Mode string

const (
	ModeOff      Mode = "off"
	ModeEnforced Mode = "enforced"
)

type Rule struct {
	KeepAt float64
	Mode   Mode
	File   string
}

func LoadRule(shipped fs.FS, name string) (Rule, error) {
	file := name + ".yaml"
	data, err := fs.ReadFile(shipped, file)
	if err != nil {
		return Rule{}, fmt.Errorf("thrift: %s: %w", file, err)
	}
	return parseRule(data, file)
}

func parseRule(data []byte, file string) (Rule, error) {
	rule := Rule{File: file}
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "mode":
			mode := Mode(value)
			if mode != ModeOff && mode != ModeEnforced {
				return Rule{}, fmt.Errorf("%s:%d: mode is %s or %s, found %q", file, i+1, ModeOff, ModeEnforced, value)
			}
			rule.Mode = mode
		case "keep_at":
			at, err := strconv.ParseFloat(value, 64)
			if err != nil || at <= 0 || at > 1 {
				return Rule{}, fmt.Errorf("%s:%d: keep_at is a number above 0 and at or below 1, found %q", file, i+1, value)
			}
			rule.KeepAt = at
		}
	}
	if rule.Mode == "" {
		return Rule{}, fmt.Errorf("%s: names no mode", file)
	}
	if rule.KeepAt == 0 {
		return Rule{}, fmt.Errorf("%s: names no keep_at", file)
	}
	return rule, nil
}
