package links

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	isession "tofu/internal/session"
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

func carried() []Link {
	return []Link{
		{URL: "https://go.dev/dl/go1.25.1.windows-amd64.zip", From: "bash", Count: 3},
		{URL: "https://en.wikipedia.org/wiki/Go_(programming_language)", From: "the answer", Count: 1},
		{URL: "https://pkg.go.dev/net/url", From: "you", Count: 2},
		{URL: "http://localhost:8080/health", From: "a tool result", Count: 1},
	}
}

func picker() Model {
	var built Model
	built.SetSize(80, 24)
	built.Set(carried(), "")
	return built
}

func TestThePickerGolden(t *testing.T) {
	built := picker()
	assertGolden(t, "links-80x24.golden", built.View())
}

func TestAnEmptySessionOpensThePickerAndSaysThereAreNoLinks(t *testing.T) {
	var empty Model
	empty.SetSize(80, 24)
	empty.Set(Collect(isession.Conversation{}), "")
	drawn := ansi.Strip(empty.View())
	if !strings.Contains(drawn, emptyTitle) {
		t.Fatalf("an empty session draws no reason\n%s", drawn)
	}
	if picked, any := empty.Picked(); any {
		t.Fatalf("an empty picker picked %+v", picked)
	}
	assertGolden(t, "links-empty-80x24.golden", empty.View())
}

func TestTypingFiltersAndTheCountFollowsTheFilter(t *testing.T) {
	built := picker()
	for _, letter := range strings.Split("wiki", "") {
		built.Key(letter)
	}
	rows := built.rows()
	if len(rows) != 1 || !strings.Contains(rows[0].URL, "wikipedia") {
		t.Fatalf("filtering by wiki gave %+v", rows)
	}
	drawn := ansi.Strip(built.View())
	if !strings.Contains(drawn, filterHint+"wiki") || !strings.Contains(drawn, "1 link") {
		t.Fatalf("the filtered view does not say what it holds\n%s", drawn)
	}
	built.Key("backspace")
	if rows := built.rows(); len(rows) != 1 {
		t.Fatalf("backspace to wik gave %d rows", len(rows))
	}
	for range 3 {
		built.Key("backspace")
	}
	if rows := built.rows(); len(rows) != len(carried()) {
		t.Fatalf("an empty filter gave %d rows, want %d", len(rows), len(carried()))
	}
}

func TestAFilterThatMatchesNothingSaysSoAndPicksNothing(t *testing.T) {
	built := picker()
	for _, letter := range strings.Split("zzz", "") {
		built.Key(letter)
	}
	if picked, any := built.Picked(); any {
		t.Fatalf("a filter matching nothing picked %+v", picked)
	}
	if drawn := ansi.Strip(built.View()); !strings.Contains(drawn, noMatch+"zzz") {
		t.Fatalf("a filter matching nothing draws no reason\n%s", drawn)
	}
}

func TestFilteringMovesThePickOntoAVisibleRow(t *testing.T) {
	built := picker()
	built.Key("down")
	built.Key("down")
	if picked, _ := built.Picked(); !strings.Contains(picked.URL, "pkg.go.dev") {
		t.Fatalf("two downs picked %+v", picked)
	}
	for _, letter := range strings.Split("wik", "") {
		built.Key(letter)
	}
	picked, any := built.Picked()
	if !any || !strings.Contains(picked.URL, "wikipedia") {
		t.Fatalf("filtering left the pick on %+v", picked)
	}
}

func TestThePickStopsAtBothEnds(t *testing.T) {
	built := picker()
	for range len(carried()) + 3 {
		built.Key("down")
	}
	if picked, _ := built.Picked(); picked.URL != carried()[len(carried())-1].URL {
		t.Fatalf("the pick ran past the last row onto %+v", picked)
	}
	for range len(carried()) + 3 {
		built.Key("up")
	}
	if picked, _ := built.Picked(); picked.URL != carried()[0].URL {
		t.Fatalf("the pick ran past the first row onto %+v", picked)
	}
}

func TestATroubleReadingTheRecordIsDrawnInsteadOfNothing(t *testing.T) {
	var built Model
	built.SetSize(80, 24)
	built.Set(nil, "the session body does not read back")
	if drawn := ansi.Strip(built.View()); !strings.Contains(drawn, "does not read back") {
		t.Fatalf("the picker swallowed the reason it is empty\n%s", drawn)
	}
}

func TestARepeatSaysHowManyTimesAndASingleSightingDoesNot(t *testing.T) {
	drawn := ansi.Strip(picker().View())
	if !strings.Contains(drawn, timesMark+"3") {
		t.Fatalf("a link seen three times does not say three\n%s", drawn)
	}
	if strings.Contains(drawn, timesMark+"1") {
		t.Fatalf("a link seen once says the count\n%s", drawn)
	}
}
