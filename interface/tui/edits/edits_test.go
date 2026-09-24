package edits

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/subagent"
	"tofu/internal/golden"
	roster "tofu/internal/subagent"
)

const gatePath = "internal/judge/policy/toolgate.go"

func changedAt(agent, path, id string) Edit {
	diff := "--- " + path + "\n" +
		"+++ " + path + "\n" +
		"@@ -40,6 +40,7 @@\n" +
		" func Decide(answers Answers) Verdict {\n" +
		"-\treturn Ask\n" +
		"+\treturn AskWithReason(answers)\n" +
		" }\n"
	edit, _ := Changed(agent, path, diff, "", id, time.Date(2026, 9, 19, 14, 34, 18, 0, time.UTC))
	return edit
}

func onePickedEdit(root string) Model {
	var m Model
	m.SetSize(120, 24)
	m.Root = root
	m.Add(changedAt("go-dev", gatePath, "e1a2b3"))
	return m
}

func threeAgentsEditing() Model {
	var m Model
	m.SetSize(120, 36)
	m.Root = "/repo"
	m.Busy = true
	m.Children = []subagent.Child{
		{Name: "go-docs", State: roster.Finished},
		{Name: "go-dev", State: roster.Working},
	}
	m.Add(changedAt(Self, gatePath, "e1a2b3"))
	m.Add(changedAt("go-dev", "internal/rule/parse.go", "b7c4d1"))
	m.Add(changedAt("go-docs", "library/changelog/CHANGELOG.md", "44f0aa"))
	return m
}

func TestTheSidebarKeepsItsOwnWidth(t *testing.T) {
	golden.Assert(t, "edits-120x36.golden", threeAgentsEditing().View())
}

func TestAPathIsDrawnAsAnOSC8Hyperlink(t *testing.T) {
	content := onePickedEdit("").View()
	want := oscStart + "file://" + gatePath + oscEnd + gatePath + oscStart + oscEnd
	if !strings.Contains(content, want) {
		t.Errorf("the path is not wrapped in an OSC 8 hyperlink\nwant substring: %q\ngot: %q", want, content)
	}
	if strings.Contains(content, "code") || strings.Contains(content, "subl") || strings.Contains(content, "zed") {
		t.Errorf("an editor is named in the rendered view\n%s", content)
	}
}

func TestATerminalWithNoOSC8SupportDrawsThePlainPath(t *testing.T) {
	stripped := ansi.Strip(onePickedEdit("").View())
	if !strings.Contains(stripped, gatePath) {
		t.Errorf("stripping every escape leaves no plain path behind\n%s", stripped)
	}
	if strings.Contains(stripped, "\x1b") {
		t.Errorf("an escape byte survived stripping\n%q", stripped)
	}
}

func TestEachChangeNamesWhoWhereWhenAndItsID(t *testing.T) {
	stripped := ansi.Strip(onePickedEdit("").View())
	for _, want := range []string{"go-dev edited", gatePath, "14:34:18", "#e1a2b3"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("a change does not name %q\n%s", want, stripped)
		}
	}
}

func TestARootJoinsARelativePathIntoTheHyperlinkTarget(t *testing.T) {
	content := onePickedEdit("/repo").View()
	want := oscStart + "file:///repo/" + gatePath + oscEnd
	if !strings.Contains(content, want) {
		t.Errorf("a configured root does not reach the hyperlink target\nwant substring: %q\ngot: %q", want, content)
	}
}
