package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"tofu/interface/tui/filmstrip"
	"tofu/interface/tui/fixture"
	"tofu/interface/tui/trace"
)

func runFrame(t *testing.T, args []string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := frameVerb(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestFrameRendersToStandardOutputAndExitsOK(t *testing.T) {
	out, errOut, code := runFrame(t, []string{"--plain"})
	if code != exitOK {
		t.Fatalf("exit code is %d, want %d, stderr: %s", code, exitOK, errOut)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the verb wrote nothing to standard output")
	}
}

func TestEveryScenarioFrameCanBePrintedByName(t *testing.T) {
	names := filmstrip.Names()
	listed, _, code := runFrame(t, []string{"--list"})
	if code != exitOK {
		t.Fatalf("--list exits %d", code)
	}
	if printed := strings.Split(strings.TrimSpace(listed), "\n"); len(printed) != len(names) {
		t.Fatalf("--list printed %d lines, want %d", len(printed), len(names))
	}
	for _, name := range names {
		if !strings.Contains(listed, name) {
			t.Errorf("--list does not carry %q", name)
		}
		out, errOut, code := runFrame(t, []string{name, "--plain"})
		if code != exitOK {
			t.Fatalf("frame %q does not render: exit %d, %s", name, code, errOut)
		}
		if !strings.Contains(out, fixture.Slug) {
			t.Errorf("frame %q does not name the slug %q\n%s", name, fixture.Slug, out)
		}
	}
}

func TestAnUnknownFrameNameIsAUsageError(t *testing.T) {
	_, errOut, code := runFrame(t, []string{"plain/99-never"})
	if code != exitUsage {
		t.Fatalf("exit code is %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "--list") {
		t.Errorf("the error does not point at --list\n%s", errOut)
	}
}

func TestFrameTakesAWidthAndAHeight(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		out, errOut, code := runFrame(t, []string{"plain/09-answered", "--width", strconv.Itoa(width), "--height", "24", "--plain"})
		if code != exitOK {
			t.Fatalf("width %d: exit code is %d, stderr: %s", width, code, errOut)
		}
		if !strings.Contains(out, fixture.Task) {
			t.Errorf("width %d: the answered frame did not render\n%s", width, out)
		}
	}
}

func TestAPlainFlagStripsTheEscapeCodes(t *testing.T) {
	painted, _, code := runFrame(t, []string{"plain/01-fresh"})
	if code != exitOK {
		t.Fatalf("exit code is %d", code)
	}
	plain, _, code := runFrame(t, []string{"plain/01-fresh", "--plain"})
	if code != exitOK {
		t.Fatalf("exit code is %d", code)
	}
	if !strings.Contains(painted, "\x1b[") {
		t.Fatal("the default render carries no escape code to strip")
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("--plain left an escape code\n%s", plain)
	}
}

func chatFeed(out string) string {
	lines := strings.Split(out, "\n")
	if len(lines) <= 2 {
		return out
	}
	return strings.Join(lines[2:], "\n")
}

func TestNoIDDrawsUnderALineThatIsNotAnEvent(t *testing.T) {
	out, _, code := runFrame(t, []string{"plain/01-fresh", "--plain"})
	if code != exitOK {
		t.Fatal("the fresh frame did not render")
	}
	if strings.Contains(chatFeed(out), "#") {
		t.Errorf("the fresh session draws an id under a line that is not an event\n%s", out)
	}
}

func TestAnAnswersIDIsItsOwnEventsNotTheSessions(t *testing.T) {
	out, _, code := runFrame(t, []string{"child/07-answered", "--width", "200", "--plain"})
	if code != exitOK {
		t.Fatal("the answered frame did not render")
	}
	feed := chatFeed(out)
	if want := trace.Short("d41c08"); !strings.Contains(feed, want) {
		t.Fatalf("the answer does not carry its own event id %q\n%s", want, feed)
	}
	if sessionID := trace.Short(fixture.SessionID); strings.Contains(feed, sessionID) {
		t.Fatalf("the answer carries the session id %q instead of its own\n%s", sessionID, feed)
	}
}

func TestARowOneResetSurvivesLongerThanTheQuotaMeterAcrossFourWidths(t *testing.T) {
	for _, step := range []struct {
		width    int
		hasReset bool
	}{
		{80, false},
		{100, true},
		{120, true},
		{150, true},
	} {
		out, _, code := runFrame(t, []string{"plain/09-answered", "--width", strconv.Itoa(step.width), "--plain"})
		if code != exitOK {
			t.Fatalf("width %d did not render", step.width)
		}
		if strings.Contains(out, "resets in") != step.hasReset {
			t.Errorf("width %d: reset presence is %v, want %v\n%s", step.width, strings.Contains(out, "resets in"), step.hasReset, out)
		}
	}
}
