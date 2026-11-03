package status

import (
	"strings"
	"testing"
)

func TestStatusStreamTakesBothTerminatorsAndLeavesTheText(t *testing.T) {
	var stream Stream
	text, records := stream.Take("A\x1b]7501;state=working:progress=40\x1b\\B\x1b]7501;state=blocked:kind=auth\aC\x1b]0;title\aD")
	if text != "ABC\x1b]0;title\aD" {
		t.Errorf("text = %q", text)
	}
	if len(records) != 2 || records[0].State != Working || *records[0].Progress != 40 || records[1].Kind != Auth {
		t.Errorf("records = %+v", records)
	}
}

func TestStatusStreamHoldsAReportSplitAcrossReads(t *testing.T) {
	whole := "before\x1b]7501;state=done:msg=" + b64("built") + "\x1b\\after"
	for cut := 1; cut < len(whole); cut++ {
		var stream Stream
		first, records := stream.Take(whole[:cut])
		second, late := stream.Take(whole[cut:])
		records = append(records, late...)
		if first+second != "beforeafter" || len(records) != 1 || records[0].Msg != "built" {
			t.Fatalf("cut at %d: text %q + %q, records %+v", cut, first, second, records)
		}
		if cut >= len("before") && !strings.HasPrefix(first, "before") {
			t.Fatalf("cut at %d held back the text before the report: %q", cut, first)
		}
	}
}

func TestStatusStreamDropsABadReportAndGivesUpOnAnEndlessOne(t *testing.T) {
	var stream Stream
	text, records := stream.Take("x\x1b]7501;app=cargo\ay")
	if text != "xy" || len(records) != 0 {
		t.Errorf("a report with no state gave %q, %+v", text, records)
	}
	endless := "\x1b]7501;state=working:" + strings.Repeat("a", maxSequence)
	text, records = stream.Take(endless)
	if text != endless || len(records) != 0 {
		t.Errorf("an unterminated report past the size limit was held or parsed: %d bytes out, %+v", len(text), records)
	}
}

func TestStatusSyncClearsWhatIsGoneBeforeItSends(t *testing.T) {
	var board Board
	parent, child := Record{ID: "agents/a", State: Working}, Record{ID: "agents/a/agents/b", State: Working}
	if sent := board.Sync([]Record{parent, child}); len(sent) != 2 {
		t.Fatalf("first sync sent %+v", sent)
	}
	if sent := board.Sync([]Record{parent, child}); len(sent) != 0 {
		t.Errorf("an unchanged sync sent %+v", sent)
	}
	sent := board.Sync([]Record{child})
	if len(sent) != 2 || sent[0] != (Record{ID: "agents/a", State: Clear}) || sent[1].ID != child.ID {
		t.Errorf("dropping the parent sent %+v, want its clear and then the child again", sent)
	}
	if listed := board.List(); len(listed) != 1 || listed[0].ID != child.ID {
		t.Errorf("board holds %+v", listed)
	}
}
