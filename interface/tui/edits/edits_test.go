package edits

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/subagent"
	"tofu/internal/golden"
)

const (
	gatePath    = "internal/judge/policy/toolgate.go"
	largeLines  = 1108
	deleteLines = 340
	commentMark = "/" + "/"
	firstLine   = "func sample0(value int) int {"
)

func goLine(at int) string {
	switch at % 6 {
	case 0:
		return fmt.Sprintf("func sample%d(value int) int {", at)
	case 1:
		return fmt.Sprintf("\tif value > %d {", at)
	case 2:
		return fmt.Sprintf("\t\treturn value * %d %s scaled for row %d of a generated fixture long enough to wrap", at, commentMark, at)
	case 3:
		return "\t}"
	case 4:
		return fmt.Sprintf("\treturn len(strings.Repeat(\"x\", %d))", at)
	}
	return "}"
}

func createdFile(lines int) string {
	var body strings.Builder
	for at := range lines {
		body.WriteString(goLine(at) + "\n")
	}
	return body.String()
}

func deletionDiff(path string, lines int) string {
	var body strings.Builder
	body.WriteString("--- " + path + "\n+++ " + path + "\n@@ -1," + strconv.Itoa(lines) + " +1,0 @@\n")
	for at := range lines {
		body.WriteString("-" + goLine(at) + "\n")
	}
	return body.String()
}

func mustChange(t testing.TB, agent, path, diff, created, id string) Edit {
	t.Helper()
	edit, ok := Changed(agent, path, diff, created, id, time.Date(2026, 9, 19, 14, 34, 18, 0, time.UTC))
	if !ok {
		t.Fatalf("%s is not an edit", id)
	}
	return edit
}

func changedAt(t testing.TB, agent, path, id string) Edit {
	t.Helper()
	diff := "--- " + path + "\n" +
		"+++ " + path + "\n" +
		"@@ -40,6 +40,7 @@\n" +
		" func Decide(answers Answers) Verdict {\n" +
		"-\treturn Ask\n" +
		"+\treturn AskWithReason(answers)\n" +
		" }\n"
	return mustChange(t, agent, path, diff, "", id)
}

func session(t testing.TB, width, height int) Model {
	t.Helper()
	var m Model
	m.SetSize(width, height)
	m.Root = "/repo"
	m.SubAgents = []subagent.Row{{Name: "c2"}, {Name: "c1"}}
	m.Add(changedAt(t, Self, gatePath, "call-e1a2b3"))
	m.Add(mustChange(t, "c1", "internal/rule/generated.go", "", createdFile(largeLines), "call-b7c4d1"))
	m.Add(mustChange(t, "c1", "internal/rule/legacy.go", deletionDiff("internal/rule/legacy.go", deleteLines), "", "call-44f0aa"))
	m.Add(changedAt(t, "c2", "internal/turn/loop.go", "call-9d0e1f"))
	return m
}

func reading(m Model, id string) Model {
	m.selected, m.reading, m.focusMain = id, true, true
	return m
}

func onePickedEdit(t *testing.T, root string) Model {
	var m Model
	m.SetSize(120, 24)
	m.Root = root
	m.Add(changedAt(t, "go-dev", gatePath, "e1a2b3"))
	return m
}

func TestTheIndexAt120x36(t *testing.T) {
	golden.Assert(t, "edits-index-120x36.golden", session(t, 120, 36).View())
}

func thirtyEditsOverTwelveFiles(t *testing.T, height int) Model {
	var m Model
	m.SetSize(120, height)
	m.Root = "/repo"
	order := []string{"app.ts", "cache.ts", "analytics.ts", "app.ts", "query.ts", "smoke.sh", "migrate.ts", "db.ts", "tsconfig.json", "package.json", "index.ts", "app.ts", "analytics.ts", "catalog.ts", "customers.ts", "query.ts", "smoke.sh", "migrate.ts", "db.ts", "cache.ts", "tsconfig.json", "package.json", "index.ts", "catalog.ts", "customers.ts", "query.ts", "analytics.ts", "cache.ts", "db.ts", "app.ts"}
	for at, name := range order {
		m.Add(changedAt(t, Self, "web/src/"+name, fmt.Sprintf("call-%06x", 0xa00000+at)))
	}
	return m
}

func TestTheSidebarListsEachFileOnceAndScrollsToItsLastRow(t *testing.T) {
	golden.Assert(t, "edits-sidebar-120x36.golden", thirtyEditsOverTwelveFiles(t, 36).View())
	short := thirtyEditsOverTwelveFiles(t, 16)
	short.Key("end")
	golden.Assert(t, "edits-sidebar-end-120x16.golden", short.View())
}

