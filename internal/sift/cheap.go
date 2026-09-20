package sift

import (
	"fmt"
	"regexp"
	"strings"

	"boji/internal/konst"
)

const emDash = string(rune(0x2014))

var (
	boldRun      = regexp.MustCompile(`\*\*[^*]+\*\*`)
	greenResult  = regexp.MustCompile(`(ci|tests|test|suite) (is |are |will be |stays )?green`)
	agreeingOpen = regexp.MustCompile(`^[ \t\n]*(you are right|you.re right|great question|good catch|exactly right|absolutely right)`)
	metricRules  = []*regexp.Regexp{
		regexp.MustCompile(`[0-9]+ (tests?|specs?)`),
		regexp.MustCompile(`typecheck ?[0-9]`),
		regexp.MustCompile(`lint (clean|0)`),
		regexp.MustCompile(`[0-9]+ routes?|routes? 200`),
		regexp.MustCompile(`(everything|all) committed|tree clean`),
	}
)

func Cheap(part Part) Mark {
	if part.Fenced() {
		return Mark{Keep: true}
	}
	low := strings.ToLower(part.Text)
	var flags []string

	if words := part.Words(); words > konst.BrevityWordCap {
		flags = append(flags, fmt.Sprintf("%d words past the cap", words))
	}
	if n := strings.Count(part.Text, emDash); n > 0 {
		flags = append(flags, fmt.Sprintf("%d em dashes", n))
	}
	if n := len(boldRun.FindAllString(part.Text, -1)); n > konst.BrevityBoldCap {
		flags = append(flags, fmt.Sprintf("%d bold phrases", n))
	}
	var banned []string
	for _, word := range []string{"landed", "dispatched", "in flight", "shipped", "surfaced", "north star"} {
		if hasWord(low, word) {
			banned = append(banned, word)
		}
	}
	if greenResult.MatchString(low) {
		banned = append(banned, "green")
	}
	if len(banned) > 0 {
		flags = append(flags, "banned words: "+strings.Join(banned, ", "))
	}
	if metrics(low) >= konst.BrevityMetricsCap {
		flags = append(flags, "a scorecard")
	}
	if agreeingOpen.MatchString(low) {
		flags = append(flags, "it opens by agreeing")
	}

	if len(flags) == 0 {
		return Mark{Keep: true}
	}
	return Mark{Reason: strings.Join(flags, "; ")}
}

func metrics(low string) int {
	count := 0
	for _, rule := range metricRules {
		if rule.MatchString(low) {
			count++
		}
	}
	return count
}

func hasWord(low, word string) bool {
	for i := 0; i+len(word) <= len(low); {
		offset := strings.Index(low[i:], word)
		if offset < 0 {
			return false
		}
		start := i + offset
		if !lowerLetterAt(low, start-1) && !lowerLetterAt(low, start+len(word)) {
			return true
		}
		i = start + 1
	}
	return false
}

func lowerLetterAt(s string, i int) bool {
	return i >= 0 && i < len(s) && s[i] >= 'a' && s[i] <= 'z'
}
