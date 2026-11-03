package guard

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"tofu/internal/sys"
)

const (
	commentHitsShown  = 8
	commentPreviewLen = 60
)

func emdash(stdin []byte) Answer {
	dash := string(rune(0x2014))
	codePageDash := string([]rune{0xe2, 0x20ac, 0x201d})
	if text := string(stdin); strings.Contains(text, dash) || strings.Contains(text, codePageDash) {
		return refuse(2, "BLOCKED: em dash (U+2014) is forbidden in this repository. Use a comma, a colon, a period, or parentheses instead.")
	}
	return Answer{}
}

var (
	directive     = sys.LazyRegexp(`(?i)^//(go:|nolint)`)
	spacedSlashes = sys.LazyRegexp(`(?i)\s//\s`)
)

func comments(ev event) Answer {
	path := ev.ToolInput.FilePath
	if blank(path) || !foldSuffix(path, ".go") {
		return Answer{}
	}
	text := ev.ToolInput.Content
	if blank(text) {
		text = ev.ToolInput.NewString
	}
	if blank(text) {
		return Answer{}
	}
	var hits []string
	for index, line := range newline().Split(text, -1) {
		trim := strings.TrimLeftFunc(line, unicode.IsSpace)
		switch {
		case directive().MatchString(trim):
		case strings.HasPrefix(trim, "//"), strings.HasPrefix(trim, "/*"):
			hits = append(hits, fmt.Sprintf("  line %d: %s", index+1, string([]rune(trim)[:min(commentPreviewLen, len([]rune(trim)))])))
		case spacedSlashes().MatchString(line) && !strings.Contains(line, "://"):
			hits = append(hits, fmt.Sprintf("  line %d: %s", index+1, strings.TrimSpace(line)))
		}
	}
	if len(hits) == 0 {
		return Answer{}
	}
	shown := strings.Join(hits[:min(commentHitsShown, len(hits))], "\n")
	if len(hits) > commentHitsShown {
		shown += fmt.Sprintf("\n  ... and %d more", len(hits)-commentHitsShown)
	}
	return refuse(2, `BLOCKED: this repository holds no comments in Go files. Not a line comment, not a block comment, not a documentation comment.

`+shown+`

A comment is a repair for a name, a type or a shape that did not carry its meaning. Fix that instead:
  rename the identifier so the sentence is unnecessary
  give the value a type whose name says what the comment said
  split the function so each part is obvious on its own
  move the reasoning into your report, where a person reads it once

Only compiler directives are allowed: //go:build, //go:generate, //go:embed, //nolint.`)
}

var (
	libraryPath   = sys.LazyRegexp(`(?i)(^|/)library/`)
	changelogPath = sys.LazyRegexp(`(?i)/library/changelog/`)
	forbidden     = []struct {
		pattern func() *regexp.Regexp
		says    string
	}{
		{sys.LazyRegexp(`(?i)\b(TOFU|BOJI)-\d+`), "a ticket id"},
		{sys.LazyRegexp(`(?i)\bbench/[a-z]`), "a bench path"},
		{sys.LazyRegexp(`(?i)report-\d{4}-\d{2}-\d{2}`), "a dated report"},
		{sys.LazyRegexp(`(?i)\.local/`), "a path under .local"},
		{sys.LazyRegexp(`(?i)\b(the owner|the user) (decided|asked|wants|said)`), "a decision attributed to a person"},
		{sys.LazyRegexp(`(?i)\bwithdrawn on \d{4}-\d{2}-\d{2}`), "a withdrawal note"},
	}
)

func libraryWording(ev event) Answer {
	slashed := strings.ReplaceAll(ev.ToolInput.FilePath, `\`, "/")
	if slashed == "" || !libraryPath().MatchString(slashed) || changelogPath().MatchString(slashed) {
		return Answer{}
	}
	body := ""
	for _, value := range []string{ev.ToolInput.Content, ev.ToolInput.NewString} {
		if value != "" {
			body += "\n" + value
		}
	}
	if body == "" {
		return Answer{}
	}
	for _, rule := range forbidden {
		if hit := rule.pattern().FindString(body); hit != "" {
			return refuse(2, fmt.Sprintf(`BLOCKED: %s carries %s, '%s'.

library/ is tofu's own knowledge and behaviour: its rules, its agents, its
plugins, its question sets, its model catalog. It is embedded in the binary
and reaches a turn in whatever repository tofu runs in.

This repository's development does not belong there. A decision switch is
library data; the evidence behind the switch is not. Put the prose in
bench/ or under .local/ and leave the library with what tofu reads.`, slashed, rule.says, hit))
		}
	}
	return Answer{}
}
