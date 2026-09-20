package web_test

import (
	"testing"

	"tofu/internal/web"
)

func TestALinkOnlyRowOrItemIsRemoved(t *testing.T) {
	for _, unit := range []web.Unit{
		{Kind: web.UnitItem, Text: "- Guide (https://example.com/guide)"},
		{Kind: web.UnitRow, Text: "Guide (https://example.com/guide)"},
	} {
		units := []web.Unit{{Kind: web.UnitHeading, Text: "# Title"}, unit}
		kept, cut := web.Reduce(units)
		t.Logf("%v: kept %d of %d, cut %+v", unit, len(kept), len(units), cut)
		if len(kept) != 1 {
			t.Fatalf("a link-only %v survived: %+v", unit.Kind, kept)
		}
		if cut.Units != 1 || cut.Bytes != len(unit.Text) {
			t.Fatalf("the cut does not account for what went: %+v", cut)
		}
	}
}

func TestARowOrItemWithALinkAndOtherTextIsKept(t *testing.T) {
	for _, unit := range []web.Unit{
		{Kind: web.UnitItem, Text: "- See the Guide (https://example.com/guide) for more"},
		{Kind: web.UnitRow, Text: "Guide (https://example.com/guide) | the walkthrough"},
	} {
		units := []web.Unit{{Kind: web.UnitHeading, Text: "# Title"}, unit}
		kept, cut := web.Reduce(units)
		t.Logf("%v: kept %d of %d, cut %+v", unit, len(kept), len(units), cut)
		if len(kept) != 2 {
			t.Fatalf("a %v holding a link and other text was dropped: %+v", unit.Kind, cut)
		}
	}
}

func TestACodeBlockIsNeverRemoved(t *testing.T) {
	units := []web.Unit{
		{Kind: web.UnitHeading, Text: "# Title"},
		{Kind: web.UnitCode, Text: "```\nGuide (https://example.com/guide)\n```"},
	}
	kept, cut := web.Reduce(units)
	t.Logf("kept %d of %d, cut %+v", len(kept), len(units), cut)
	if len(kept) != 2 || cut.Units != 0 {
		t.Fatalf("a code block was removed: kept %+v cut %+v", kept, cut)
	}
}

func TestTheUnitCarryingTheTitleIsNeverRemoved(t *testing.T) {
	units := []web.Unit{
		{Kind: web.UnitHeading, Text: "# Title (https://example.com/)"},
		{Kind: web.UnitItem, Text: "- Guide (https://example.com/guide)"},
	}
	kept, cut := web.Reduce(units)
	t.Logf("kept %d of %d, cut %+v", len(kept), len(units), cut)
	if len(kept) == 0 || kept[0] != units[0] {
		t.Fatalf("the title unit was removed: %+v", kept)
	}
	if cut.Units != 1 {
		t.Fatalf("the link-only item beside the title survived: %+v", cut)
	}
}
