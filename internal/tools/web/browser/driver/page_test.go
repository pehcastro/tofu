package browser

import (
	"encoding/json"
	"fmt"
	"strconv"
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

func TestParsePageKeepsLinksBesideTheElementsJevReads(t *testing.T) {
	page, err := ParsePage([]byte(`{"url":"https://www.airbnb.com.br/s/Atibaia","fingerprint":"f1","links":{"4":"https://www.airbnb.com.br/rooms/42"},` +
		`"elements":[{"index":4,"role":"link","label":"Chalé na Serra"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	elements, _ := json.Marshal(page.Elements)
	t.Logf("links %v, elements %s", page.Links, elements)
	if page.Links[4] != "https://www.airbnb.com.br/rooms/42" || strings.Contains(string(elements), "rooms/42") {
		t.Fatalf("links %v and elements %s; want the link in Links and nowhere in the elements", page.Links, elements)
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

type fixtureNode struct {
	NodeID   string   `json:"nodeId"`
	Ignored  bool     `json:"ignored"`
	Role     axValue  `json:"role"`
	Name     axValue  `json:"name"`
	ChildIDs []string `json:"childIds"`
	Backend  int      `json:"backendDOMNodeId"`
}

type fixture struct {
	nodes []fixtureNode
	urls  map[int]string
}

func (f *fixture) add(role, name string, kids ...string) string {
	id := strconv.Itoa(len(f.nodes) + 1)
	f.nodes = append(f.nodes, fixtureNode{NodeID: id, Ignored: role == "none", Role: axValue{role}, Name: axValue{name}, ChildIDs: kids, Backend: len(f.nodes) + 1})
	return id
}

func (f *fixture) link(name, url string, kids ...string) string {
	id := f.add("link", name, kids...)
	f.urls[len(f.nodes)] = url
	return id
}

func (f *fixture) render(t *testing.T) (string, map[string]refEntry) {
	t.Helper()
	raw, _ := json.Marshal(f.nodes)
	var nodes []axNode
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}
	refs := refMap{entries: map[string]refEntry{}}
	tree, roots := refs.snapshot("", "loader", nodes, nil)
	for i := range tree {
		tree[i].url = f.urls[tree[i].backend]
	}
	var out strings.Builder
	for _, root := range roots {
		render(&out, tree, root, 0, view{skip: -1})
	}
	return out.String(), refs.entries
}

type book struct{ title, url, price string }

func booksCategory() (*fixture, []book) {
	f := &fixture{urls: map[int]string{}}
	var categories []string
	for c := range 50 {
		name := fmt.Sprintf("Category %d", c)
		categories = append(categories, f.add("listitem", "", f.add("ListMarker", "• "), f.link(name, fmt.Sprintf("/catalogue/category/books/category-%d_%d/index.html", c, c+2))))
	}
	star, tick := string(rune(0xe006)), string(rune(0xe013))
	var books []book
	var cards []string
	for n := range 20 {
		b := book{title: fmt.Sprintf("The Mystery of the Blue Train, Volume %d", n), url: fmt.Sprintf("/catalogue/the-mystery-of-the-blue-train-volume-%d_%d/index.html", n, 900+n), price: fmt.Sprintf("£%d.%02d", 10+n, 17*n%100)}
		books = append(books, b)
		var stars []string
		for range 5 {
			stars = append(stars, f.add("none", "", f.add("StaticText", star)))
		}
		short := b.title[:18] + "..."
		cards = append(cards, f.add("listitem", "", f.add("article", "",
			f.add("generic", "", f.link(b.title, b.url, f.add("image", b.title))),
			f.add("paragraph", "", stars...),
			f.add("heading", short, f.link(short, b.url, f.add("StaticText", short))),
			f.add("generic", "",
				f.add("paragraph", "", f.add("StaticText", b.price)),
				f.add("paragraph", "", f.add("none", "", f.add("StaticText", tick)), f.add("StaticText", " In stock"))),
			f.add("form", "", f.add("button", "Add to basket")))))
	}
	f.add("RootWebArea", "Mystery | Books to Scrape - Sandbox",
		f.add("generic", "", f.link("Books to Scrape", "/index.html"), f.add("StaticText", "We love being scraped!")),
		f.add("generic", "",
			f.add("list", "", f.add("listitem", "", f.link("Home", "/index.html")), f.add("listitem", "", f.add("StaticText", "Mystery"))),
			f.add("complementary", "", f.add("list", "", f.add("listitem", "", f.link("Books", "/catalogue/category/books_1/index.html"), f.add("list", "", categories...)))),
			f.add("generic", "",
				f.add("heading", "Mystery"),
				f.add("form", "", f.add("StaticText", "32"), f.add("none", "", f.add("StaticText", "results - showing")), f.add("StaticText", "1 to 20.")),
				f.add("list", "", cards...),
				f.add("list", "", f.add("listitem", "", f.add("StaticText", "Page 1 of 2")), f.add("listitem", "", f.link("next", "page-2.html"))))))
	return f, books
}

func TestABooksCategoryRendersUnder17KBWithEveryBookPriceAndRef(t *testing.T) {
	f, books := booksCategory()
	out, refs := f.render(t)
	t.Logf("%d bytes, %d lines\n%s", len(out), strings.Count(out, "\n"), out)
	if len(out) >= 17*1024 {
		t.Errorf("the page renders to %d bytes, want under %d", len(out), 17*1024)
	}
	for _, b := range books {
		if !strings.Contains(out, strconv.Quote(b.title)) || !strings.Contains(out, "url="+b.url+"]") || !strings.Contains(out, strconv.Quote(b.price)) {
			t.Errorf("book %+v is missing its link, url or price", b)
		}
	}
	for ref := range refs {
		if !strings.Contains(out, "ref="+ref+",") && !strings.Contains(out, "ref="+ref+"]") {
			t.Errorf("ref %s is gone", ref)
		}
	}
	if strings.Contains(out, "ListMarker") || strings.Contains(out, `\ue`) || strings.Count(out, "In stock") != len(books) {
		t.Error("a list marker or an icon glyph survived, or the stock text went with the glyph")
	}
	previous := 0
	for line := range strings.Lines(out) {
		depth := len(line) - len(strings.TrimLeft(line, " "))
		if depth > previous+1 {
			t.Fatalf("line %q is indented %d past a line at %d, want one space a level", line, depth, previous)
		}
		previous = depth
	}
}

func TestANamedFormOrArticleKeepsItsLineAndAnUnnamedOneOnlyItsChildren(t *testing.T) {
	f := &fixture{urls: map[int]string{}}
	f.add("RootWebArea", "Shop",
		f.add("form", "Search", f.add("searchbox", "Query")),
		f.add("article", "Review", f.add("StaticText", "Great book")),
		f.add("form", "", f.add("button", "Add to basket"), f.add("StaticText", "£3.00")),
		f.add("article", "", f.add("paragraph", "", f.add("StaticText", "★★★★☆"), f.add("StaticText", "4 of 5"))))
	out, _ := f.render(t)
	t.Logf("\n%s", out)
	for _, want := range []string{`form "Search"`, `article "Review"`, `button "Add to basket" [ref=`, `"£3.00"`, `"★★★★☆4 of 5"`} {
		if !strings.Contains(out, want) {
			t.Errorf("%s is missing", want)
		}
	}
	if strings.Contains(out, "- form\n") || strings.Contains(out, "- article\n") || strings.Contains(out, "paragraph") {
		t.Error("an unnamed form, article or paragraph kept its line")
	}
}
