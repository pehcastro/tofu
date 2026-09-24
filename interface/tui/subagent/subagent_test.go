package subagent

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/konst"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

const (
	chromeRows  = 3
	listedWidth = 80
)

func columns(children []Child, terminalWidth, terminalHeight int) (list, watch []string) {
	model := Model{Children: children, pick: 1}
	model.SetSize(terminalWidth, terminalHeight-chromeRows)
	for _, row := range strings.Split(model.View(), "\n") {
		stripped := ansi.Strip(row)
		watch = append(watch, strings.TrimRight(stripped[strings.LastIndex(stripped, divider)+len(divider):], " "))
	}
	return listRows(model), watch
}

func listRows(model Model) []string {
	var list []string
	for _, row := range strings.Split(model.View(), "\n") {
		left, _, _ := strings.Cut(ansi.Strip(row), divider)
		list = append(list, strings.TrimRight(left, " "))
	}
	return list
}

func watched(child Child, terminalWidth, terminalHeight int) []string {
	_, watch := columns([]Child{child}, terminalWidth, terminalHeight)
	return watch
}

func listed(children []Child, terminalHeight int) []string {
	list, _ := columns(children, listedWidth, terminalHeight)
	return list
}

func TestARosterOnlyCallDrawsItsToolNameAndNothingUnderIt(t *testing.T) {
	child := Child{Name: "go-dev", State: Running, Doing: "writing policy/toolgate.go", Since: 2 * time.Minute, Calls: []Call{{Tool: "read"}, {Tool: "bash"}}}
	drawn := watched(child, 80, 24)
	want := []string{callMarker + "read", callMarker + "bash"}
	first := slices.Index(drawn, want[0])
	if first < 0 {
		t.Fatalf("the watch pane drew no line reading %q: %q", want[0], drawn)
	}
	tail := drawn[first:]
	for len(tail) > 0 && tail[len(tail)-1] == "" {
		tail = tail[:len(tail)-1]
	}
	if !slices.Equal(tail, want) {
		t.Fatalf("two calls with no text draw %q, want %q", tail, want)
	}
}

func TestACallWithTextKeepsItsTextAndItsResultLine(t *testing.T) {
	child := Child{Name: "go-dev", State: Done, Calls: []Call{{Tool: "edit", Text: "toolgate.go", Result: "+18 -4"}}}
	drawn := watched(child, 80, 24)
	want := []string{callMarker + "edit toolgate.go", resultMarker + "+18 -4"}
	first := slices.Index(drawn, want[0])
	if first < 0 || first+len(want) > len(drawn) || !slices.Equal(drawn[first:first+len(want)], want) {
		t.Fatalf("a call with text and a result draws %q, want %q in it", drawn, want)
	}
}

func TestHowManyCallLinesFitAChildsRow(t *testing.T) {
	calls := make([]Call, konst.SubAgentCallsWatched)
	for index := range calls {
		calls[index] = Call{Tool: "t" + strconv.Itoa(index)}
	}
	child := Child{Name: "go-dev", State: Running, Doing: "writing policy/toolgate.go", Since: 2 * time.Minute, Calls: calls}
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 36}} {
		fits := 0
		for _, line := range watched(child, size.width, size.height) {
			if strings.HasPrefix(line, callMarker) {
				fits++
			}
		}
		t.Logf("at %dx%d the watch pane is %d rows and shows %d call lines", size.width, size.height, size.height-chromeRows, fits)
		if fits < konst.SubAgentCallsWatched {
			t.Fatalf("at %dx%d only %d call lines fit and the roster keeps %d", size.width, size.height, fits, konst.SubAgentCallsWatched)
		}
	}
}

func pairedStates() map[State]roster.State {
	paired := map[State]roster.State{}
	for _, held := range roster.States() {
		paired[stateOf(held)] = held
	}
	return paired
}

