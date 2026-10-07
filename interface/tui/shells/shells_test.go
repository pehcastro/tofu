package shells

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/internal/golden"
)

var testNow = time.Date(2026, 9, 25, 14, 30, 0, 0, time.UTC)

func testModel() Model {
	lines := make([]string, 0, 50)
	for index := range 50 {
		switch index % 5 {
		case 0:
			lines = append(lines, "GET /api/session/"+strconv.Itoa(index)+" 200 12ms")
		case 1:
			lines = append(lines, "file change: internal/turn/loop.go")
		case 2:
			lines = append(lines, "rebuilt "+strconv.Itoa(index)+" packages in 180ms")
		case 3:
			lines = append(lines, "warn: slow request to /api/models")
		default:
			lines = append(lines, "\x1b[32mok\x1b[0m  tofu/internal/shell  0.4s")
		}
	}
	zero, ended := 0, testNow.Add(-2*time.Minute)
	m := New(func() time.Time { return testNow })
	m.SetSize(120, 36)
	m.Set([]Entry{
		{Name: "dev-server", Command: "npm run dev", State: Running, Started: testNow.Add(-372 * time.Second), PID: 18432, Dir: "./web", Owner: "go-dev 13", Log: strings.Join(lines, "\n") + "\n"},
		{Name: "build", Command: "go build ./...", State: Exited, Started: testNow.Add(-3 * time.Minute), Ended: &ended, ExitCode: &zero, PID: 21904, Dir: ".", Log: "compiled 42 packages\n"},
		{Name: "watcher", Command: "go test ./... -watch", State: Killed, Started: testNow.Add(-5 * time.Minute), Ended: &ended, PID: 17812, Dir: ".", Owner: "ui-audit", Log: "running interface tests\n"},
	})
	return m
}

func TestShellsViewGolden(t *testing.T) {
	golden.Assert(t, "shells-120x36.golden", testModel().View())
}

func TestALeftOverShellIsMarkedAndOfferedForEnding(t *testing.T) {
	m := testModel()
	m.Entries[0].State, m.Entries[0].Owner = LeftOver, ""
	golden.Assert(t, "shells-left-over-120x36.golden", m.View())
	if intent := m.Key("k"); intent != IntentKillAsk {
		t.Fatalf("k on a left over shell gives intent %d, want the confirmation", intent)
	}
}

func TestStyledShellSurfaceMatchesLipgloss(t *testing.T) {
	logLines := strings.Split((&cache{}).styledLog(testModel().Entries[0].Log), "\n")
	content := "\n" + strings.Join(logLines[:min(20, len(logLines))], "\n")
	for _, width := range []int{60, 90, 120} {
		got := (&look.PaneCache{}).Surface(width, 24, "", panePadding, content)
		if want := look.Surface(width, 24, "", panePadding, content); got != want {
			t.Fatalf("styled shell surface changed at width=%d", width)
		}
	}
}

func TestShellSidebarCacheMatchesUncached(t *testing.T) {
	m := testModel()
	check := func(stage string) {
		t.Helper()
		got := m.View()
		uncached := m
		uncached.cache = nil
		if want := uncached.View(); got != want {
			t.Fatalf("%s: cached shell view differs from uncached render", stage)
		}
	}
	check("initial")
	m.Wheel(-8)
	check("scroll")
	m.Key("j")
	check("process")
	m.Entries[1].State = Killed
	check("status")
	m.SetSize(80, 36)
	check("width")
}

func TestShellWheelScrollsOutputWithoutChangingSidebarSelection(t *testing.T) {
	m := testModel()
	before := ansi.Strip(m.View())
	m.Wheel(-3)
	if m.pick != 0 || m.scroll != 3 {
		t.Fatalf("sidebar wheel changed selection instead of scrolling output: pick=%d scroll=%d", m.pick, m.scroll)
	}
	if ansi.Strip(m.View()) == before {
		t.Fatal("shell output did not move after wheel input")
	}
	m.Wheel(3)
	if m.scroll != 0 {
		t.Fatalf("wheel down did not return to newest output: %d", m.scroll)
	}
}

func TestOutputPaneSaysWhenNothingWasPrinted(t *testing.T) {
	m := testModel()
	m.Entries[0].Log, m.Entries[1].Log = "", ""
	for pick, want := range map[int]string{0: "nothing printed yet", 1: "printed nothing"} {
		m.pick = pick
		if view := ansi.Strip(m.View()); !strings.Contains(view, want) {
			t.Errorf("a %s shell with no output does not say %q:\n%s", m.Entries[pick].State, want, view)
		}
	}
}

func BenchmarkProgressedScreenRender(b *testing.B) {
	b.Run("shells", func(b *testing.B) {
		m := testModel()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.scroll = i % 30
			_ = m.View()
		}
	})
}

func BenchmarkSteadyScreenRender(b *testing.B) {
	b.Run("shells", func(b *testing.B) {
		m := testModel()
		m.scroll = 15
		_ = m.View()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m.View()
		}
	})
}

func BenchmarkWheelEventFrame(b *testing.B) {
	b.Run("shells", func(b *testing.B) {
		m := testModel()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.scroll = i % 30
			m.Wheel(-3)
			_ = m.View()
		}
	})
}
