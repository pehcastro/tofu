package filmstrip

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var foldSummary = regexp.MustCompile(`^· \(\d+\) tools( ·|$)`)

const (
	stopHint     = "ctrl+c stops the turn"
	stoppingHint = "stopping the turn, ctrl+c will not quit"
)

func turnIsRunning(lines []string) bool {
	for _, row := range lines {
		if strings.Contains(row, stopHint) || strings.Contains(row, stoppingHint) {
			return true
		}
	}
	return false
}

func TestNoRunningFrameOfAnyScenarioCarriesAFoldedSummaryLine(t *testing.T) {
	running := map[string]int{}
	for _, frame := range shot() {
		lines := strings.Split(ansi.Strip(frame.Content), "\n")
		if !turnIsRunning(lines) {
			continue
		}
		running[frame.Scenario]++
		for number, row := range lines {
			if foldSummary.MatchString(strings.TrimSpace(row)) {
				t.Errorf("%s line %d summarises a turn that has not ended\n%s", frame.Name, number+1, strings.TrimSpace(row))
			}
		}
	}
	for _, one := range scenarios() {
		if running[one.name] == 0 {
			t.Errorf("%s has no running frame, so the check passed over it", one.name)
		}
	}
}
