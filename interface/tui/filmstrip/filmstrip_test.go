package filmstrip

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/frametime"
	"tofu/interface/tui/progress"
	"tofu/internal/golden"
	"tofu/internal/konst"
)

const (
	goldenSuffix   = ".golden"
	readableSuffix = ".txt"
	headerFields   = "  ·  "
	leastScenarios = 5
	leastPlain     = 7
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

func TestTheClockIsInEveryFrameOfEverySequenceAndNeverGoesDown(t *testing.T) {
	session, turn := map[string]int{}, map[string]int{}
	for _, frame := range shot() {
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
	fields := strings.Split(strings.TrimSpace(row), headerFields)
	return clockSeconds(fields[len(fields)-1])
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
	counted := map[string]int{}
	order := []string{}
	for _, frame := range shot() {
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
	for _, scenario := range order {
		if counted[scenario] < 2 {
			t.Errorf("%s has %d frames, so it is a still and not a sequence", scenario, counted[scenario])
		}
	}
	first, opens := Find("plain/01-fresh", fixture.Width, fixture.Height)
	if !opens {
		t.Fatal("the plain turn does not start on an empty session")
	}
	if !strings.Contains(ansi.Strip(first.Content), "type a task and press enter") {
		t.Errorf("the first frame of the plain turn is not a fresh session\n%s", ansi.Strip(first.Content))
	}
	done, found := Find("plain/09-answered", fixture.Width, fixture.Height)
	if !found {
		t.Fatal("the plain turn does not end on a finished turn")
	}
	if !strings.Contains(ansi.Strip(done.Content), "cooked for") {
		t.Errorf("the last frame of the plain turn is not finished\n%s", ansi.Strip(done.Content))
	}
}

func TestEveryFrameNamesTheOneFixtureSet(t *testing.T) {
	for _, frame := range shot() {
		top := strings.Split(ansi.Strip(frame.Content), "\n")[0]
		for _, want := range []string{"./" + fixture.Path, fixture.Branch, fixture.Slug, fixture.SessionName, "#3c5f71"} {
			if !strings.Contains(top, want) {
				t.Errorf("%s: the top bar does not name %q\n%s", frame.Name, want, top)
			}
		}
		if !strings.Contains(ansi.Strip(frame.Content), "tofu "+fixture.Release) {
			t.Errorf("%s: the bottom bar does not name the release %q", frame.Name, fixture.Release)
		}
	}
}

func TestAFrameStaysInsideTheFrameBudget(t *testing.T) {
	r := newReel(fixture.Width, fixture.Height)
	for _, step := range twelveTools().beats {
		step.play(r)
	}
	frametime.Frames(t, "twelve-tools answered", func() { _ = r.app.View().Content })
}
