package markdown

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
)

func TestReportBodyRendersWithoutMarkup(t *testing.T) {
	body := "## Files written\n\n- `src/index.ts`: the entry point\n- `src/greet.ts`: exports `greet`\n\n```\n$ bun -e 'console.log(require(\"./src/greet.ts\").greet(\"tofu\"))'\nhello tofu\n```\n"
	var red, green, blue int
	if _, err := fmt.Sscanf(string(look.SyntaxString), "#%02x%02x%02x", &red, &green, &blue); err != nil {
		t.Fatal(err)
	}
	stringColour := fmt.Sprintf("38;2;%d;%d;%d", red, green, blue)
	var renderer Renderer
	lines := renderer.Lines(body, 80)
	highlighted := false
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "##") || strings.Contains(plain, "`") {
			t.Errorf("markup left in %q", plain)
		}
		if strings.Contains(plain, "bun -e") && strings.Contains(line, stringColour) {
			highlighted = true
		}
	}
	if !highlighted {
		t.Errorf("fence code has no theme string colour:\n%q", lines)
	}
}
