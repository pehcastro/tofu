package look

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type Theme string

const (
	ThemeTofu           Theme = "tofu"
	ThemeTofuLight      Theme = "tofu light"
	ThemeTofuDusk       Theme = "tofu dusk"
	ThemeSolarizedLight Theme = "Solarized Light"
	ThemeDracula        Theme = "Dracula"
	ThemeNord           Theme = "Nord"
	ThemeGruvbox        Theme = "Gruvbox"
	ThemeTerminal       Theme = "terminal"
)

type palette struct {
	roles  map[Color]Color
	syntax [5]Color
}

type transform struct {
	canvas   string
	replacer *strings.Replacer
}

var palettes = map[Theme]palette{
	ThemeTofu: {
		roles:  map[Color]Color{Background: "#191622", Panel: "#2f343e", PanelLight: "#454a56", Text: "#e1e1e6", MutedColor: "#a4aab5", FaintColor: "#696b77", Mint: "#67e480", MintMuted: "#48925a", Blue: "#74ade8", Amber: "#dec184", Red: "#d07277", Violet: "#988bc7", ReferenceType: "#aa9acb", ReferenceID: "#c3b4df", DiffAddBackground: "#20372b", DiffDeleteBackground: "#3a2529"},
		syntax: [5]Color{"#ff79c6", "#78d1e1", "#e7de79", "#878e98", "#78d1e1"},
	},
	ThemeTofuLight: {
		roles:  map[Color]Color{Background: "#f5f3f0", Panel: "#eae6e3", PanelLight: "#d8d2ce", Text: "#302b35", MutedColor: "#5c5864", FaintColor: "#77717e", Mint: "#247b59", MintMuted: "#78ab95", Blue: "#315f99", Amber: "#916725", Red: "#aa4556", Violet: "#65559b", ReferenceType: "#67508f", ReferenceID: "#503872", DiffAddBackground: "#deeee3", DiffDeleteBackground: "#f3dfe1"},
		syntax: [5]Color{"#93476b", "#32618b", "#7a6220", "#6d7580", "#356d83"},
	},
	ThemeTofuDusk: {
		roles:  map[Color]Color{Background: "#211b29", Panel: "#302737", PanelLight: "#46374a", Text: "#eee5e9", MutedColor: "#b3a5ad", FaintColor: "#82737e", Mint: "#8bcda5", MintMuted: "#618673", Blue: "#9dbbd9", Amber: "#dfbd91", Red: "#d89197", Violet: "#b6a0cf", ReferenceType: "#bda9ce", ReferenceID: "#d1bde0", DiffAddBackground: "#293d36", DiffDeleteBackground: "#442d37"},
		syntax: [5]Color{"#e4a1bd", "#9bc8d6", "#ddc58f", "#958896", "#a0c9d2"},
	},
	ThemeSolarizedLight: {
		roles:  map[Color]Color{Background: "#fdf6e3", Panel: "#eee8d5", PanelLight: "#ded7c0", Text: "#073642", MutedColor: "#586e75", FaintColor: "#657b83", Mint: "#2b806a", MintMuted: "#7faf9a", Blue: "#268bd2", Amber: "#987000", Red: "#c63c32", Violet: "#6c71c4", ReferenceType: "#645da3", ReferenceID: "#4b528e", DiffAddBackground: "#dcece2", DiffDeleteBackground: "#f3ddd4"},
		syntax: [5]Color{"#a04f75", "#26759a", "#806d1c", "#657b83", "#5e7499"},
	},
	ThemeDracula: {
		roles:  map[Color]Color{Background: "#282a36", Panel: "#343746", PanelLight: "#44475a", Text: "#f8f8f2", MutedColor: "#a6a8b7", FaintColor: "#696d80", Mint: "#50c9a7", MintMuted: "#40897a", Blue: "#8be9fd", Amber: "#f1fa8c", Red: "#ff6e6e", Violet: "#bd9bd8", ReferenceType: "#b69bcc", ReferenceID: "#cdb1df", DiffAddBackground: "#263d36", DiffDeleteBackground: "#463039"},
		syntax: [5]Color{"#ff79c6", "#8be9fd", "#f1fa8c", "#6272a4", "#bd93f9"},
	},
	ThemeNord: {
		roles:  map[Color]Color{Background: "#2e3440", Panel: "#353c49", PanelLight: "#434c5e", Text: "#eceff4", MutedColor: "#a5b0c0", FaintColor: "#687789", Mint: "#8fbcbb", MintMuted: "#68868a", Blue: "#88c0d0", Amber: "#ebcb8b", Red: "#bf616a", Violet: "#b48ead", ReferenceType: "#a989b3", ReferenceID: "#c3a0c9", DiffAddBackground: "#334844", DiffDeleteBackground: "#49383e"},
		syntax: [5]Color{"#81a1c1", "#88c0d0", "#a3be8c", "#79889c", "#b48ead"},
	},
	ThemeGruvbox: {
		roles:  map[Color]Color{Background: "#282828", Panel: "#32302f", PanelLight: "#3c3836", Text: "#ebdbb2", MutedColor: "#a89984", FaintColor: "#665c54", Mint: "#8ec07c", MintMuted: "#65835a", Blue: "#83a598", Amber: "#fabd2f", Red: "#fb4934", Violet: "#d3869b", ReferenceType: "#b489a4", ReferenceID: "#d8a6bc", DiffAddBackground: "#35442f", DiffDeleteBackground: "#4a302b"},
		syntax: [5]Color{"#fb4934", "#8ec07c", "#b8bb26", "#928374", "#d3869b"},
	},
}

