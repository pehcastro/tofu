package sift

import (
	"fmt"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/platform/fs"
)

var (
	goFramework       = fs.LazyRegexp(`^(?:=== (?:RUN|CONT|PAUSE|NAME)\s|\s*--- (?:PASS|FAIL|SKIP): |(?:PASS|FAIL)$|ok  \t|FAIL\t|\?   \t|\s+\S+\.go:\d+: |        |panic: |\[signal |goroutine \d+ \[|\t\S+:\d+|created by |\S+\((?:0x|\.\.\.|\))|exit status \d+$|# |\S+\.go:\d+:\d+: |testing: |coverage: )`)
	unittestProgress  = fs.LazyRegexp(`^[.FEsxu]+$`)
	unittestVerdict   = fs.LazyRegexp(`(?:^| \.\.\. )(?:ok|FAIL|ERROR|skipped.*|expected failure|unexpected success)$`)
	unittestRanLine   = fs.LazyRegexp(`^Ran \d+ tests? in `)
	unittestTallyLine = fs.LazyRegexp(`^(?:OK|FAILED)(?: \(.*\))?$`)
	pytestRule        = fs.LazyRegexp(`^[=_-]{3,}(?: .* [=_-]{3,})?$`)
	pytestCaptured    = fs.LazyRegexp(`^-{3,} Captured (?:stdout|stderr|log) \w+ -{3,}$`)
	tofuTrailer       = fs.LazyRegexp(`^(?:the command exited \d+|bash: .*|citations: \d+ found, \d+ resolved.*)$`)
	pytestNoise       = fs.LazyRegexp(`\[\s*\d+%\]$|^(?:platform |cachedir: |rootdir: |configfile: |plugins: |collecting |collected )|^=*\s*(?:\d+ \w+, )*\d+ \w+ in [\d.]+s\b`)
)

func goPrinted(lines []string) []string {
	return slices.DeleteFunc(lines, goFramework().MatchString)
}

func unittestPrinted(lines []string) []string {
	var printed []string
	framed, ran := false, false
	for _, line := range lines {
		switch {
		case unittestRanLine().MatchString(line):
			framed, ran = false, true
		case !ran && (strings.HasPrefix(line, "=====") || strings.HasPrefix(line, "-----")):
			framed = true
		case framed || unittestProgress().MatchString(line) || unittestVerdict().MatchString(line) || ran && unittestTallyLine().MatchString(line):
		default:
			printed = append(printed, line)
		}
	}
	return printed
}

func pytestPrinted(lines []string) []string {
	var printed []string
	inline, captured := true, false
	for _, line := range lines {
		switch {
		case pytestCaptured().MatchString(line):
			inline, captured = false, true
		case pytestRule().MatchString(line):
			inline, captured = strings.Contains(line, " test session starts "), false
		case captured || inline && !pytestNoise().MatchString(line):
			printed = append(printed, line)
		}
	}
	return printed
}

func printedBlock(printed []string) string {
	printed = slices.DeleteFunc(printed, func(line string) bool { return line == "" || tofuTrailer().MatchString(line) })
	if len(printed) == 0 {
		return ""
	}
	shown := printed[:min(len(printed), konst.TestLinesCap)]
	block := "printed by the program:\n" + strings.Join(shown, "\n") + "\n"
	if cut := len(printed) - len(shown); cut > 0 {
		block += fmt.Sprintf("(%d more printed lines not shown)\n", cut)
	}
	return block
}
