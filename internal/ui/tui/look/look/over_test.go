package look

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func compositorOver(base, dialog string, x, y int) string {
	return lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(dialog).X(x).Y(y).Z(1)).Render()
}

func overBases() map[string]string {
	const width, height = 40, 12
	plain := strings.Repeat("underlay ", width)[:width]
	styled := Painted("left side ", Text, Panel) + Muted("muted middle text") + "\x1b[1m" + Style(Amber).Render("bold amber tail") + "\x1b[m"
	wide := strings.Repeat("界a", width/3) + "b"
	rows := func(row string) string { return strings.TrimSuffix(strings.Repeat(row+"\n", height), "\n") }
	return map[string]string{
		"plain":  rows(plain),
		"blank":  rows(strings.Repeat(" ", width)),
		"styled": rows(styled),
		"wide":   rows(wide),
		"mixed":  rows(plain) + "\n" + rows(styled) + "\n" + rows(wide),
		"short":  "one\n\x1b[31mtwo\x1b[m\nthree   ",
		"open":   rows("\x1b[41m" + plain),
	}
}

func overDialogs() map[string]string {
	return map[string]string{
		"panel":  DialogPanel(18, "Title", "a description", DialogChoice(true, "Yes")+"\n"+DialogChoice(false, "No"), "enter picks"),
		"plain":  "  unstyled head\n  界 wide row\n  tail",
		"single": Painted(" x ", Background, Mint),
	}
}

func TestOverMatchesCompositor(t *testing.T) {
	for baseName, base := range overBases() {
		baseWidth, baseHeight := lipgloss.Width(base), lipgloss.Height(base)
		for dialogName, dialog := range overDialogs() {
			dialogWidth, dialogHeight := lipgloss.Width(dialog), lipgloss.Height(dialog)
			for _, at := range [][2]int{{0, 0}, {1, 1}, {2, 3}, {7, 4}, {baseWidth - dialogWidth, baseHeight - dialogHeight}, {baseWidth - 3, 2}, {3, baseHeight - 1}, {baseWidth + 2, baseHeight + 1}} {
				x, y := max(0, at[0]), max(0, at[1])
				want := compositorOver(base, dialog, x, y)
				got := Over(base, dialog, x, y)
				if ansi.Strip(got) != ansi.Strip(want) {
					t.Fatalf("%s under %s at %d,%d: text differs\n got %q\nwant %q", dialogName, baseName, x, y, ansi.Strip(got), ansi.Strip(want))
				}
				if canonical := compositorOver(got, "", 0, 0); canonical != want {
					t.Fatalf("%s under %s at %d,%d: cells differ\n got %q\nwant %q", dialogName, baseName, x, y, canonical, want)
				}
			}
		}
	}
}
