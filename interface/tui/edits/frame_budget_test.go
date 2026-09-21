package edits

import (
	"strconv"
	"testing"
	"time"

	"tofu/interface/tui/frametime"
)

func TestFrameBudgetWithAFullFeedOfEdits(t *testing.T) {
	var m Model
	m.SetSize(120, 36)
	m.Root = "/repo"
	for index := range editWindow {
		path := "internal/pkg" + strconv.Itoa(index%40) + "/file.go"
		diff := "--- " + path + "\n+++ " + path + "\n@@ -1,3 +1,4 @@\n func f() {\n-\treturn old(x)\n+\treturn new(x)\n }\n"
		edit, _ := Changed("go-dev", path, diff, "", strconv.Itoa(index), time.Now())
		m.Add(edit)
	}
	m.View()
	frametime.Frames(t, "file edits, "+strconv.Itoa(editWindow)+" entries", func() { m.View() })
}
