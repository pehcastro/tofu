package sample

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/cli"
	"tofu/internal/golden"
)

var (
	moment = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	names  = []string{"models-reload", "login-status", "browser-install"}
)

func printed(t *testing.T, page cli.Page, lines []string) string {
	t.Helper()
	var out bytes.Buffer
	if err := page.Print(&out, lines); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestTheThreeMocksMatchTheirGoldensInColourAndPlainAtTwoWidths(t *testing.T) {
	for mode, profile := range map[string]colorprofile.Profile{"colour": colorprofile.TrueColor, "plain": colorprofile.NoTTY} {
		for _, columns := range []int{80, 120} {
			page := cli.Page{Profile: profile, Width: min(columns, 100), Home: "/home/sample"}
			for i, lines := range Pages(page, moment) {
				golden.Assert(t, names[i]+"-"+mode+"-"+strconv.Itoa(columns)+".golden", printed(t, page, lines))
			}
		}
	}
}

func TestEveryCardOnOnePageHasTheSameWidth(t *testing.T) {
	for _, width := range []int{60, 80, 100} {
		page := cli.Page{Profile: colorprofile.TrueColor, Width: width, Home: "/home/sample"}
		lines := Pages(page, moment)[1]
		var edges []int
		for _, line := range lines {
			line = ansi.Strip(line)
			if trimmed := strings.TrimLeft(line, " "); strings.HasPrefix(trimmed, "┌") || strings.HasPrefix(trimmed, "└") {
				edges = append(edges, ansi.StringWidth(line))
			}
		}
		if len(edges) != 6 {
			t.Fatalf("at %d columns the accounts page has %d card edges, want 6 for three cards:\n%s", width, len(edges), ansi.Strip(strings.Join(lines, "\n")))
		}
		for _, cells := range edges {
			if cells != width {
				t.Errorf("at %d columns a card edge is %d cells, want every edge %d: %v", width, cells, width, edges)
				break
			}
		}
	}
}
