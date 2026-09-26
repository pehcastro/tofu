package look

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
)

var sentinelRoles = []Color{Background, Panel, PanelLight, Text, MutedColor, FaintColor, Mint, Blue, Amber, Red, Violet, ReferenceType, ReferenceID, DiffAddBackground, DiffDeleteBackground}

var syntaxSentinels = []Color{SyntaxKeyword, SyntaxFunction, SyntaxString, SyntaxComment, SyntaxNumber}

func applyThemeLegacy(view string, theme Theme) string {
	if theme == ThemeTerminal {
		return Apply(view, theme)
	}
	p, ok := palettes[theme]
	if !ok {
		return view
	}
	canvas := "\x1b[" + sgr(sgrBackground, Background) + "m"
	view = canvas + view
	view = strings.ReplaceAll(view, "\x1b[m", "\x1b[m"+canvas)
	view = strings.ReplaceAll(view, "\x1b[0m", "\x1b[0m"+canvas)
	view = strings.ReplaceAll(view, "\x1b[49m", canvas)
	for old, replacement := range p.roles {
		for _, mode := range []int{sgrForeground, sgrBackground} {
			view = strings.ReplaceAll(view, sgr(mode, old), sgr(mode, replacement))
		}
	}
	for i, role := range syntaxSentinels {
		view = strings.ReplaceAll(view, sgr(sgrForeground, role), sgr(sgrForeground, p.syntax[i]))
	}
	return view
}

func dimUnderlayLegacy(view string) string {
	for _, fg := range []Color{Text, MutedColor, Mint, Amber, Red, Violet, ReferenceType, ReferenceID, SyntaxKeyword, SyntaxFunction, SyntaxString, SyntaxComment, SyntaxNumber} {
		view = strings.ReplaceAll(view, sgr(sgrForeground, fg), sgr(sgrForeground, FaintColor))
	}
	for _, bg := range []Color{Panel, PanelLight} {
		view = strings.ReplaceAll(view, sgr(sgrBackground, bg), sgr(sgrBackground, Background))
	}
	return view
}

func themeFixture() string {
	var output strings.Builder
	for range 200 {
		output.WriteString("row ")
		output.WriteString(Style(Text).Render("plain text "))
		output.WriteString(Style(SyntaxKeyword).Render("keyword "))
		output.WriteString(Style(Mint).Render("active "))
		output.WriteString("\x1b[0m\x1b[49m\x1b[m")
		output.WriteString(Style(DiffAddBackground).Render("+ added"))
		output.WriteByte('\n')
	}
	return output.String()
}

func TestApplyThemeMatchesLegacy(t *testing.T) {
	var fixture strings.Builder
	fixture.WriteString(themeFixture())
	for _, old := range append(slices.Clone(sentinelRoles), syntaxSentinels...) {
		for _, mode := range []int{sgrForeground, sgrBackground} {
			fixture.WriteString("\x1b[" + sgr(mode, old) + "mX")
		}
	}
	for _, theme := range append(Themes(), "unrecognized") {
		if got, want := Apply(fixture.String(), theme), applyThemeLegacy(fixture.String(), theme); got != want {
			t.Errorf("theme %q differs from old mapping", theme)
		}
	}
	if got, want := Dim(fixture.String()), dimUnderlayLegacy(fixture.String()); got != want {
		t.Error("dimmed underlay differs from old mapping")
	}
}

func TestDefaultTofuThemePaintsCanvasAndSyntax(t *testing.T) {
	view := Painted("panel", Text, Panel) + "\n" + Style(SyntaxKeyword).Render("func") + Style(SyntaxFunction).Render(" render")
	tofu := Apply(view, ThemeTofu)
	for _, want := range []string{sgr(sgrBackground, "#191622"), sgr(sgrBackground, "#2f343e"), sgr(sgrForeground, "#ff79c6"), sgr(sgrForeground, "#78d1e1")} {
		if !strings.Contains(tofu, want) {
			t.Fatalf("Nu Disco canvas/syntax role %q is missing", want)
		}
	}
	nord := Apply(view, ThemeNord)
	if !strings.Contains(nord, sgr(sgrBackground, "#2e3440")) || !strings.Contains(nord, sgr(sgrBackground, "#353c49")) {
		t.Fatal("Nord did not recolor the canvas and panels")
	}
}

func TestEveryNamedThemeChangesTheWholeCanvas(t *testing.T) {
	view := "blank " + Painted("panel", Text, Panel) + "\x1b[m tail"
	for theme, p := range palettes {
		if !slices.Contains(Themes(), theme) {
			t.Errorf("theme %q is not listed by Themes", theme)
		}
		for _, role := range sentinelRoles {
			if p.roles[role] == "" {
				t.Errorf("theme %q lacks role %q", theme, role)
			}
		}
		for _, syntax := range p.syntax {
			if syntax == "" {
				t.Errorf("theme %q lacks a syntax color", theme)
			}
		}
		canvas, ok := Canvas(theme).(color.RGBA)
		if !ok {
			t.Errorf("theme %q did not set the terminal canvas", theme)
			continue
		}
		if got, want := fmt.Sprintf("48;2;%d;%d;%d", canvas.R, canvas.G, canvas.B), sgr(sgrBackground, p.roles[Background]); got != want {
			t.Errorf("theme %q canvas = %q, want %q", theme, got, want)
		}
		if !strings.Contains(Apply(view, theme), sgr(sgrBackground, p.roles[Background])) {
			t.Errorf("theme %q did not paint blank cells", theme)
		}
	}
	if Canvas(ThemeTerminal) != nil {
		t.Fatal("terminal theme should restore the terminal's own canvas")
	}
}

func BenchmarkApplyTheme(b *testing.B) {
	fixture := themeFixture()
	for _, name := range []string{"legacy", "compiled"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if name == "legacy" {
					_ = applyThemeLegacy(fixture, ThemeTofu)
				} else {
					_ = Apply(fixture, ThemeTofu)
				}
			}
		})
	}
}

func BenchmarkDimUnderlay(b *testing.B) {
	fixture := themeFixture()
	for _, name := range []string{"legacy", "compiled"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if name == "legacy" {
					_ = dimUnderlayLegacy(fixture)
				} else {
					_ = Dim(fixture)
				}
			}
		})
	}
}