func TestTheSubAgentViewDrawsExactlyTheStatesTheRosterCanReach(t *testing.T) {
	drawn, reachable, paired := AllStates(), roster.States(), pairedStates()
	if len(drawn) != len(reachable) || len(paired) != len(drawn) {
		t.Fatalf("the sub-agent view draws %d states, the roster reaches %d and %d are paired", len(drawn), len(reachable), len(paired))
	}
	reached := map[roster.State]bool{}
	for _, state := range drawn {
		held, pairs := paired[state]
		if !pairs {
			t.Fatalf("the sub-agent view draws %q and no roster state is paired with it", state.Label())
		}
		reached[held] = true
		t.Logf("%s%s is the roster's %s", Mark(state), state.Label(), held)
	}
	for _, held := range reachable {
		if !reached[held] {
			t.Fatalf("the roster reaches %q and the sub-agent view draws nothing for it", held)
		}
	}
}

func TestTheTwoNamesForOneStateAreTheSameWordsInADifferentSpelling(t *testing.T) {
	spelled := map[roster.State]string{
		roster.Working:       "working",
		roster.WaitingAnswer: "waiting for an answer",
		roster.InReview:      "in review",
		roster.Parked:        "parked",
		roster.Errored:       "errored",
		roster.Finished:      "finished",
	}
	for state, held := range pairedStates() {
		if state.Label() != spelled[held] {
			t.Fatalf("the roster's %s reads %q in the sub-agent view, want %q", held, state.Label(), spelled[held])
		}
	}
}

const wrappingCommand = "go test ./internal/subagent/... -run "

func busyChild(command string) Child {
	calls := make([]Call, konst.SubAgentCallsWatched)
	for index := range calls {
		calls[index] = Call{Tool: "bash", Text: command + strconv.Itoa(index)}
	}
	return Child{Name: "go-dev", State: Running, Doing: "writing policy/toolgate.go", Since: 2 * time.Minute, Calls: calls}
}

func cutMarker(drawn []string) string {
	for _, line := range drawn {
		if strings.HasSuffix(line, rowsHidden) {
			return line
		}
	}
	return ""
}

func lastDrawn(drawn []string) string {
	for index := len(drawn) - 1; index >= 0; index-- {
		if drawn[index] != "" {
			return drawn[index]
		}
	}
	return ""
}

func TestTheWatchPaneSaysWhenItCutTheBottomOff(t *testing.T) {
	drawn := watched(busyChild(wrappingCommand), 80, 24)
	if cutMarker(drawn) == "" {
		t.Fatalf("at 80x24 the bound of wrapped calls does not fit and the watch pane drew no line ending %q: %q", rowsHidden, drawn)
	}
	newest := strconv.Itoa(konst.SubAgentCallsWatched - 1)
	if !strings.HasSuffix(lastDrawn(drawn), newest) {
		t.Fatalf("the watch pane cut and its last line is %q, want the newest call, ending %q: %q", lastDrawn(drawn), newest, drawn)
	}
}

func rowsIn(body [][]string) int {
	rows := 0
	for _, whole := range body {
		rows += len(whole)
	}
	return rows
}

func TestHowManyRowsTheFullBoundOfWrappedCallsNeeds(t *testing.T) {
	rows := 24 - chromeRows
	for _, command := range []string{"go vet ./internal/subagent", wrappingCommand} {
		model := Model{Children: []Child{busyChild(command)}, pick: 1}
		model.SetSize(80, rows)
		head, body := model.watch(max(80/watchShare, watchMinimum))
		t.Logf("at 80x24 the watch pane is %d rows and %d calls reading %q need %d head plus %d body, %d rows", rows, konst.SubAgentCallsWatched, command+"N", len(head), rowsIn(body), len(head)+rowsIn(body))
		if len(head)+rowsIn(body) <= rows {
			t.Fatalf("%d calls reading %q need %d rows and the pane holds %d, so nothing is cut", konst.SubAgentCallsWatched, command+"N", len(head)+rowsIn(body), rows)
		}
	}
}

