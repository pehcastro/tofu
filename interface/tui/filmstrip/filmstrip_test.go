package filmstrip

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/frametime"
	"tofu/interface/tui/progress"
	"tofu/interface/tui/session"
	"tofu/internal/golden"
	"tofu/internal/konst"
)

const (
	goldenSuffix   = ".golden"
	readableSuffix = ".txt"
	settingsLink   = "[settings]"
	leastScenarios = 5
	leastPlain     = 7
	budgetWidth    = 120
	budgetHeight   = 36
)

func stem(frame Frame) string {
	return strings.ReplaceAll(frame.Name, nameSeparator, "-") +
		"-" + strconv.Itoa(fixture.Width) + "x" + strconv.Itoa(fixture.Height)
}

func shot() []Frame { return All(fixture.Width, fixture.Height) }

func TestEveryFrameOfEverySequenceMatchesItsGolden(t *testing.T) {
	for _, frame := range shot() {
		t.Run(frame.Name, func(t *testing.T) {
			golden.Assert(t, stem(frame)+goldenSuffix, frame.Content)
			golden.Assert(t, stem(frame)+readableSuffix, ansi.Strip(frame.Content))
		})
	}
}

func TestEveryGoldenHasAReadableSiblingWithNoEscapeAndNoVersion(t *testing.T) {
	goldens, err := filepath.Glob(filepath.Join("testdata", "*"+goldenSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if len(goldens) != len(Names()) {
		t.Fatalf("testdata holds %d goldens and the filmstrip has %d frames", len(goldens), len(Names()))
	}
	for _, painted := range goldens {
		readable := strings.TrimSuffix(painted, goldenSuffix) + readableSuffix
		body, err := os.ReadFile(readable)
		if err != nil {
			t.Fatal(err)
		}
		sibling := string(body)
		if strings.Contains(sibling, "\x1b[") {
			t.Errorf("%s carries an escape code", readable)
		}
		if strings.Contains(sibling, konst.Version) {
			t.Errorf("%s carries the version %s, so every release would move it", readable, konst.Version)
		}
	}
}

func chatScenarios() []string {
	return []string{"plain", "twelve-tools", "markdown", "asking", "interrupted", "letting-tools-finish", "failed", "sub-agent"}
}

func TestTheClockIsInEveryChatFrameAndNeverGoesDown(t *testing.T) {
	session, turn := map[string]int{}, map[string]int{}
	for _, frame := range shot() {
		if !slices.Contains(chatScenarios(), frame.Scenario) {
			continue
		}
		lines := strings.Split(ansi.Strip(frame.Content), "\n")
		seconds, found := headerClock(lines[0])
		if !found {
			t.Fatalf("%s has no clock in its top bar\n%s", frame.Name, lines[0])
		}
		if seconds < session[frame.Scenario] {
			t.Errorf("%s: the session clock fell from %ds to %ds", frame.Name, session[frame.Scenario], seconds)
		}
		session[frame.Scenario] = seconds
		if running, live := turnClock(lines); live {
			if running < turn[frame.Scenario] {
				t.Errorf("%s: the turn clock fell from %ds to %ds", frame.Name, turn[frame.Scenario], running)
			}
			turn[frame.Scenario] = running
		}
	}
	for scenario, seconds := range session {
		if seconds == 0 {
			t.Errorf("%s never advances its session clock, so a fall could not show", scenario)
		}
	}
}

func headerClock(row string) (int, bool) {
	fields := strings.Fields(row)
	link := slices.Index(fields, settingsLink)
	start := link
	for start > 0 {
		if _, ok := clockSeconds(fields[start-1]); !ok {
			break
		}
		start--
	}
	if start == link || link < 0 {
		return 0, false
	}
	return clockSeconds(strings.Join(fields[start:link], " "))
}

func turnClock(lines []string) (int, bool) {
	seconds, live := 0, false
	for _, row := range lines {
		trimmed := strings.TrimSpace(row)
		if trimmed == "" || !strings.ContainsRune(progress.Frames, []rune(trimmed)[0]) {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		if reading, ok := clockSeconds(fields[1]); ok {
			seconds, live = reading, true
		}
	}
	return seconds, live
}

func clockSeconds(text string) (int, bool) {
	units := map[byte]int{'s': 1, 'm': 60, 'h': 3600, 'd': 86400}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return 0, false
	}
	total := 0
	for _, part := range parts {
		unit, known := units[part[len(part)-1]]
		if !known {
			return 0, false
		}
		count, err := strconv.Atoi(part[:len(part)-1])
		if err != nil {
			return 0, false
		}
		total += count * unit
	}
	return total, true
}

func TestFiveScenariosExistAndThePlainTurnRunsFromEmptyToFinished(t *testing.T) {
	frames := shot()
	counted := map[string]int{}
	order := []string{}
	for _, frame := range frames {
		if counted[frame.Scenario] == 0 {
			order = append(order, frame.Scenario)
		}
		counted[frame.Scenario]++
	}
	if len(order) < leastScenarios {
		t.Fatalf("%d scenarios exist, want at least %d: %v", len(order), leastScenarios, order)
	}
	if counted["plain"] < leastPlain {
		t.Errorf("the plain turn has %d frames, want at least %d", counted["plain"], leastPlain)
	}
	for _, scenario := range chatScenarios() {
		if counted[scenario] < 2 {
			t.Errorf("%s has %d frames, so it is a still and not a sequence", scenario, counted[scenario])
		}
	}
	shown := map[string]string{}
	for _, frame := range frames {
		shown[frame.Name] = ansi.Strip(frame.Content)
	}
	if !strings.Contains(shown["plain/01-fresh"], session.Placeholder) {
		t.Errorf("the first frame of the plain turn is not a fresh session\n%s", shown["plain/01-fresh"])
	}
	if !strings.Contains(shown["plain/09-answered"], "cooked for") {
		t.Errorf("the last frame of the plain turn is not finished\n%s", shown["plain/09-answered"])
	}
}

func TestEveryChatFrameNamesTheOneFixtureSet(t *testing.T) {
	for _, frame := range shot() {
		if !slices.Contains(chatScenarios(), frame.Scenario) {
			continue
		}
		lines := strings.Split(ansi.Strip(frame.Content), "\n")
		if !strings.Contains(lines[0], "./"+fixture.Path) {
			t.Errorf("%s: the top bar does not name ./%s\n%s", frame.Name, fixture.Path, lines[0])
		}
		if footer := lines[len(lines)-1]; !strings.Contains(footer, fixture.Slug) {
			t.Errorf("%s: the footer does not name %s\n%s", frame.Name, fixture.Slug, footer)
		}
	}
}

func TestEveryScreenStaysInsideTheFrameBudget(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(stubbedHost(home))
	for index, one := range append([]scenario{twelveTools()}, screens()...) {
		r := newReel(budgetWidth, budgetHeight, filepath.Join(home, strconv.Itoa(index)), one.tune)
		for _, step := range one.beats {
			step.play(r)
		}
		frametime.Frames(t, one.name, func() { _ = r.app.View().Content })
		r.driver.Close()
	}
}
