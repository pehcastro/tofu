package cat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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

type clock struct{ interval time.Duration }

type tickMsg struct{ clock *clock }

type Model struct {
	clock   *clock
	frame   int
	pose    Pose
	running bool
	plain   bool
}

type Option func(*Model)

func WithFPS(fps int) Option {
	return func(m *Model) {
		if fps > 0 {
			m.clock.interval = time.Second / time.Duration(fps)
		}
	}
}

func WithPlain(plain bool) Option { return func(m *Model) { m.plain = plain } }

func New(options ...Option) Model {
	m := Model{clock: &clock{interval: 250 * time.Millisecond}, running: true}
	for _, option := range options {
		option(&m)
	}
	return m
}

func (m Model) Init() tea.Cmd { return m.tick() }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	tick, ok := msg.(tickMsg)
	if !ok || tick.clock != m.clock || !m.running {
		return m, nil
	}
	m.frame++
	return m, m.tick()
}

func (m *Model) SetPose(pose Pose) { m.pose, m.frame = pose, 0 }
func (m *Model) Pause()            { m.running = false }

func (m Model) View() string { return m.render(m.currentFrame()) }

func (m Model) ViewFrame(frame int) string {
	m.frame = frame
	return m.View()
}

func (m Model) currentFrame() sprite {
	if m.pose == Lying {
		return []sprite{lying, lying, lyingTail, lying}[m.frame%4]
	}
	return []sprite{sitting, sitting, sitting, tailRight, sitting, sitting}[m.frame%6]
}

func (m Model) tick() tea.Cmd {
	owner := m.clock
	return tea.Tick(owner.interval, func(time.Time) tea.Msg { return tickMsg{clock: owner} })
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

func (m Model) render(frame sprite) string {
	var output strings.Builder
	for y, row := range frame {
		for _, symbol := range []byte(row) {
			switch {
			case symbol == '.':
				output.WriteString("  ")
			case m.plain:
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
