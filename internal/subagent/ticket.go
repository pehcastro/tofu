package subagent

import (
	"regexp"
	"strconv"
	"strings"
)

const SessionEnv = "TOFU_SESSION"

func TicketContract(id string, revisions []string, report string, stopped bool) Contract {
	if len(revisions) == 0 {
		return Contract{NoTicket: true}
	}
	return contractOf(id, acceptanceBullets(strings.Split(revisions[len(revisions)-1], "\n")), report, stopped)
}

type Summary struct {
	Omitted, Lines int
	Partial        bool
}

var summaryLine = regexp.MustCompile(`contract \S+: (\d+) of (\d+) acceptance lines omitted( \(partial)?`)

func ReadSummary(text string) (Summary, bool) {
	match := summaryLine.FindStringSubmatch(text)
	if match == nil {
		return Summary{}, false
	}
	omitted, _ := strconv.Atoi(match[1])
	lines, _ := strconv.Atoi(match[2])
	return Summary{Omitted: omitted, Lines: lines, Partial: match[3] != ""}, true
}