func TestAnOpenDiffAt120x36(t *testing.T) {
	golden.Assert(t, "edits-diff-120x36.golden", reading(session(t, 120, 36), "call-e1a2b3").View())
}

func TestAPathIsDrawnAsAnOSC8Hyperlink(t *testing.T) {
	content := onePickedEdit(t, "").View()
	want := oscStart + "file://" + gatePath + oscEnd + gatePath + oscStart + oscEnd
	if !strings.Contains(content, want) {
		t.Errorf("the path is not wrapped in an OSC 8 hyperlink\nwant substring: %q\ngot: %q", want, content)
	}
	if strings.Contains(content, "code") || strings.Contains(content, "subl") || strings.Contains(content, "zed") {
		t.Errorf("an editor is named in the rendered view\n%s", content)
	}
}

func TestATerminalWithNoOSC8SupportDrawsThePlainPath(t *testing.T) {
	stripped := ansi.Strip(onePickedEdit(t, "").View())
	if !strings.Contains(stripped, gatePath) {
		t.Errorf("stripping every escape leaves no plain path behind\n%s", stripped)
	}
	if strings.Contains(stripped, "\x1b") {
		t.Errorf("an escape byte survived stripping\n%q", stripped)
	}
}

func TestARootJoinsARelativePathIntoTheHyperlinkTarget(t *testing.T) {
	content := onePickedEdit(t, "/repo").View()
	want := oscStart + "file:///repo/" + gatePath + oscEnd
	if !strings.Contains(content, want) {
		t.Errorf("a configured root does not reach the hyperlink target\nwant substring: %q\ngot: %q", want, content)
	}
}

func TestDiffViewportFastMatchesLipgloss(t *testing.T) {
	m := session(t, 120, 36)
	for _, width := range []int{24, 45, 100, 140} {
		for _, edit := range m.edits {
			m.cache.rows = diffRowIndex{wrapped: map[int][]string{}, painted: map[int]string{}}
			rows := make([]string, 0, 24)
			for at, line := range edit.lines[:min(24, len(edit.lines))] {
				for _, row := range m.wrappedDiffRows(edit, at, width) {
					rows = append(rows, m.paintedDiffRow(line, row, len(rows), width))
				}
			}
			content := strings.Join(rows[:min(24, len(rows))], "\n")
			got, ok := look.PlainSurfaceFast(width-1, 24, 0, content)
			if !ok {
				continue
			}
			want := lipgloss.NewStyle().Width(width - 1).Height(24).Render(content)
			gotRows, wantRows := strings.Split(got, "\n"), strings.Split(want, "\n")
			for at := range gotRows {
				if at >= len(wantRows) || gotRows[at] != wantRows[at] {
					t.Fatalf("diff viewport changed for edit=%s width=%d row=%d\n got %q\nwant %q", edit.ID, width, at, gotRows[at], wantRows[at])
				}
			}
		}
	}
}

func TestDiffDialogRowsMatchFreshAcrossEvictionAndMutation(t *testing.T) {
	var created strings.Builder
	for at := range 400 {
		fmt.Fprintf(&created, "func sample%d() { return \"界🙂\" }\n", at)
	}
	edit := mustChange(t, "", "dialog.go", "", created.String(), "dialog-cache")
	m := Model{cache: &caches{}}
	for _, width := range []int{24, 60} {
		for start := 0; start < 360; start += 15 {
			got := m.dialogRows(edit, width, start, start+30)
			want := (Model{}).dialogRows(edit, width, start, start+30)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("cached dialog differs at width %d start %d", width, start)
			}
			if len(m.cache.rows.dialog.rows) > rowCacheBound {
				t.Fatal("dialog row cache exceeded bound")
			}
		}
	}
	edit.lines[350] = "-replacement"
	got := m.dialogRows(edit, 60, 345, 375)
	want := (Model{}).dialogRows(edit, 60, 345, 375)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal("dialog cache ignored same-length content mutation")
	}
}

func TestDiffRowIndexMatchesStyledWrapping(t *testing.T) {
	diff := "@@ -1,4 +1,4 @@\n+\t\t\t\t\t\tfoo\n+ 界🙂界🙂 " + strings.Repeat("word ", 20) + "\n-before\rafter\n+\x1b[31mred\x1b[0m\n"
	edit := mustChange(t, "", "all.go", diff, "", "all-lines")
	large := mustChange(t, "", "large.go", "", createdFile(60), "large")
	edit.lines, edit.numbers = append(edit.lines, large.lines...), append(edit.numbers, large.numbers...)
	for _, width := range []int{24, 45, 100} {
		m := Model{cache: &caches{}}
		offsets := m.indexedDiffRows(edit, width)
		for at, line := range edit.lines {
			want := strings.Count(lipgloss.Wrap(diffLine(line, edit.numbers[at], m.lexer(edit.Path)), max(wrapFloor, width-wrapGutter), ""), "\n") + 1
			if got := offsets[at+1] - offsets[at]; got != want {
				t.Fatalf("width %d line %q: got %d rows, want %d", width, line, got, want)
			}
		}
	}
}

