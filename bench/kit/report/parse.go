package report

import (
	"regexp"
	"strings"
)

var (
	markdownHeading   = regexp.MustCompile(`^#{1,6}\s+(.*\S)\s*$`)
	shoutedHeading    = regexp.MustCompile(`^[A-Z][A-Z0-9 ,'\-]{2,}$`)
	conclusionHeading = regexp.MustCompile(`(?i)\b(headline|answer|conclusion|findings?|recommendation|won|numbers say)\b`)
	sentenceEnd       = regexp.MustCompile(`[.!?](\s|$)`)
	inlineMarkup      = regexp.MustCompile("[`*]")
	whitespaceRun     = regexp.MustCompile(`\s+`)
)

const firstSentenceFloor = 120

type heading struct {
	line int
	text string
}

func lineHeading(text string) (string, bool) {
	if match := markdownHeading.FindStringSubmatch(text); match != nil {
		return match[1], true
	}
	if shoutedHeading.MatchString(text) {
		return text, true
	}
	return "", false
}

func headingsOf(lines []string) []heading {
	var found []heading
	for i, line := range lines {
		if text, ok := lineHeading(strings.TrimRight(line, " \t")); ok {
			found = append(found, heading{line: i, text: text})
		}
	}
	return found
}

func sectionsOf(lines []string) []heading {
	found := headingsOf(lines)
	if len(found) == 0 || strings.TrimSpace(strings.Join(lines[:found[0].line], "")) != "" {
		return found
	}
	return found[1:]
}

func selfDeclaredState(body string) (State, string) {
	lines := strings.Split(body, "\n")
	sections := sectionsOf(lines)
	head := lines
	if len(sections) > 0 {
		head = lines[:sections[0].line]
	}
	for _, line := range head {
		switch {
		case strings.Contains(line, "WITHDRAWN") && strings.Contains(line, "PART"):
			return StateWithdrawnInPart, plain(strings.TrimSpace(line))
		case strings.Contains(line, "WITHDRAWN"):
			return StateWithdrawn, plain(strings.TrimSpace(line))
		case strings.Contains(line, "STALE"):
			return StateStale, plain(strings.TrimSpace(line))
		}
	}
	return StateStands, ""
}

func conclusionOf(body string) string {
	lines := strings.Split(body, "\n")
	sections := sectionsOf(lines)
	for i, h := range sections {
		if !conclusionHeading.MatchString(h.text) {
			continue
		}
		end := len(lines)
		if i+1 < len(sections) {
			end = sections[i+1].line
		}
		if said := firstProse(lines[h.line+1 : end]); said != "" {
			return said
		}
	}
	return Unparsed
}

func firstProse(lines []string) string {
	var held []string
	for _, line := range lines {
		text := plain(strings.TrimSpace(strings.TrimLeft(line, "> ")))
		if text == "" || strings.HasPrefix(text, "|") || strings.HasPrefix(text, "#") {
			if len(held) > 0 {
				break
			}
			continue
		}
		held = append(held, text)
	}
	if len(held) == 0 {
		return ""
	}
	return sentences(strings.Join(held, " "))
}

func sentences(text string) string {
	taken, rest, count := "", text, 0
	for count < 2 && len(taken) < firstSentenceFloor {
		at := sentenceEnd.FindStringIndex(rest)
		if at == nil {
			return strings.TrimSpace(taken + rest)
		}
		taken += rest[:at[1]]
		rest = rest[at[1]:]
		count++
	}
	return strings.TrimSpace(taken)
}

func plain(text string) string {
	return strings.TrimSpace(inlineMarkup.ReplaceAllString(text, ""))
}

func flatten(text string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(plain(text), " "))
}
