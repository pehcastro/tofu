package status

import (
	"encoding/base64"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func b64(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

func body(sequence string) string {
	return strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b]7501;"), "\x1b\\")
}

func TestStatusEncodeWritesWhatATerminalAccepts(t *testing.T) {
	progress := 40
	cases := []struct {
		name   string
		record Record
		want   string
	}{
		{"the root carries no id", Record{State: Working, App: "tofu"}, "state=working:app=tofu"},
		{"kind rides only on blocked", Record{State: Done, Kind: Permission}, "state=done"},
		{"blocked carries its kind", Record{State: Blocked, Kind: Question, ID: "agents/go-dev-1"}, "state=blocked:id=agents/go-dev-1:kind=question"},
		{"progress rides only on working or blocked", Record{State: Done, Progress: &progress}, "state=done"},
		{"working carries progress", Record{State: Working, Progress: &progress}, "state=working:progress=40"},
		{"a newline and an escape in text become spaces", Record{State: Errored, Msg: "line one\nline\x1btwo\u0085"}, "state=error:msg=" + b64("line one line two ")},
		{"clear names its id only", Record{State: Clear, ID: "shells/dev", Title: "dev"}, "state=clear:id=shells/dev"},
	}
	for _, c := range cases {
		got := Encode(c.record)
		if !strings.HasPrefix(got, "\x1b]7501;") || !strings.HasSuffix(got, "\x1b\\") || body(got) != c.want {
			t.Errorf("%s: Encode = %q, want body %q", c.name, got, c.want)
		}
	}
}

func TestStatusEncodeClipsLongTextOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", 3000)
	sequence := Encode(Record{State: Working, Title: long, Msg: long})
	if len(sequence) > maxSequence {
		t.Fatalf("sequence is %d bytes, over %d", len(sequence), maxSequence)
	}
	parsed, err := Parse(body(sequence))
	if err != nil {
		t.Fatalf("a clipped record does not parse back: %v", err)
	}
	if !utf8.ValidString(parsed.Title) || len(parsed.Title) > maxTitle || len(parsed.Title) < maxTitle-1 {
		t.Errorf("title clipped to %d bytes, valid utf8 %v", len(parsed.Title), utf8.ValidString(parsed.Title))
	}
	if !utf8.ValidString(parsed.Msg) || len(parsed.Msg) > maxMsg || len(parsed.Msg) < maxMsg-1 {
		t.Errorf("msg clipped to %d bytes, valid utf8 %v", len(parsed.Msg), utf8.ValidString(parsed.Msg))
	}
}

func TestStatusPathMakesEveryNameAValidSegment(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"agents", "go-dev-3"}, "agents/go-dev-3"},
		{[]string{"shells", "npm run dev"}, "shells/npm_run_dev"},
		{[]string{"shells", "a/b"}, "shells/a_b"},
		{[]string{"shells", ""}, "shells/_"},
		{[]string{"cron", strings.Repeat("x", 40)}, "cron/" + strings.Repeat("x", 32)},
		{[]string{"agents", "café"}, "agents/caf__"},
		{strings.Split("a/b/c/d/e/f/g/h/i/j", "/"), "a/b/c/d/e/f/g/h"},
	}
	for _, c := range cases {
		got := Path(c.parts...)
		if got != c.want {
			t.Errorf("Path(%q) = %q, want %q", c.parts, got, c.want)
		}
		if _, err := Parse("state=idle:id=" + got); err != nil {
			t.Errorf("Path(%q) = %q does not parse as an id: %v", c.parts, got, err)
		}
	}
	nine := make([]string, 8)
	for index := range nine {
		nine[index] = strings.Repeat("y", 32)
	}
	if got := Path(nine...); len(got) > maxID {
		t.Errorf("eight full segments make an id of %d bytes, over %d", len(got), maxID)
	}
}