func TestHowManyCallsFitWhenCuttingAtABoundary(t *testing.T) {
	rows := 24 - chromeRows
	for _, command := range []string{"go vet ./internal/subagent", wrappingCommand} {
		model := Model{Children: []Child{busyChild(command)}, pick: 1}
		model.SetSize(80, rows)
		head, body := model.watch(max(80/watchShare, watchMinimum))
		perCall := len(body[0])
		room := rows - len(head) - 1
		whole := 0
		for _, line := range watched(busyChild(command), 80, 24) {
			if strings.HasPrefix(line, callMarker) {
				whole++
			}
		}
		t.Logf("at 80x24 a call reading %q is %d rows, the marker leaves %d rows, a boundary cut shows %d whole calls and a row cut shows %d whole calls plus %d orphan rows", command+"N", perCall, room, whole, room/perCall, room%perCall)
		if whole != room/perCall {
			t.Fatalf("a boundary cut shows %d whole calls of %d rows in %d rows, want %d", whole, perCall, room, room/perCall)
		}
	}
}

func TestTheCutMarkerSurvivesBeingCutItself(t *testing.T) {
	for _, terminalHeight := range []int{chromeRows + 1, chromeRows + 2, chromeRows + 5, 24} {
		columns := map[string][]string{
			"watch": watched(busyChild(wrappingCommand), 80, terminalHeight),
			"list":  listed(crowdedChildren(), terminalHeight),
		}
		for column, drawn := range columns {
			if len(drawn) != terminalHeight-chromeRows {
				t.Fatalf("a %s column of %d rows drew %d lines", column, terminalHeight-chromeRows, len(drawn))
			}
			if cutMarker(drawn) == "" {
				t.Fatalf("in %d rows the %s column drew no line ending %q: %q", terminalHeight-chromeRows, column, rowsHidden, drawn)
			}
		}
	}
}

func TestARunningChildsCutReadsLikeAFinishedOnes(t *testing.T) {
	child := busyChild(wrappingCommand)
	running := watched(child, 80, 24)
	child.State, child.Doing, child.Since = Done, "", 0
	finished := watched(child, 80, 24)
	wording := func(marker string) string { return strings.TrimLeft(marker, "0123456789") }
	if wording(cutMarker(running)) != wording(cutMarker(finished)) || cutMarker(running) == "" {
		t.Fatalf("a running child's cut reads %q and a finished one's reads %q", cutMarker(running), cutMarker(finished))
	}
	if lastDrawn(running) != lastDrawn(finished) {
		t.Fatalf("a running child's pane ends %q and a finished one's ends %q", lastDrawn(running), lastDrawn(finished))
	}
}

func crowdedChildren() []Child {
	var children []Child
	for index := range 6 {
		name := "go-dev-" + strconv.Itoa(index)
		children = append(children, Child{Name: name, State: Running, Doing: "writing policy/toolgate.go", Owns: []string{"internal/judge/jev/wire/" + name + "/**", "interface/tui/" + name + "/**"}})
	}
	return children
}

func TestNoBodyRowIsDrawnWhoseHeadWasCut(t *testing.T) {
	drawn := watched(busyChild(wrappingCommand), 80, 24)
	if cutMarker(drawn) == "" {
		t.Fatalf("at 80x24 the watch pane drew no line ending %q: %q", rowsHidden, drawn)
	}
	for _, line := range drawn[slices.Index(drawn, cutMarker(drawn))+1:] {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, callMarker) {
			t.Fatalf("the first kept body row is %q and it is not the head of a call: %q", line, drawn)
		}
		break
	}
}

