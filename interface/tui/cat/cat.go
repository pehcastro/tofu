package cat

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	BodyColor  = "#444a54"
	ShadeColor = "#2b2f35"
	EyeColor   = "#8bd46e"
	WhiteColor = "#e8eaeb"
)

type Pose uint8

const (
	Sitting Pose = iota
	Lying
)

type sprite []string

var (
	sitting = sprite{
		".X...S.......", ".XX.XS.......", ".XXXXS.......", "XGXGXXS......",
		".XXXXS.......", "...XS........", "..XWXS.....S.", "..XWXXS...S..",
		"..XXXXXS..S..", "..XXXXXS.S...", "..WXWXXXS....", ".............",
	}
	tailRight = sprite{
		".X...S.......", ".XX.XS.......", ".XXXXS.......", "XGXGXXS......",
		".XXXXS.......", "...XS........", "..XWXS......S", "..XWXXS....S.",
		"..XXXXXS...S.", "..XXXXXS..S..", "..WXWXXXXS...", ".............",
	}
	lying = sprite{
		".X...S............", ".XX.XS............", ".XXXXS............", "XGXGXXS.XXX...X...",
		".XXXXSXXXXXX...X..", "..XWXXXXXXXXX..X..", "WXWXXXXXWWWXXXX...", "..................",
		"..................", "..................", "..................", "..................",
	}
	lyingTail = sprite{
		".X...S............", ".XX.XS............", ".XXXXS..........S.", "XGXGXXS.XXX....S..",
		".XXXXSXXXXXX..S...", "..XWXXXXXXXXXS....", "WXWXXXXXWWWXXXS...", "..................",
		"..................", "..................", "..................", "..................",
	}
)

type Model struct {
	frame  int
	pose   Pose
	cycles map[Pose][]string
}

func New(plain bool) Model {
	sit, tail, lie, lieTail := render(sitting, plain), render(tailRight, plain), render(lying, plain), render(lyingTail, plain)
	return Model{cycles: map[Pose][]string{
		Sitting: {sit, sit, sit, tail, sit, sit},
		Lying:   {lie, lie, lieTail, lie},
	}}
}

func (m *Model) SetPose(pose Pose) { m.pose, m.frame = pose, 0 }
func (m *Model) Step()             { m.frame++ }

func (m Model) View() string {
	cycle := m.cycles[m.pose]
	return cycle[m.frame%len(cycle)]
}

func (m Model) ViewFrame(frame int) string {
	m.frame = frame
	return m.View()
}

func colorOf(symbol byte) string {
	switch symbol {
	case 'S':
		return ShadeColor
	case 'G':
		return EyeColor
	case 'W':
		return WhiteColor
	default:
		return BodyColor
	}
}

func plainOf(symbol byte) string {
	switch symbol {
	case 'W':
		return "++"
	case 'G':
		return "oo"
	default:
		return "##"
	}
}

func render(frame sprite, plain bool) string {
	var output strings.Builder
	for y, row := range frame {
		for _, symbol := range []byte(row) {
			switch {
			case symbol == '.':
				output.WriteString("  ")
			case plain:
				output.WriteString(plainOf(symbol))
			default:
				output.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(colorOf(symbol))).Render("  "))
			}
		}
		if y < len(frame)-1 {
			output.WriteByte('\n')
		}
	}
	return output.String()
}
