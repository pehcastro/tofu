package welcome

import (
	"math"
	"os"
	"slices"
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
	stageWidth        = 68
	stageHeight       = 14
	canvasWidth       = 30
	canvasHeight      = 12
	pulseInterval     = 120 * time.Millisecond
	pulsesPerCatFrame = 2
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
	cat    cat.Model
	pose   int
	pulse  int
	ascii  bool
	pixels map[[2]string]string
}

func NewIdentity() Identity {
	ascii := os.Getenv("TOFU_ASCII") != "" || os.Getenv("TERM") == "dumb"
	return Identity{ascii: ascii, cat: cat.New(ascii || os.Getenv("NO_COLOR") != ""), pixels: map[[2]string]string{}}
}

func (i Identity) Init() tea.Cmd {
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg { return pulseMsg{} })
}

func (i Identity) Update(msg tea.Msg) (Identity, tea.Cmd) {
	if _, ok := msg.(pulseMsg); !ok {
		return i, nil
	}
	i.pulse++
	if i.pulse%pulsesPerCatFrame == 0 {
		i.cat.Step()
	}
	return i, i.Init()
}

func (i *Identity) TogglePose() {
	i.pose = (i.pose + 1) % len(poses)
	i.cat.SetPose(poses[i.pose].cat)
}

func (i Identity) PoseName() string { return poses[i.pose].name }

func (i Identity) View(width int) string {
	current := poses[i.pose]
	stage := place(width, stageHeight, i.cat.View(), current.x, current.y)
	return stage + "\n" + strings.Join(centered(width, 0, i.logo()), "\n")
}

func (i Identity) Fit(width, rows int) []string {
	fits := func(art string) bool { return lipgloss.Width(art) <= width && lipgloss.Height(art) <= rows }
	art := i.View(min(stageWidth, width))
	if !fits(art) {
		art = compactIdentity(art)
	}
	if !fits(art) {
		art = i.logo()
	}
	if !fits(art) {
		return nil
	}
	return centered(width, rows, art)
}

func centered(width, rows int, art string) []string {
	lines := strings.Split(art, "\n")
	widths, block := make([]int, len(lines)), 0
	for n, line := range lines {
		widths[n] = ansi.StringWidth(line)
		block = max(block, widths[n])
	}
	if block < width {
		for n, line := range lines {
			gap := width - widths[n]
			right := int(math.Round(float64(gap) / 2))
			lines[n] = strings.Repeat(" ", gap-right) + line + strings.Repeat(" ", right)
		}
		block = width
	}
	if gap := rows - len(lines); gap > 0 {
		bottom := int(math.Round(float64(gap) / 2))
		blank := []string{strings.Repeat(" ", block)}
		lines = slices.Concat(slices.Repeat(blank, gap-bottom), lines, slices.Repeat(blank, bottom))
	}
	return lines
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
	return halfBlocks(wordmarkCanvas(i.pulse), i.pixel)
}

func (i Identity) pixel(upper, lower string) string {
	key := [2]string{upper, lower}
	if _, ok := i.pixels[key]; !ok {
		i.pixels[key] = pixelPair(upper, lower)
	}
	return i.pixels[key]
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
