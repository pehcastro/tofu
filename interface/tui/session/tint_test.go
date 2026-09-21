package session

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s does not match the golden file\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func downsampled(profile colorprofile.Profile, frame string) string {
	var buf bytes.Buffer
	writer := &colorprofile.Writer{Forward: &buf, Profile: profile}
	_, _ = writer.WriteString(frame)
	return buf.String()
}

func composerFrame(columns int, focus bool, typed string) string {
	model := New(fixed(), counted(new(int)))
	model.SetSize(columns, 24)
	if focus {
		model.Focus()
	}
	for _, glyph := range typed {
		model.Update(tea.KeyPressMsg{Code: glyph, Text: string(glyph)})
	}
	model.Append(Entry{Kind: Assistant, Body: "ready"})
	return model.View()
}

func TestComposerTintGolden(t *testing.T) {
	assertGolden(t, "composer-tint-truecolor-80x24.golden", composerFrame(80, true, ""))
}

func TestComposerTintGoldenBlurred(t *testing.T) {
	assertGolden(t, "composer-tint-blurred-truecolor-80x24.golden", composerFrame(80, false, ""))
}

func TestABlankTintedRowSitsAboveAndBelowTheComposersText(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Focus()
	empty := model.View()
	assertGolden(t, "composer-gap-empty-80x24.golden", empty)

	model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	twoLines := model.View()
	assertGolden(t, "composer-gap-two-lines-80x24.golden", twoLines)

	rule := strings.Repeat("─", 80)
	for _, frame := range []string{empty, twoLines} {
		if strings.Contains(frame, rule) {
			t.Fatalf("a rule still draws above the composer:\n%s", frame)
		}
		block := composerBlock(t, frame)
		for _, row := range []string{block[0], block[len(block)-1]} {
			if strings.TrimSpace(ansi.Strip(row)) != "" {
				t.Fatalf("the gap row carries text: %q", row)
			}
			if !strings.Contains(row, tintEscape()) {
				t.Fatalf("the gap row carries no tint: %q", row)
			}
		}
	}
}

var sgrSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

func cellBackgrounds(row string) []string {
	var cells []string
	background, from := "", 0
	paint := func(text string) {
		for _, glyph := range text {
			for range widget.Cells(string(glyph)) {
				cells = append(cells, background)
			}
		}
	}
	for _, span := range sgrSequence.FindAllStringIndex(row, -1) {
		paint(row[from:span[0]])
		background = afterSGR(background, row[span[0]:span[1]])
		from = span[1]
	}
	paint(row[from:])
	return cells
}

func afterSGR(background, sequence string) string {
	params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m"), ";")
	for index := 0; index < len(params); {
		code, _ := strconv.Atoi(params[index])
		span := 1
		if code == 48 && index+1 < len(params) {
			span = 3
			if params[index+1] == "2" {
				span = 5
			}
		}
		switch {
		case params[index] == "" || code == 0 || code == 49:
			background = ""
		case code == 48 || (code >= 40 && code <= 47) || (code >= 100 && code <= 107):
			background = strings.Join(params[index:min(index+span, len(params))], ";")
		}
		index += span
	}
	return background
}

func tintEscape() string {
	escape, _, _ := strings.Cut(lipgloss.NewStyle().Background(theme.ComposerColor()).Render("X"), "X")
	return escape
}

func composerBlock(t *testing.T, frame string) []string {
	t.Helper()
	rows := strings.Split(frame, "\n")
	first := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, tintEscape()) })
	if first < 0 || first+composerRows+tintPadRows > len(rows) {
		t.Fatalf("no composer block found in frame:\n%s", frame)
	}
	return rows[first : first+composerRows+tintPadRows]
}

func TestEveryCellOfEveryComposerRowCarriesTheTint(t *testing.T) {
	tint := cellBackgrounds(tintEscape() + "X")[0]
	texts := map[string]string{
		"empty":    "",
		"one line": "read the gate",
		"wrapping": strings.Repeat("the composer wraps this line more than once ", 5),
	}
	for _, columns := range []int{80, 120} {
		for name, typed := range texts {
			for index, row := range composerBlock(t, composerFrame(columns, true, typed)) {
				cells := cellBackgrounds(row)
				if len(cells) != columns {
					t.Errorf("%s at %d columns: composer row %d draws %d cells, want %d", name, columns, index, len(cells), columns)
				}
				for column, background := range cells {
					if background != tint {
						t.Errorf("%s at %d columns: composer row %d column %d has background %q, want the tint %q", name, columns, index, column, background, tint)
						break
					}
				}
				if edge := ansi.Cut(ansi.Strip(row), 0, len(composerInset)); edge != composerInset {
					t.Errorf("%s at %d columns: composer row %d opens with %q, want %q and no border glyph", name, columns, index, edge, composerInset)
				}
			}
		}
	}
}

func TestComposerTintReadsAtSixteenColours(t *testing.T) {
	frame := downsampled(colorprofile.ANSI, composerFrame(80, true, ""))
	assertGolden(t, "composer-tint-ansi16-80x24.golden", frame)
	painted := downsampled(colorprofile.ANSI, lipgloss.NewStyle().Background(theme.ComposerColor()).Render("X"))
	escape, _, _ := strings.Cut(painted, "X")
	if !bytes.Contains([]byte(frame), []byte(escape)) {
		t.Fatalf("the composer tint does not survive a sixteen colour terminal, want %q in:\n%s", escape, frame)
	}
}
