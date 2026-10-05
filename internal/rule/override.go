package rule

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const switchedOn = "on"

type Overriding struct {
	Rule  Rule
	Base  Rule
	Stale bool
}

func Overrides(below, layer []Rule) []Overriding {
	var found []Overriding
	for _, one := range layer {
		at, overrides, stale := target(below, one)
		if !overrides {
			continue
		}
		listed := Overriding{Rule: one, Stale: stale}
		if at >= 0 {
			listed.Base = below[at]
		}
		found = append(found, listed)
	}
	return found
}

func target(below []Rule, one Rule) (at int, overrides, stale bool) {
	if one.Override.Of == "" {
		at = slices.IndexFunc(below, func(r Rule) bool { return r.ID == one.ID })
		return at, at >= 0, false
	}
	at = slices.IndexFunc(below, func(r Rule) bool { return r.ID == one.Override.Of })
	return at, true, at < 0 || below[at].Version != one.Override.Version
}

func (one Rule) over(base Rule) Rule {
	if one.Override.Of == "" {
		return one
	}
	switch {
	case one.Mode != "":
		base.Mode = one.Mode
	case base.Mode == ModeOff:
		base.Mode = ModeShadow
	}
	base.ID, base.Text, base.File, base.Override = one.ID, cmp.Or(one.Text, base.Text), one.File, one.Override
	return base
}

func parseOverride(data []byte, path string) (Rule, error) {
	r := Rule{File: path, Override: Override{By: ByPerson}}
	err := scanKV(data, path, func(key, value string, line int) error {
		switch key {
		case "id":
			r.ID = value
		case "overrides":
			of, version, _ := strings.Cut(value, "@")
			n, err := strconv.Atoi(version)
			if of == "" || err != nil || n < 1 {
				return fmt.Errorf("%s:%d: overrides is <id>@<version>, like em_dash@1, found %q", path, line, value)
			}
			r.Override.Of, r.Override.Version = of, n
		case "mode":
			switch value {
			case string(ModeOff):
				r.Mode = ModeOff
			case switchedOn:
				r.Mode = ModeShadow
			default:
				return fmt.Errorf("%s:%d: an override's mode is %q or %q, and a text: replaces what the rule says instead, found %q", path, line, ModeOff, switchedOn, value)
			}
		case "text":
			r.Text = value
		case "reason":
			r.Override.Reason = value
		case "by":
			if By(value) != ByPerson && By(value) != ByAsked {
				return fmt.Errorf("%s:%d: by is %q or %q, found %q", path, line, ByPerson, ByAsked, value)
			}
			r.Override.By = By(value)
		case "at":
			r.Override.At = value
		default:
			return fmt.Errorf("%s:%d: %q does not belong in an override, which carries id, overrides, mode: off, mode: on or text, reason, by and at, and takes everything else from the rule it overrides", path, line, key)
		}
		return nil
	})
	switch {
	case err != nil:
		return Rule{}, err
	case r.ID == "":
		return Rule{}, fmt.Errorf("%s: the override declares no id", path)
	case strings.TrimSpace(r.Override.Reason) == "":
		return Rule{}, fmt.Errorf("%s: the override of %s carries no reason, and the reason is how a person reading the project later knows why", path, r.Override.Of)
	case (r.Mode != "") == (r.Text != ""):
		return Rule{}, fmt.Errorf("%s: an override carries mode: off, mode: on or a text:, exactly one of them", path)
	}
	return r, nil
}

func OverrideFile(id, text string, o Override) ([]byte, error) {
	if text == "" {
		return overrideFile(id, "mode: "+string(ModeOff), o)
	}
	return overrideFile(id, "text: "+text, o)
}

func SwitchOnFile(id string, o Override) ([]byte, error) {
	return overrideFile(id, "mode: "+switchedOn, o)
}

func overrideFile(id, change string, o Override) ([]byte, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	if strings.ContainsAny(change+o.Reason, "\r\n") {
		return nil, fmt.Errorf("the override of %q needs its text and its reason on one line each", id)
	}
	data := fmt.Appendf(nil, "id: %s\noverrides: %s@%d\n%s\nreason: %s\nby: %s\nat: %s\n", id, o.Of, o.Version, change, o.Reason, o.By, o.At)
	_, err := parseOverride(data, id+"@1.yaml")
	return data, err
}

func validID(id string) error {
	if id == "" || strings.ContainsFunc(id, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' }) {
		return fmt.Errorf("a rule id is lower case letters, digits and underscores, found %q", id)
	}
	return nil
}