func TestStatusParseFollowsTheSpec(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Record
	}{
		{"unknown keys are ignored", "state=idle:colour=red", Record{State: Idle}},
		{"a repeated key keeps the last value", "state=idle:state=working", Record{State: Working}},
		{"a malformed pair is skipped", "nonsense:=x:Upper=1:state=done:app=bad app", Record{State: Done}},
		{"whitespace around keys and values is removed", " state = working : app = cargo ", Record{State: Working, App: "cargo"}},
		{"kind is ignored without blocked", "state=working:kind=auth", Record{State: Working}},
		{"an unknown kind is absent", "state=blocked:kind=snack", Record{State: Blocked}},
		{"progress outside 0 to 100 is absent", "state=working:progress=101", Record{State: Working}},
		{"progress that is not an integer is absent", "state=working:progress=40%", Record{State: Working}},
		{"progress is ignored on done", "state=done:progress=40", Record{State: Done}},
		{"a byte outside the value set skips the pair, not the report", "state=done:msg=@@@@", Record{State: Done}},
		{"padding is optional", "state=done:msg=QQ", Record{State: Done, Msg: "A"}},
		{"the spec's terraform example", "state=blocked:kind=permission:app=terraform:msg=QXBwbHkgMyB0byBhZGQsIDEgdG8gY2hhbmdlLCAwIHRvIGRlc3Ryb3k/",
			Record{State: Blocked, Kind: Permission, App: "terraform", Msg: "Apply 3 to add, 1 to change, 0 to destroy?"}},
		{"clear with an id", "state=clear:id=us-east", Record{State: Clear, ID: "us-east"}},
	}
	for _, c := range cases {
		got, err := Parse(c.body)
		if err != nil || !same(got, c.want) {
			t.Errorf("%s: Parse(%q) = %+v, %v; want %+v", c.name, c.body, got, err, c.want)
		}
	}
	progress, err := Parse("state=blocked:progress=0")
	if err != nil || progress.Progress == nil || *progress.Progress != 0 {
		t.Errorf("progress=0 on blocked parsed as %+v, %v", progress, err)
	}
}

