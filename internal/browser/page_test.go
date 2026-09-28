package browser

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"tofu/internal/konst"
)

func snapshotJSON(text string, elements ...string) []byte {
	return []byte(fmt.Sprintf(`{"url":"http://fixture/","title":"Forma","text":%q,"fingerprint":"f1","elements":[%s]}`,
		text, strings.Join(elements, ",")))
}

func TestParsePageDropsPasswordFileAndHiddenInputs(t *testing.T) {
	page, err := ParsePage(snapshotJSON("",
		`{"index":1,"role":"searchbox","label":"Destination","input":"search"}`,
		`{"index":2,"role":"textbox","label":"Password","input":"password"}`,
		`{"index":3,"role":"textbox","label":"Upload","input":"file"}`,
		`{"index":4,"role":"textbox","label":"Token","input":"hidden"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Elements) != 1 || page.Elements[0].Index != 1 {
		t.Fatalf("elements kept %+v, want only the destination field", page.Elements)
	}
}

func TestParsePageRefusesAnUnknownRoleAndAMissingFingerprint(t *testing.T) {
	if _, err := ParsePage(snapshotJSON("", `{"index":1,"role":"slider","label":"Volume"}`)); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	if _, err := ParsePage([]byte(`{"url":"http://fixture/","elements":[]}`)); err == nil {
		t.Fatal("a snapshot with no fingerprint was accepted")
	}
}

func TestParsePageCapsTextOnARuneAndElementsAtTheCeiling(t *testing.T) {
	var elements []string
	for i := 1; i <= konst.BrowserElementCeiling+5; i++ {
		elements = append(elements, fmt.Sprintf(`{"index":%d,"role":"button","label":"b%d"}`, i, i))
	}
	page, err := ParsePage(snapshotJSON(strings.Repeat("é", konst.BrowserPageTextRunes+10), elements...))
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(page.Text); n != konst.BrowserPageTextRunes || !utf8.ValidString(page.Text) {
		t.Fatalf("text holds %d runes, valid %v, want %d", n, utf8.ValidString(page.Text), konst.BrowserPageTextRunes)
	}
	if len(page.Elements) != konst.BrowserElementCeiling {
		t.Fatalf("kept %d elements, want %d", len(page.Elements), konst.BrowserElementCeiling)
	}
}

func TestParseOpRefusesAnUnknownName(t *testing.T) {
	for op := OpClick; op <= OpBlocked; op++ {
		parsed, err := ParseOp(op.String())
		if err != nil || parsed != op {
			t.Fatalf("ParseOp(%q) = %v, %v", op, parsed, err)
		}
	}
	if _, err := ParseOp("NAVIGATE"); err == nil {
		t.Fatal("NAVIGATE parsed as an op")
	}
}
