package look

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TypedID(kind, id string) string {
	id = strings.Trim(id, "[]")
	if !strings.HasPrefix(id, "#") {
		id = "#" + id
	}
	return Faint("[") + Style(ReferenceType).Render(kind) + Style(ReferenceID).Bold(true).Render(id) + Faint("]")
}

func AgentRef(name string) string { return AgentRefIn(Mint, name) }

func AgentRefIn(c Color, name string) string {
	identity := strings.Trim(strings.Trim(name, "[]"), "&")
	identity = strings.ReplaceAll(strings.ReplaceAll(identity, " {", " "), "}", "")
	return Style(c).Render("[&" + identity + "]")
}

func QuietBadge(label string) string { return badge(label, MutedColor) }

func StateBadge(label string, active bool) string {
	if active {
		return badge(label, Mint)
	}
	return badge(label, MutedColor)
}

func badge(label string, fg Color) string {
	return Style(fg).Background(PanelLight.value()).Padding(0, 1).Render(label)
}

func SidebarItem(width int, selected bool, label, count string) string {
	marker, fg, bg := idleMarker, MutedColor, Panel
	if selected {
		marker, fg, bg = selectedMarker, Text, PanelLight
	}
	return Style(fg).Width(width).Background(bg.value()).Render(Sides(marker+label, count, width))
}

func SidebarEntry(width int, selected bool, label, status, description string) string {
	marker, name, bg := idleMarker, Text, Panel
	if selected {
		marker, name, bg = selectedMarker, Mint, PanelLight
	}
	line := Sides(Style(name).Bold(selected).Render(marker+label), status, width)
	first := lipgloss.NewStyle().Width(width).Background(bg.value()).Render(KeepSurfaceBackground(line, bg))
	return first + "\n  " + Muted(ansi.Truncate(description, width-2, "…"))
}

func SidebarDeltaItem(width int, selected bool, label string, added, removed int) string {
	marker, fg, bg := idleMarker, MutedColor, Panel
	if selected {
		marker, fg, bg = selectedMarker, Text, PanelLight
	}
	delta := Style(Mint).Render(SignedLines(int64(added))) + "  " + Style(Red).Render(SignedLines(-int64(removed)))
	return lipgloss.NewStyle().Width(width).Background(bg.value()).Render(Sides(Style(fg).Render(marker+label), delta, width))
}

func SettingRow(width int, selected bool, label, description, value string) string {
	marker := idleMarker
	if selected {
		marker = Accent(selectedMarker)
	}
	var control string
	switch value {
	case "on":
		control = Accent("● on")
	case "off":
		control = Faint("○ off")
	default:
		control = Style(Blue).Background(PanelLight.value()).Padding(0, 1).Render(value + "  ▾")
	}
	first := Style(Text).Width(width).Render(Sides(marker+Title(label), control, width))
	return first + "\n" + Style(MutedColor).Width(width).Render("  "+description)
}