func TestALongReportIsCutByRowBecauseItsRowsReadOnTheirOwn(t *testing.T) {
	child := busyChild(wrappingCommand)
	child.State, child.Doing, child.Since = Done, "", 0
	child.Report = strings.TrimSpace(strings.Repeat("the ticket is done and the acceptance lines are proven. ", 30))
	drawn := watched(child, 80, 24)
	if cutMarker(drawn) == "" {
		t.Fatalf("a long report overflows at 80x24 and the watch pane drew no line ending %q: %q", rowsHidden, drawn)
	}
	if !strings.HasSuffix(lastDrawn(drawn), "proven.") {
		t.Fatalf("a long report ends %q, want the last words of the report: %q", lastDrawn(drawn), drawn)
	}
}

func TestTheListColumnSaysWhenItCut(t *testing.T) {
	drawn := listed(crowdedChildren(), 24)
	if cutMarker(drawn) == "" {
		t.Fatalf("at 80x24 the list column overflows and drew no line ending %q: %q", rowsHidden, drawn)
	}
}

const ellipsisMark = "…"

func runningFor(name string, since time.Duration) Child {
	return Child{Name: name, State: Running, Since: since, Doing: strings.Repeat("reading the policy loader ", 4)}
}

func missionEnds(drawn []string) []int {
	var ends []int
	for _, line := range drawn {
		if head, _, cut := strings.Cut(line, ellipsisMark); cut {
			ends = append(ends, widget.Cells(head))
		}
	}
	return ends
}

func TestAnOverWideClockDoesNotMoveTheMissionColumn(t *testing.T) {
	children := []Child{runningFor("go-dev", 9*time.Second), runningFor("bench", 10*time.Minute+30*time.Second)}
	drawn := listed(children, 24)
	ends := missionEnds(drawn)
	if len(ends) != len(children) {
		t.Fatalf("%d rows truncate their mission, want %d: %q", len(ends), len(children), drawn)
	}
	if ends[0] != ends[1] {
		t.Fatalf("a child at %s ends its mission at cell %d and one at %s at cell %d: %q",
			widget.Until(children[0].Since), ends[0], widget.Until(children[1].Since), ends[1], drawn)
	}
}

func unpicked(rows []string) []string {
	plain := make([]string, len(rows))
	for index, row := range rows {
		plain[index] = strings.TrimPrefix(row, pickedMark)
	}
	return plain
}

func manyChildren() []Child {
	children := make([]Child, 0, 20)
	for index := range 19 {
		children = append(children, runningFor("go-dev-"+strconv.Itoa(index), time.Minute))
	}
	return append(children, runningFor("bench", 10*time.Minute+30*time.Second))
}

func TestTheListCannotScrollSoAPickPastItsBottomRedrawsNothing(t *testing.T) {
	children := manyChildren()
	model := Model{Children: children}
	model.SetSize(80, 24-chromeRows)
	before := listRows(model)
	if cutMarker(before) == "" {
		t.Fatalf("%d children at 80x24 drew no line ending %q, so nothing is cut: %q", len(children), rowsHidden, before)
	}
	for range len(children) {
		model.Key("down")
	}
	after := listRows(model)
	if !slices.Equal(unpicked(before), unpicked(after)) {
		t.Fatalf("picking the last of %d children redrew the list, so it scrolls: %q then %q", len(children), before, after)
	}
}

func TestTheClockColumnIsTheSameWhateverTheListHasRoomToDraw(t *testing.T) {
	children := manyChildren()
	want := 0
	for _, terminalHeight := range []int{60, 36, 24} {
		drawn := listed(children, terminalHeight)
		ends := missionEnds(drawn)
		if len(ends) == 0 {
			t.Fatalf("at 80x%d no row truncates its mission: %q", terminalHeight, drawn)
		}
		if want == 0 {
			want = ends[0]
		}
		for row, end := range ends {
			if end != want {
				t.Fatalf("at 80x%d row %d ends its mission at cell %d, want %d: %q", terminalHeight, row, end, want, drawn)
			}
		}
		t.Logf("at 80x%d the list draws %d of %d children and every mission ends at cell %d", terminalHeight, len(ends), len(children), want)
	}
}