func TestStatusParseDiscardsAReportWhole(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Refusal
	}{
		{"no state", "app=cargo", NoState},
		{"a state added later never turns into idle", "state=paused", NoState},
		{"base64 that does not decode", "state=done:msg=A", BadText},
		{"a control character in decoded text", "state=done:title=" + b64("a\nb"), BadText},
		{"a C1 control in decoded text", "state=done:msg=" + b64("a\u0085b"), BadText},
		{"msg encoded over its limit, before decoding", "state=done:msg=" + strings.Repeat("A", maxMsgEncoded+4), OverLimit},
		{"title decoded over its limit", "state=done:title=" + b64(strings.Repeat("t", maxTitle+1)), OverLimit},
		{"a key over sixteen bytes", "state=done:" + strings.Repeat("k", maxKey+1) + "=1", OverLimit},
		{"app over thirty two bytes", "state=done:app=" + strings.Repeat("a", maxApp+1), OverLimit},
		{"an empty segment never falls back to the root", "state=working:id=a//b", BadID},
		{"a segment over thirty two bytes", "state=working:id=" + strings.Repeat("s", maxSegment+1), BadID},
		{"nine levels", "state=working:id=a/b/c/d/e/f/g/h/i", BadID},
		{"a space in an id", "state=working:id=a b", BadID},
		{"a whole sequence over 4096 bytes", "state=done:" + strings.Repeat("x=1:", 1100), OverLimit},
		{"the feature detection query", "?", Query},
	}
	for _, c := range cases {
		got, err := Parse(c.body)
		if err != c.want {
			t.Errorf("%s: Parse = %+v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestStatusEncodeThenParseIsTheSameRecord(t *testing.T) {
	progress := 7
	records := []Record{
		{State: Idle, App: "tofu", Title: "session one"},
		{State: Blocked, Kind: Auth, ID: Path("shells", "claude"), Progress: &progress, Msg: "Log in to continue"},
		{State: Errored, ID: Path("agents", "go-dev-1", "agents", "research-1"), Msg: "exit 2"},
	}
	for _, record := range records {
		parsed, err := Parse(body(Encode(record)))
		if err != nil || !same(parsed, record) {
			t.Errorf("round trip of %+v gave %+v, %v", record, parsed, err)
		}
	}
}

func TestStatusBoardHoldsOneRecordPerID(t *testing.T) {
	var board Board
	if !board.Apply(Record{State: Working, App: "deploy", Msg: "Deploying"}) {
		t.Fatal("the first report changed nothing")
	}
	if board.Apply(Record{State: Working, App: "deploy", Msg: "Deploying"}) {
		t.Error("the same report twice counted as a change")
	}
	board.Apply(Record{State: Working})
	if listed := board.List(); len(listed) != 1 || listed[0].App != "" || listed[0].Msg != "" {
		t.Errorf("a report did not replace its record whole: %+v", listed)
	}
	for _, id := range []string{"build", "build/test", "build/test/unit", "builder"} {
		board.Apply(Record{State: Working, ID: id})
	}
	board.Apply(Record{State: Clear, ID: "build"})
	ids := []string{}
	for _, record := range board.List() {
		ids = append(ids, record.ID)
	}
	if strings.Join(ids, ",") != ",builder" {
		t.Errorf("clear of build left %q, want the root and builder", ids)
	}
	if board.Apply(Record{State: Clear, ID: "missing"}) {
		t.Error("clearing an id with nothing under it counted as a change")
	}
	board.Apply(Record{State: Clear})
	if listed := board.List(); len(listed) != 0 {
		t.Errorf("clear with no id left %+v", listed)
	}
}

func TestStatusBoardSyncPastCapacitySendsOnce(t *testing.T) {
	wanted := []Record{{State: Idle, App: "tofu"}}
	for index := range maxRecords + 50 {
		wanted = append(wanted, Record{State: Done, ID: Path("shells", "bash-"+strconv.Itoa(index))})
	}
	var board Board
	first := board.Sync(wanted)
	if len(first) != maxRecords || first[0].ID != "" {
		t.Fatalf("first sync sent %d records led by %q, want %d led by the lead", len(first), first[0].ID, maxRecords)
	}
	if again := board.Sync(wanted); len(again) != 0 {
		t.Fatalf("an unchanged set past capacity sent %d records again, want none", len(again))
	}
	gone, entering := wanted[1], wanted[maxRecords]
	moved := board.Sync(slices.Delete(slices.Clone(wanted), 1, 2))
	if len(moved) != 2 || moved[0] != (Record{ID: gone.ID, State: Clear}) || moved[1] != entering {
		t.Fatalf("one record leaving sent %+v, want the clear of %s and %s entering", moved, gone.ID, entering.ID)
	}
}

func TestStatusBoardStampsAtOnAStateChangeOnly(t *testing.T) {
	tick := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	board := Board{Now: func() time.Time { tick = tick.Add(time.Minute); return tick }}
	first := board.Sync([]Record{{ID: "build", State: Working, Msg: "one"}})
	if len(first) != 1 || !first[0].At.Equal(time.Date(2026, 10, 9, 12, 1, 0, 0, time.UTC)) {
		t.Fatalf("a new record went out as %+v, want it stamped 12:01", first)
	}
	if again := board.Sync([]Record{{ID: "build", State: Working, Msg: "one"}}); len(again) != 0 {
		t.Fatalf("an unchanged record went out again as %+v: the stamp made it look new", again)
	}
	reworded := board.Sync([]Record{{ID: "build", State: Working, Msg: "two"}})
	if len(reworded) != 1 || !reworded[0].At.Equal(first[0].At) {
		t.Fatalf("a new msg in the same state went out as %+v, want the 12:01 stamp kept", reworded)
	}
	finished := board.Sync([]Record{{ID: "build", State: Done, Msg: "two"}})
	if len(finished) != 1 || !finished[0].At.After(first[0].At) {
		t.Fatalf("a state change went out as %+v, want a stamp after 12:01", finished)
	}
	if listed := board.List(); len(listed) != 1 || !listed[0].At.Equal(finished[0].At) {
		t.Fatalf("the board lists %+v, want the stamp it sent", listed)
	}
	board.Apply(Record{ID: "build", State: Clear})
	if back := board.Sync([]Record{{ID: "build", State: Done, Msg: "two"}}); len(back) != 1 || !back[0].At.After(finished[0].At) {
		t.Fatalf("a record cleared and back went out as %+v, want a fresh stamp", back)
	}
	var unclocked Board
	if sent := unclocked.Sync([]Record{{ID: "build", State: Working}}); len(sent) != 1 || !sent[0].At.IsZero() {
		t.Fatalf("a board with no clock stamped %+v", sent)
	}
}

func TestStatusBoardEvictsTheLeastRecentlyUpdated(t *testing.T) {
	var board Board
	for index := range maxRecords {
		board.Apply(Record{State: Working, ID: Path("r", strings.Repeat("a", index%30+1), string(rune('a'+index/30)))})
	}
	first := Path("r", "a", "a")
	board.Apply(Record{State: Done, ID: first})
	board.Apply(Record{State: Working, ID: "newcomer"})
	listed := board.List()
	if len(listed) != maxRecords {
		t.Fatalf("board holds %d records, want %d", len(listed), maxRecords)
	}
	held := map[string]bool{}
	for _, record := range listed {
		held[record.ID] = true
	}
	if !held[first] || !held["newcomer"] || held[Path("r", "aa", "a")] {
		t.Errorf("eviction picked the wrong record: first %v newcomer %v second %v", held[first], held["newcomer"], held[Path("r", "aa", "a")])
	}
}
