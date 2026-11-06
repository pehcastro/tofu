package sift

import (
	"fmt"
	"strings"
)

const drainWildcard = "<*>"

type drainTemplate struct {
	tokens []string
	first  string
	count  int
}

func Drain(text string, similarPercent, templatesCap int) string {
	var templates []*drainTemplate
	byLength, folded := map[int][]*drainTemplate{}, 0
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		tokens := strings.Fields(line)
		if len(tokens) == 0 {
			continue
		}
		folded++
		var best *drainTemplate
		bestAgree := 0
		for _, candidate := range byLength[len(tokens)] {
			if agree, aligned := drainAgreement(candidate.tokens, tokens); aligned && agree > bestAgree {
				best, bestAgree = candidate, agree
			}
		}
		if best != nil && bestAgree*100 >= similarPercent*len(tokens) {
			for i, token := range tokens {
				if best.tokens[i] != token {
					best.tokens[i] = drainWildcard
				}
			}
			best.count++
			continue
		}
		if len(templates) == templatesCap {
			return text
		}
		made := &drainTemplate{tokens: tokens, first: line, count: 1}
		templates, byLength[len(tokens)] = append(templates, made), append(byLength[len(tokens)], made)
	}
	if len(templates) == folded {
		return text
	}
	var out strings.Builder
	for _, template := range templates {
		if template.count == 1 {
			out.WriteString(strings.TrimRight(template.first, " \t") + "\n")
			continue
		}
		fmt.Fprintf(&out, "[%d lines] %s\n", template.count, strings.Join(template.tokens, " "))
	}
	fmt.Fprintf(&out, "[sift: %d lines folded into %d templates by drain]\n", folded, len(templates))
	return out.String()
}

func drainAgreement(template, tokens []string) (int, bool) {
	agree := 0
	for i, token := range tokens {
		variable := template[i] == drainWildcard || strings.ContainsAny(token, "0123456789")
		switch {
		case template[i] == token && !variable:
			agree++
		case i == 0 && !variable:
			return 0, false
		}
	}
	return agree, true
}
