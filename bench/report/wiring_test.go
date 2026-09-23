package report

import (
	"html"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/method"
	shipped "tofu/library"
)

const fixtureTable = `kind: method_table
table_version: 1
notes: a fixture standing in for the shipped table
methods:
  page_sift:
    method: cheap
    why: the link stripper keeps 21 of 21 planted needles against jev's 15, 1.40x
    measured: bench/websift/report-2026-09-21.md
    cost: nothing, and no call is made
  shell_sift:
    method: judged
    why: jev keeps 27 of 34 planted needles against the free method's 18, 1.50x
    measured: bench/sift/report-2026-09-21.md
    cost: a median $0.000540 a session and 838 ms on a shell call
`

func shippedTable(t *testing.T) method.Table {
	t.Helper()
	table, err := method.Load(shipped.Files())
	if err != nil {
		t.Fatalf("loading the shipped method table: %v", err)
	}
	return table
}

func fixturePage(t *testing.T, rows string) string {
	t.Helper()
	table, err := method.Parse([]byte(rows), "a fixture table")
	if err != nil {
		t.Fatalf("parsing the fixture table: %v", err)
	}
	callers := map[string]caller{
		"shell_sift": {Point: "shell_sift", File: "internal/turn/shellsift.go", Symbol: "cutShellResult"},
		"page_sift":  {Point: "page_sift", Nothing: "0 files outside bench/websift name it"},
	}
	data := Data{Wiring: wiringOf(table, callers), MethodTable: table.File}
	return Page(data)
}

func TestOneLineOfTheMethodTableChangesWhatThePageSaysDecides(t *testing.T) {
	before := fixturePage(t, fixtureTable)
	if !strings.Contains(before, "yes, through the judged method") {
		t.Fatal("the fixture table says shell_sift is judged and the page does not say so")
	}
	after := fixturePage(t, strings.Replace(fixtureTable, "    method: judged", "    method: unwired", 1))
	if strings.Contains(after, "yes, through the judged method") {
		t.Error("one line of the table now reads unwired and the page still says the judged method decides shell_sift")
	}
	if stated := strings.Count(after, ">"+NotWired+"<"); stated != 1 {
		t.Errorf("the table leaves shell_sift unwired and the page states %q %d times, want it once beside the point that lost its method", NotWired, stated)
	}
	t.Logf("judged in the table renders %q, unwired in the same line renders %q", "yes, through the judged method", NotWired)
}

func TestTheBuildRefusesAPageThatContradictsTheMethodTable(t *testing.T) {
	table := shippedTable(t)
	claimed := Placement{
		Point: "shell_sift", Question: "how much of a shell result to keep", Decides: "shell_sift", InUse: method.Unwired,
		Axis:       Axis{Name: "planted needles kept", Unit: "percent of results", BetterWhen: BetterHigher},
		Free:       Method{Name: "regex on error lines", Does: "keeps the head and the tail", DoesFrom: "internal/sift/shellarm.go", Percent: 52.9, Hits: 18, OutOf: 34, Evidence: "18 of 34"},
		Judged:     Method{Name: "Jev", Does: "scores each chunk", DoesFrom: "bench/sift/arm.go", Percent: 79.4, Hits: 27, OutOf: 34, Evidence: "27 of 34"},
		SampleSize: 34, SampleOf: "recorded shell results",
		Source: "bench/sift/report-2026-09-21.md",
	}
	bodies := map[string]string{"bench/sift/report-2026-09-21.md": "18 of 34 and 27 of 34"}
	err := checkPlacement(claimed, table, filepath.Join("..", ".."), bodies)
	if err == nil {
		t.Fatal("the page said shell_sift is not wired and the table says judged, and the build accepted it")
	}
	t.Logf("refused, as it must be: %v", err)
	claimed.InUse = method.Judged
	if err := checkPlacement(claimed, table, filepath.Join("..", ".."), bodies); err != nil {
		t.Fatalf("the page and the table agree and the build still refused: %v", err)
	}
}

func TestEveryPointTheTableWiresCarriesWhatItCostsOnThePage(t *testing.T) {
	data := built(t)
	table := shippedTable(t)
	page := Page(data)
	if len(data.Wiring) != len(table.Choices) {
		t.Fatalf("the page states what decides %d points and %s names %d", len(data.Wiring), table.File, len(table.Choices))
	}
	on := 0
	for _, row := range data.Wiring {
		chosen, err := table.Of(row.Point)
		if err != nil {
			t.Fatalf("%s is on the page and not in the table", row.Point)
		}
		if !row.On {
			t.Logf("%s: %s", row.Point, row.SwitchedOn)
			continue
		}
		on++
		if row.Costs != chosen.Cost {
			t.Errorf("%s costs %q on the page and %q at %s:%d", row.Point, row.Costs, chosen.Cost, table.File, chosen.Line)
		}
		if !strings.Contains(page, html.EscapeString(chosen.Cost)) {
			t.Errorf("%s is switched on and the page never says it costs %q", row.Point, chosen.Cost)
		}
		t.Logf("%s: %s, costing %s, called by %s", row.Point, row.SwitchedOn, row.Costs, row.CalledBy)
	}
	if on == 0 {
		t.Fatal("no point is switched on, so nothing proves a cost is rendered")
	}
}

func TestAPointWhoseCheapMethodNothingCallsIsNotCalledDecided(t *testing.T) {
	data := built(t)
	page := Page(data)
	for _, row := range data.Wiring {
		if row.CalledBy != "" || !strings.HasPrefix(row.SwitchedOn, NotWired+": ") {
			continue
		}
		if !strings.Contains(page, html.EscapeString(row.SwitchedOn)) {
			t.Errorf("%s has nothing calling it and the page does not say so", row.Point)
		}
		t.Logf("%s: %s", row.Point, row.SwitchedOn)
		return
	}
	t.Skip("every point the table wires has a caller in the tree, so nothing here has a dead method to state")
}

func TestACallerNamedForAPointThatDoesNotCallItIsRefused(t *testing.T) {
	table := shippedTable(t)
	tree := filepath.Join("..", "..")
	callers, err := readCallers("callers.json")
	if err != nil {
		t.Fatalf("reading %s: %v", CallersPath, err)
	}
	if err := checkCallers(table, callers, tree); err != nil {
		t.Fatalf("the callers on disk were refused: %v", err)
	}
	callers["shell_sift"] = caller{Point: "shell_sift", File: "internal/turn/shellsift.go", Symbol: "noSuchFunction"}
	if err := checkCallers(table, callers, tree); err == nil {
		t.Error("a caller naming a function that file does not hold was accepted")
	} else {
		t.Logf("refused, as it must be: %v", err)
	}
	delete(callers, "shell_sift")
	if err := checkCallers(table, callers, tree); err == nil {
		t.Error("a point the table wires with nothing said about what calls it was accepted")
	} else {
		t.Logf("refused, as it must be: %v", err)
	}
}