func TestDiffScrollCacheMatchesFreshRender(t *testing.T) {
	m := session(t, 120, 36)
	edit := mustChange(t, "", "cache.go", "", "package main\n\tfunc example() {\nreturn 123\nreturn \"界🙂\"\n"+strings.Repeat("long word ", 22)+"\n}\n", "cache-edit")
	for _, width := range []int{24, 45, 100} {
		for _, scroll := range []int{0, 1, 2, 9, 100, 1, 0} {
			m.scroll = scroll
			got := m.completeFileDiff(width, 9, edit, 0, 1)
			fresh := m
			fresh.cache = nil
			if want := fresh.completeFileDiff(width, 9, edit, 0, 1); got != want {
				t.Fatalf("cache changed output at width %d scroll %d", width, scroll)
			}
		}
	}
}

func TestDiffCacheInvalidatesSameLengthContentAndMetadata(t *testing.T) {
	m := session(t, 120, 36)
	edit := mustChange(t, "", "cache.go", "", "original\n", "cache-edit")
	_ = m.completeFileDiff(80, 12, edit, 0, 1)
	edit.lines[0] = "+replacement " + strings.Repeat("wrapped ", 30)
	edit.op, edit.Path, edit.removed = OpDeleted, "renamed.go", 1
	got := m.completeFileDiff(80, 12, edit, 0, 1)
	fresh := m
	fresh.cache = nil
	if want := fresh.completeFileDiff(80, 12, edit, 0, 1); got != want {
		t.Fatal("same-length diff and metadata edit reused a stale cache")
	}
	edit.Path, edit.removed = "again.go", 2
	got = m.completeFileDiff(80, 12, edit, 0, 1)
	if want := fresh.completeFileDiff(80, 12, edit, 0, 1); got != want {
		t.Fatal("metadata-only edit reused a stale cache")
	}
}

func TestFileSidebarCacheInvalidatesOnVisibleChanges(t *testing.T) {
	m := reading(session(t, 120, 36), "call-b7c4d1")
	check := func(stage string) {
		t.Helper()
		got := m.View()
		uncached := m
		uncached.cache = nil
		if want := uncached.View(); got != want {
			t.Fatalf("%s: cached file sidebar differs from uncached render", stage)
		}
	}
	check("initial")
	m.scroll = 9
	check("scroll")
	m.author = "c1"
	check("author")
	m.selected = "call-44f0aa"
	check("selection")
	m.focusMain = false
	check("focus")
	m.SetSize(168, 36)
	check("width")
	m.Add(changedAt(t, "c3", "internal/turn/budget.go", "call-5a6b7c"))
	check("new edit")
	m.SubAgents = []subagent.Row{{Name: "c1"}, {Name: "c2"}}
	check("sub-agents reordered")
	m.Root, m.reading = "/elsewhere", false
	check("root")
}

func TestFileDiffCanReachFirstAndLastLineWithoutTruncation(t *testing.T) {
	cases := []struct {
		name  string
		edit  Edit
		lines int
	}{
		{"created", mustChange(t, "", "big.go", "", createdFile(largeLines), "big"), largeLines},
		{"deleted", mustChange(t, "", "gone.go", deletionDiff("gone.go", deleteLines), "", "gone"), deleteLines},
		{"five thousand", mustChange(t, "", "huge.go", "", createdFile(5000), "huge"), 5000},
	}
	for _, each := range cases {
		m := session(t, 120, 36)
		if got := each.edit.Added() + each.edit.Removed(); got != each.lines {
			t.Fatalf("%s full diff has %d of %d lines", each.name, got, each.lines)
		}
		bottom := ansi.Strip(m.completeFileDiff(85, 33, each.edit, 0, 1))
		m.scroll = 100000
		top := ansi.Strip(m.completeFileDiff(85, 33, each.edit, 0, 1))
		last := strconv.Itoa(len(each.edit.lines))
		if bottom == top || !strings.Contains(top, firstLine) || !strings.Contains(bottom, last+"/"+last) {
			t.Fatalf("%s cannot reach both ends of the diff\n%s\n%s", each.name, top, bottom)
		}
		t.Logf("%s top:    %s", each.name, strings.Split(top, "\n")[2])
		t.Logf("%s bottom: %s", each.name, strings.Split(bottom, "\n")[2])
	}
}