var transforms = compileTransforms()

var dimReplacer = compileDim()

func compileTransforms() map[Theme]transform {
	paletteRoles := [...]Color{Background, Panel, PanelLight, Text, MutedColor, FaintColor, Mint, MintMuted, Blue, Amber, Red, Violet, ReferenceType, ReferenceID, DiffAddBackground, DiffDeleteBackground}
	syntaxRoles := [5]Color{SyntaxKeyword, SyntaxFunction, SyntaxString, SyntaxComment, SyntaxNumber}
	compiled := make(map[Theme]transform, len(palettes))
	for theme, p := range palettes {
		canvas := "\x1b[" + sgr(sgrBackground, p.roles[Background]) + "m"
		pairs := []string{"\x1b[m", "\x1b[m" + canvas, "\x1b[0m", "\x1b[0m" + canvas, "\x1b[49m", canvas}
		for _, sentinel := range paletteRoles {
			for _, mode := range []int{sgrForeground, sgrBackground} {
				pairs = append(pairs, sgr(mode, sentinel), sgr(mode, p.roles[sentinel]))
			}
		}
		for i, role := range syntaxRoles {
			pairs = append(pairs, sgr(sgrForeground, role), sgr(sgrForeground, p.syntax[i]))
		}
		compiled[theme] = transform{canvas: canvas, replacer: strings.NewReplacer(pairs...)}
	}
	return compiled
}

func compileDim() *strings.Replacer {
	var pairs []string
	for _, fg := range []Color{Text, MutedColor, Mint, MintMuted, Amber, Red, Violet, ReferenceType, ReferenceID, SyntaxKeyword, SyntaxFunction, SyntaxString, SyntaxComment, SyntaxNumber} {
		pairs = append(pairs, sgr(sgrForeground, fg), sgr(sgrForeground, FaintColor))
	}
	for _, bg := range []Color{Panel, PanelLight} {
		pairs = append(pairs, sgr(sgrBackground, bg), sgr(sgrBackground, Background))
	}
	return strings.NewReplacer(pairs...)
}

func Apply(view string, theme Theme) string {
	if theme == ThemeTerminal {
		return ansi.Strip(view)
	}
	t, ok := transforms[theme]
	if !ok {
		return view
	}
	var out strings.Builder
	out.Grow(len(view) + len(t.canvas))
	out.WriteString(t.canvas)
	_, _ = t.replacer.WriteString(&out, view)
	return out.String()
}

func Dim(view string) string { return dimReplacer.Replace(view) }

func Canvas(theme Theme) color.Color {
	p, ok := palettes[theme]
	if !ok {
		return nil
	}
	return p.roles[Background].rgb()
}
