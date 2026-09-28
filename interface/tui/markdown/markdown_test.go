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

func TestCodeBlocksInAMessageCarryTheCodeRule(t *testing.T) {
	rule := look.Style(look.FaintColor).Render(codeRule)
	for name, body := range map[string]string{
		"indented": "My own verification:\n\n    tsc exit 0\n    /api/health 200 {\"ok\":true}\n\nAll routes answer.",
		"fenced":   "My own verification:\n\n```\ntsc exit 0\n/api/health 200 {\"ok\":true}\n```\n\nAll routes answer.",
	} {
		var renderer Renderer
		lines := renderer.Lines(body, 80)
		ruled := 0
		for _, line := range lines {
			plain := ansi.Strip(line)
			code := strings.Contains(plain, "tsc exit 0") || strings.Contains(plain, "/api/health 200")
			switch {
			case code && !strings.HasPrefix(line, rule):
				t.Errorf("%s: code line %q does not start with the faint code rule", name, line)
			case code:
				ruled++
			case strings.HasPrefix(line, rule):
				t.Errorf("%s: prose line %q carries the code rule", name, plain)
			}
		}
		if ruled != 2 {
			t.Errorf("%s: %d code lines carry the rule, want 2:\n%q", name, ruled, lines)
		}
	}
}

func TestShortTableStaysCompactWithABoldHeaderAndNoOrange(t *testing.T) {
	body := "My own verification:\n\n| check | result |\n|---|---|\n| `bunx tsc --noEmit` whole project | 0 errors |\n| orders/999999 | 404 `{\"error\":\"not found\"}` |\n| unknown path | 404 |\n\nVERIFIED."
	var renderer Renderer
	lines := renderer.Lines(body, 120)
	header := -1
	for index, line := range lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "check") && strings.Contains(plain, "result") {
			header = index
		}
		if width := ansi.StringWidth(line); width >= 70 && !strings.Contains(plain, "verification") && !strings.Contains(plain, "VERIFIED") {
			t.Errorf("table line is %d cells wide, want under 70: %q", width, plain)
		}
		if orange(line) {
			t.Errorf("orange escape in %q", line)
		}
	}
	if header < 0 {
		t.Fatalf("no header line:\n%q", lines)
	}
	if !strings.Contains(lines[header], "\x1b[1m") && !strings.Contains(lines[header], "\x1b[1;") && !strings.Contains(lines[header], ";1m") && !strings.Contains(lines[header], ";1;") {
		t.Errorf("header is not bold: %q", lines[header])
	}
}

func orange(line string) bool {
	for _, part := range strings.Split(line, "\x1b[") {
		var index, red, green, blue int
		if n, _ := fmt.Sscanf(part, "38;5;%d", &index); n == 1 && (index == 130 || index == 166 || index == 172 || index == 173 || index == 202 || index == 208 || index == 209 || index == 214 || index == 215 || index == 216) {
			return true
		}
		if n, _ := fmt.Sscanf(part, "38;2;%d;%d;%d", &red, &green, &blue); n == 3 && red > 180 && green > 60 && green < 170 && blue < 90 {
			return true
		}
	}
	return false
}
