package shells

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var fixedStart = time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)

func devServer() Entry {
	return Entry{Name: "dev-server", Command: "npm run dev", State: Running, Started: fixedStart, Log: "listening on :3000"}
}

func build() Entry {
	ok := 0
	return Entry{Name: "build", Command: "go build ./...", State: Exited, Started: fixedStart, ExitCode: &ok, Log: "compiling\ndone"}
}

func testRun() Entry {
	ok := 0
	return Entry{Name: "test-run", Command: "go test ./...", State: Exited, Started: fixedStart, ExitCode: &ok, Log: "ok tofu/internal/shell 0.4s"}
}

func viewed(entries []Entry, pick int) string {
	var m Model
	m.SetSize(120, 24)
	m.Set(entries)
	for range pick {
		m.Key("down")
	}
	return ansi.Strip(m.View())
}

func TestADevServerAppearsAsAShell(t *testing.T) {
	content := viewed([]Entry{devServer()}, 0)
	if !strings.Contains(content, "dev-server") || !strings.Contains(content, "npm run dev") {
		t.Errorf("a dev server does not appear as a shell\n%s", content)
	}
}

func TestABuildAppearsAsAShell(t *testing.T) {
	content := viewed([]Entry{build()}, 0)
	if !strings.Contains(content, "build") || !strings.Contains(content, "go build ./...") {
		t.Errorf("a build does not appear as a shell\n%s", content)
	}
}

func TestATestRunAppearsAsAShell(t *testing.T) {
	content := viewed([]Entry{testRun()}, 0)
	if !strings.Contains(content, "test-run") || !strings.Contains(content, "go test ./...") {
		t.Errorf("a test run does not appear as a shell\n%s", content)
	}
}

func TestAnExitCodeOfZeroReadsAsZeroAndAnAbsentOneAsAbsent(t *testing.T) {
	zero := viewed([]Entry{build()}, 0)
	if !strings.Contains(zero, "exit 0") {
		t.Errorf("a process that exited 0 does not say so\n%s", zero)
	}
	reconciled := build()
	reconciled.ExitCode = nil
	unknown := viewed([]Entry{reconciled}, 0)
	if strings.Contains(unknown, "exit ") {
		t.Errorf("a process whose exit code was never read claims one\n%s", unknown)
	}
}

func TestTheEmptyShellsViewSaysNothingIsRunning(t *testing.T) {
	content := viewed(nil, 0)
	if !strings.Contains(content, "no process is running") {
		t.Errorf("the empty shells view does not say so\n%s", content)
	}
}

func TestPickingAProcessShowsItsRecentOutput(t *testing.T) {
	content := viewed([]Entry{devServer(), build()}, 1)
	if !strings.Contains(content, "compiling") || !strings.Contains(content, "done") {
		t.Errorf("picking build does not show its recent output\n%s", content)
	}
}

func TestRemoveDropsAProcessFromTheList(t *testing.T) {
	var m Model
	m.SetSize(120, 24)
	m.Set([]Entry{devServer(), build()})
	m.Remove("dev-server")
	content := ansi.Strip(m.View())
	if strings.Contains(content, "dev-server") {
		t.Errorf("a removed process is still listed\n%s", content)
	}
	if !strings.Contains(content, "build") {
		t.Errorf("removing one process dropped the other\n%s", content)
	}
}

func TestNeitherViewTakesTextInput(t *testing.T) {
	var m Model
	m.SetSize(80, 24)
	m.Set([]Entry{devServer(), build()})
	m.Key("x")
	if m.pick != 0 {
		t.Errorf("a plain letter moved the pick, so it is read as a command rather than ignored")
	}
}
