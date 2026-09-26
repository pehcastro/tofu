package cover

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCompactWelcomeKeepsCatAndComposer(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TOFU_ASCII", "")
	t.Setenv("TERM", "xterm-truecolor")
	identity := NewIdentity(false)
	for _, pose := range []string{"sitting", "lying"} {
		view := WelcomeInputView(identity, 60, 20, "draft", 0, "Type to start")
		if lipgloss.Width(view) > 60 || lipgloss.Height(view) > 20 {
			t.Fatalf("%s: compact welcome is %dx%d", pose, lipgloss.Width(view), lipgloss.Height(view))
		}
		if !strings.Contains(view, "48;2;68;74;84") || !strings.Contains(ansi.Strip(view), "› draft") {
			t.Fatalf("%s: compact welcome lost the cat or composer", pose)
		}
		identity.TogglePose()
	}
}
