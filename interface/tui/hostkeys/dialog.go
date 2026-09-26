package hostkeys

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	"tofu/internal/keymap"
)

const (
	dialogMargin = 8
	hostWidth    = 82
	compactWidth = 68
	compactRows  = 24
	inspectRows  = 36
	sourceRows   = 40
	actionColumn = 13
)

func supported(host keymap.HostName) bool { return host == keymap.Zed || host == keymap.VSCode }

func origin(modal string, width, height int) (x, y int) {
	return max(1, (width-lipgloss.Width(modal))/2), max(1, (height-lipgloss.Height(modal))/2)
}

func place(base, modal string, width, height int) string {
	x, y := origin(modal, width, height)
	return look.Over(look.Dim(base), modal, x, y)
}

func choiceAt(modal string, labels []string, x, y, width, height int) int {
	x0, y0 := origin(modal, width, height)
	lines := strings.Split(modal, "\n")
	if x < x0 || x >= x0+lipgloss.Width(modal) || y < y0 || y >= y0+len(lines) {
		return -1
	}
	line := ansi.Strip(lines[y-y0])
	for index, label := range labels {
		if pointer.TextHit(line, label, x-x0) {
			return index
		}
	}
	return -1
}

func choices(labels []string, cursor int) string {
	rows := make([]string, len(labels))
	for index, label := range labels {
		rows[index] = look.DialogChoice(index == cursor, label)
	}
	return strings.Join(rows, "\n")
}
