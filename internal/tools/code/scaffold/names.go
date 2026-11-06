package scaffold

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type form int

const (
	pascalCase form = iota
	camelCase
	snakeCase
	kebabCase
	screamingSnake
)

func (f form) String() string {
	switch f {
	case pascalCase:
		return "PascalCase"
	case camelCase:
		return "camelCase"
	case snakeCase:
		return "snake_case"
	case kebabCase:
		return "kebab-case"
	case screamingSnake:
		return "SCREAMING_SNAKE"
	}
	panic(fmt.Sprintf("scaffold: unknown name form %d", int(f)))
}

func (f form) join(words []string) string {
	switch f {
	case pascalCase, camelCase:
		var out strings.Builder
		for i, word := range words {
			if i == 0 && f == camelCase {
				out.WriteString(word)
				continue
			}
			out.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
		return out.String()
	case snakeCase:
		return strings.Join(words, "_")
	case kebabCase:
		return strings.Join(words, "-")
	case screamingSnake:
		return strings.ToUpper(strings.Join(words, "_"))
	}
	panic(fmt.Sprintf("scaffold: unknown name form %d", int(f)))
}

func words(name string) ([]string, error) {
	var found []string
	for _, chunk := range strings.FieldsFunc(name, func(c rune) bool { return c == '-' || c == '_' || c == ' ' }) {
		start := 0
		for i := range len(chunk) {
			if !isLetter(chunk[i]) && !isDigit(chunk[i]) {
				return nil, fmt.Errorf("%q may hold only letters, digits, -, _ and spaces", name)
			}
			camelStep := i > 0 && isUpper(chunk[i]) && (isLower(chunk[i-1]) || isDigit(chunk[i-1]))
			acronymEnd := i > 0 && i+1 < len(chunk) && isUpper(chunk[i-1]) && isUpper(chunk[i]) && isLower(chunk[i+1])
			if camelStep || acronymEnd {
				found = append(found, strings.ToLower(chunk[start:i]))
				start = i
			}
		}
		found = append(found, strings.ToLower(chunk[start:]))
	}
	switch {
	case len(found) == 0:
		return nil, errors.New("is empty")
	case !isLetter(found[0][0]):
		return nil, fmt.Errorf("%q must start with a letter", name)
	}
	return found, nil
}

type rename struct {
	old, new string
	forms    string
}

func renames(oldWords, newWords []string) []rename {
	var pairs []rename
	for f := pascalCase; f <= screamingSnake; f++ {
		old := f.join(oldWords)
		if at := slices.IndexFunc(pairs, func(pair rename) bool { return pair.old == old }); at >= 0 {
			pairs[at].forms += ", " + f.String()
			continue
		}
		pairs = append(pairs, rename{old: old, new: f.join(newWords), forms: f.String()})
	}
	return pairs
}

func replaced(text string, pairs []rename) (string, []int) {
	counts := make([]int, len(pairs))
	var out strings.Builder
scan:
	for i := 0; i < len(text); {
		for p, pair := range pairs {
			if strings.HasPrefix(text[i:], pair.old) && startsWord(text, i, pair.old) && endsWord(text, i+len(pair.old), pair.old) {
				out.WriteString(pair.new)
				counts[p]++
				i += len(pair.old)
				continue scan
			}
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String(), counts
}

func startsWord(text string, at int, old string) bool {
	if at == 0 {
		return true
	}
	prev := text[at-1]
	return !isLetter(prev) && !isDigit(prev) || isUpper(old[0]) && (isLower(prev) || isDigit(prev))
}

func endsWord(text string, at int, old string) bool {
	if at == len(text) {
		return true
	}
	next := text[at]
	if isUpper(next) {
		return strings.ToUpper(old) != old
	}
	return !isLower(next) && !isDigit(next)
}

func isLetter(c byte) bool { return isLower(c) || isUpper(c) }

func isLower(c byte) bool { return 'a' <= c && c <= 'z' }

func isUpper(c byte) bool { return 'A' <= c && c <= 'Z' }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
