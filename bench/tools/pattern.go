package tools

import (
	"regexp"
	"strings"
)

var stopword = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "does": true, "do": true,
	"where": true, "what": true, "when": true, "it": true, "its": true, "in": true,
	"on": true, "to": true, "of": true, "for": true, "into": true, "or": true,
	"and": true, "this": true, "that": true, "as": true, "which": true, "like": true,
	"itself": true, "one": true, "own": true, "down": true, "get": true, "gets": true,
}

var quotedTerm = regexp.MustCompile("`([^`]+)`")

func QuotedTerms(question string) []string {
	found := quotedTerm.FindAllStringSubmatch(question, -1)
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, m[1])
	}
	return out
}

func ContentWords(question string) []string {
	fields := strings.FieldsFunc(question, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
			return false
		default:
			return true
		}
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		lower := strings.ToLower(f)
		if stopword[lower] || len(f) < 3 || seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, f)
	}
	return out
}

func Alternation(terms []string) string {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = regexp.QuoteMeta(t)
	}
	return strings.Join(quoted, "|")
}

func RealisticPattern(question string) string {
	quoted := QuotedTerms(question)
	if len(quoted) > 0 {
		return Alternation(quoted)
	}
	words := ContentWords(question)
	if len(words) == 0 {
		return ""
	}
	longest := words[0]
	for _, w := range words {
		if len(w) > len(longest) {
			longest = w
		}
	}
	return regexp.QuoteMeta(longest)
}
