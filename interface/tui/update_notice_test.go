package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/konst"
	"tofu/internal/widget"
)

const installedNotice = "tofu 9.9.9 is installed · restart to use it"

func screenRows(app *App) []string {
	return strings.Split(ansi.Strip(app.View().Content), "\n")
}

func rowWith(rows []string, text string) int {
	for index, row := range rows {
		if strings.Contains(row, text) {
			return index
		}
	}
	return -1
}

func TestAnUpdateNoticeSitsUnderTheWelcomeArtAndNeverInTheChat(t *testing.T) {
	for _, said := range []string{"", installedNotice} {
		app := freshApp(t, 100, 30)
		before := screenRows(app)
		if rowWith(before, "up to date") >= 0 {
			t.Fatalf("the welcome says up to date before any check answered\n%s", strings.Join(before, "\n"))
		}
		app.Update(updateMsg(said))
		rows := screenRows(app)
		screen := strings.Join(rows, "\n")
		want := said
		if said == "" {
			want = "up to date · " + konst.Version
		}
		line := rowWith(rows, want)
		art := artRows(rows)
		if line < 0 || len(art) == 0 {
			t.Fatalf("the welcome lost its art or never said %q\n%s", want, screen)
		}
		if gap := line - art[len(art)-1]; gap < 1 || gap > 2 {
			t.Fatalf("the notice is %d rows from the art rather than just under it\n%s", gap, screen)
		}
		if lead := strings.Index(rows[line], want); lead < (100-widget.Cells(want))/2-2 {
			t.Fatalf("the notice is not centred under the art\n%s", screen)
		}
		if line >= composerRow(t, rows)-2 {
			t.Fatalf("the notice sits at the composer rather than under the art\n%s", screen)
		}
	}
}

func TestARunningSessionCarriesTheNoticeAtTheRightOfTheRowAboveTheComposer(t *testing.T) {
	for _, width := range []int{120, 80} {
		app := sessionApp(t, width, 30)
		before := screenRows(app)
		app.Update(updateMsg(""))
		if quiet := screenRows(app); strings.Join(quiet, "\n") != strings.Join(before, "\n") {
			t.Fatalf("an up to date answer changed a running session\n%s", strings.Join(quiet, "\n"))
		}
		app.Update(updateMsg(installedNotice))
		rows := screenRows(app)
		screen := strings.Join(rows, "\n")
		if len(rows) != len(before) {
			t.Fatalf("the notice moved the frame from %d rows to %d\n%s", len(before), len(rows), screen)
		}
		at := rowWith(rows, "cooked for")
		if at < 0 || at > composerRow(t, rows) || rowWith(rows, "tofu 9.9") != at {
			t.Fatalf("the notice is not on the status row above the composer\n%s", screen)
		}
		if widget.Cells(rows[at]) != width {
			t.Fatalf("the status row is %d cells wide on a %d wide screen\n%s", widget.Cells(rows[at]), width, screen)
		}
		trimmed := strings.TrimRight(rows[at], " ")
		_, shown, _ := strings.Cut(trimmed, "  tofu 9.9")
		if !strings.HasPrefix(installedNotice[len("tofu 9.9"):], strings.TrimSuffix(shown, "…")) || widget.Cells(trimmed) < width-2 {
			t.Fatalf("the status row does not end with the notice\n%s", screen)
		}
		if strings.Count(screen, "tofu 9.9") > 1 {
			t.Fatalf("the notice is in the chat as well as the status row\n%s", screen)
		}
	}
}
