package cover

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/cat"
)

const mint = "#40d294"

var silver = [...]string{"#aeb4bc", "#9ca3ad", "#8a929d", "#78818d", "#666f7c"}

const (
	stageWidth    = 68
	stageHeight   = 14
	canvasWidth   = 30
	canvasHeight  = 12
	catFPS        = 4
	pulseInterval = 120 * time.Millisecond
)

type pose struct {
	name string
	cat  cat.Pose
	x, y int
}

var poses = [...]pose{
	{name: "sitting", cat: cat.Sitting, x: -17, y: 2},
	{name: "lying", cat: cat.Lying, x: -11, y: 6},
}

var canonicalTofu = [9]string{
	"..XX...........XXXX.......",
	"XXXXXX..XXXX..XXXXX.XX..XX",
	"XXXXXX.XXXXXX.XX....XX..XX",
	"..XX...XX..XX.XX....XX..XX",
	"..XX...XX..XX.XXXX..XX..XX",
	"..XX...XX..XX.XXXX..XX..XX",
	"..XX...XX..XX.XX....XX..XX",
	"..XXXX.XXXXXX.XX....XXXXXX",
	"...XXX..XXXX..XX.....XXXXX",
}

type pulseMsg struct{}

type Identity struct {
	cat     cat.Model
	pose    int
	pulse   int
	ascii   bool
	started bool
}

func NewIdentity(forceASCII bool) Identity {
	ascii := forceASCII || os.Getenv("TOFU_ASCII") != "" || os.Getenv("TERM") == "dumb"
	return Identity{ascii: ascii, cat: cat.New(
		cat.WithFPS(catFPS),
		cat.WithPlain(ascii || os.Getenv("NO_COLOR") != ""),
	)}
}

func (i *Identity) Init() tea.Cmd {
	if i.started {
		return nil
	}
	i.started = true
	return tea.Batch(i.cat.Init(), pulse())
}

func pulse() tea.Cmd {
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg { return pulseMsg{} })
}

func (i Identity) Update(msg tea.Msg) (Identity, tea.Cmd) {
	var pulseCommand, catCommand tea.Cmd
	if _, ok := msg.(pulseMsg); ok {
		i.pulse++
		pulseCommand = pulse()
	}
	i.cat, catCommand = i.cat.Update(msg)
	return i, tea.Batch(pulseCommand, catCommand)
}

func (i *Identity) TogglePose() {
	i.pose = (i.pose + 1) % len(poses)
	i.cat.SetPose(poses[i.pose].cat)
}

func (i Identity) PoseName() string { return poses[i.pose].name }

func (i Identity) View(width int) string {
	current := poses[i.pose]
	stage := place(width, stageHeight, i.cat.View(), current.x, current.y)
	return stage + "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, i.logo())
}

func (i Identity) Fit(width, rows int) []string {
	whole := i.View(min(stageWidth, width))
	for _, art := range []string{whole, compactIdentity(whole), i.logo()} {
		if lipgloss.Width(art) <= width && lipgloss.Height(art) <= rows {
			return strings.Split(lipgloss.Place(width, rows, lipgloss.Center, lipgloss.Center, art), "\n")
		}
	}
	return nil
}

func compactIdentity(view string) string {
	var rows []string
	for _, row := range strings.Split(view, "\n") {
		if strings.TrimSpace(ansi.Strip(row)) != "" || strings.Contains(row, "\x1b[48;") {
			rows = append(rows, row)
		}
	}
	return strings.Join(rows, "\n")
}

func (i Identity) logo() string {
	if i.ascii {
		return asciiWordmark(i.pulse)
	}
	return wordmark(i.pulse)
}

func place(width, height int, content string, offsetX, offsetY int) string {
	lines := strings.Split(content, "\n")
	for len(lines) > 1 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	contentWidth := lipgloss.Width(content)
	left := max(0, min(width-contentWidth, (width-contentWidth)/2+offsetX))
	top := max(0, min(height-len(lines), offsetY))
	stage := make([]string, height)
	for index, line := range lines {
		stage[top+index] = strings.Repeat(" ", left) + line
	}
	return strings.Join(stage, "\n")
}

func wordmarkCanvas(frame int) [][]string {
	canvas := make([][]string, canvasHeight)
	for row := range canvas {
		canvas[row] = make([]string, canvasWidth)
	}
	for y, row := range canonicalTofu {
		for x := range row {
			if row[x] == 'X' {
				canvas[y+1][x+2] = silver[y/2]
			}
		}
	}
	path := orbit(canvasWidth, canvasHeight)
	point := path[frame%len(path)]
	canvas[point[1]][point[0]] = mint
	return canvas
}

func orbit(width, height int) [][2]int {
	path := make([][2]int, 0, 2*(width+height)-4)
	for x := 0; x < width; x++ {
		path = append(path, [2]int{x, 0})
	}
	for y := 1; y < height; y++ {
		path = append(path, [2]int{width - 1, y})
	}
	for x := width - 2; x >= 0; x-- {
		path = append(path, [2]int{x, height - 1})
	}
	for y := height - 2; y > 0; y-- {
		path = append(path, [2]int{0, y})
	}
	return path
}

func wordmark(frame int) string {
	return halfBlocks(wordmarkCanvas(frame), pixelPair)
}

func asciiWordmark(frame int) string {
	return halfBlocks(wordmarkCanvas(frame), func(upper, lower string) string {
		if upper == "" && lower == "" {
			return " "
		}
		return "#"
	})
}

func halfBlocks(canvas [][]string, cell func(upper, lower string) string) string {
	var output strings.Builder
	for top := 0; top < len(canvas); top += 2 {
		for x := range canvas[top] {
			output.WriteString(cell(canvas[top][x], canvas[top+1][x]))
		}
		if top+2 < len(canvas) {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func pixelPair(upper, lower string) string {
	switch {
	case upper == "" && lower == "":
		return " "
	case upper == lower:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(upper)).Render("█")
	case upper != "" && lower != "":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(upper)).Background(lipgloss.Color(lower)).Render("▀")
	case upper != "":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(upper)).Render("▀")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(lower)).Render("▄")
	}
}
