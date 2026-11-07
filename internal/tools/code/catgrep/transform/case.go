package transform

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Case uint8

const (
	LowerCase Case = iota + 1
	UpperCase
	Capitalize
	CamelCase
	SnakeCase
	KebabCase
	PascalCase
)

func (c Case) String() string {
	switch c {
	case LowerCase:
		return "lowerCase"
	case UpperCase:
		return "upperCase"
	case Capitalize:
		return "capitalize"
	case CamelCase:
		return "camelCase"
	case SnakeCase:
		return "snakeCase"
	case KebabCase:
		return "kebabCase"
	case PascalCase:
		return "pascalCase"
	}
	panic("transform: unknown case")
}

func (c *Case) UnmarshalText(text []byte) (err error) {
	*c, err = named(text, LowerCase, PascalCase)
	return err
}

func named[T interface {
	~uint8
	fmt.Stringer
}](text []byte, first, last T) (T, error) {
	var names []string
	for known := first; known <= last; known++ {
		if known.String() == string(text) {
			return known, nil
		}
		names = append(names, known.String())
	}
	return 0, fmt.Errorf("%q is not one of %s", text, strings.Join(names, ", "))
}

type Separator uint8

const (
	CaseChange Separator = iota + 1
	Dash
	Dot
	Slash
	Space
	Underscore
)

func (s Separator) String() string {
	switch s {
	case CaseChange:
		return "caseChange"
	case Dash:
		return "dash"
	case Dot:
		return "dot"
	case Slash:
		return "slash"
	case Space:
		return "space"
	case Underscore:
		return "underscore"
	}
	panic("transform: unknown separator")
}

func (s *Separator) UnmarshalText(text []byte) (err error) {
	*s, err = named(text, CaseChange, Underscore)
	return err
}

func (c Case) Apply(s string, seps []Separator) string {
	switch c {
	case LowerCase:
		return strings.ToLower(s)
	case UpperCase:
		return strings.ToUpper(s)
	case Capitalize:
		return capitalize(s)
	case CamelCase:
		words := Split(s, seps)
		for i, w := range words {
			if i == 0 {
				words[i] = strings.ToLower(w)
			} else {
				words[i] = capitalize(w)
			}
		}
		return strings.Join(words, "")
	case SnakeCase:
		return strings.ToLower(strings.Join(Split(s, seps), "_"))
	case KebabCase:
		return strings.ToLower(strings.Join(Split(s, seps), "-"))
	case PascalCase:
		words := Split(s, seps)
		for i, w := range words {
			words[i] = capitalize(w)
		}
		return strings.Join(words, "")
	}
	panic("transform: unknown case")
}

func capitalize(s string) string {
	first, width := utf8.DecodeRuneInString(s)
	if width == 0 {
		return s
	}
	return strings.ToUpper(string(first)) + s[width:]
}

const delimitersInSeparatorOrder = "-./ _"

type caseState uint8

const (
	ignoreCase caseState = iota
	lower
	oneUpper
	multiUpper
)

func Split(s string, seps []Separator) []string {
	delimiters, state := []rune(delimitersInSeparatorOrder), lower
	if seps != nil {
		delimiters, state = nil, ignoreCase
		for _, sep := range seps {
			if sep == CaseChange {
				state = lower
				continue
			}
			delimiters = append(delimiters, rune(delimitersInSeparatorOrder[sep-Dash]))
		}
	}
	var words []string
	emit := func(from, to int) {
		if from < to {
			words = append(words, s[from:to])
		}
	}
	left, right := 0, 0
	var lastUpper rune
	for _, c := range s {
		switch {
		case slices.Contains(delimiters, c):
			emit(left, right)
			left = right + utf8.RuneLen(c)
			right = left
			if state != ignoreCase {
				state = lower
			}
			continue
		case state == lower && unicode.IsUpper(c):
			emit(left, right)
			left = right
			right += utf8.RuneLen(c)
			state = oneUpper
			continue
		case state == multiUpper && unicode.IsLower(c):
			split := right - utf8.RuneLen(lastUpper)
			emit(left, split)
			left = split
			right += utf8.RuneLen(c)
			state = lower
			continue
		}
		right += utf8.RuneLen(c)
		switch {
		case state == ignoreCase:
		case unicode.IsLower(c):
			state = lower
		case state == lower:
			state = oneUpper
		default:
			state, lastUpper = multiUpper, c
		}
	}
	emit(left, right)
	return words
}
