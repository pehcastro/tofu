package web

import (
	"bytes"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

type UnitKind int

const (
	UnitParagraph UnitKind = iota
	UnitHeading
	UnitCode
	UnitItem
	UnitQuote
	UnitRow
)

type Unit struct {
	Kind UnitKind
	Text string
}

func Render(units []Unit) string {
	var out strings.Builder
	for i, unit := range units {
		if i > 0 {
			gap := "\n\n"
			if units[i-1].Kind == unit.Kind && (unit.Kind == UnitItem || unit.Kind == UnitRow) {
				gap = "\n"
			}
			out.WriteString(gap)
		}
		out.WriteString(unit.Text)
	}
	return out.String()
}

func Units(contentType string, body []byte, base *url.URL) []Unit {
	if !strings.Contains(contentType, "html") {
		return textUnits(string(body))
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return textUnits(string(body))
	}
	var units []Unit
	title := clean(textOf(find(doc, "title")))
	root := doc
	for _, tag := range []string{"main", "article", "body"} {
		if found := find(doc, tag); found != nil {
			root = found
			break
		}
	}
	units = append(units, blocks(root, base)...)
	first := ""
	if len(units) > 0 && units[0].Kind == UnitHeading {
		first = strings.TrimLeft(units[0].Text, "# ")
	}
	if title == "" || (first != "" && strings.HasPrefix(title, first)) {
		return units
	}
	return append([]Unit{{Kind: UnitHeading, Text: "# " + title}}, units...)
}

type Cut struct {
	Units int
	Bytes int
}

var (
	numberedMarker = regexp.MustCompile(`^[0-9]+\. `)
	pureLink       = regexp.MustCompile(`^[^|()]+\([a-z][a-z0-9+.-]*://[^()\s]+\)$`)
)

func Reduce(units []Unit) ([]Unit, Cut) {
	if len(units) == 0 {
		return units, Cut{}
	}
	kept := make([]Unit, 1, len(units))
	kept[0] = units[0]
	var cut Cut
	for _, unit := range units[1:] {
		if LinkOnly(unit.Kind, unit.Text) {
			cut.Units++
			cut.Bytes += len(unit.Text)
			continue
		}
		kept = append(kept, unit)
	}
	return kept, cut
}

func LinkOnly(kind UnitKind, text string) bool {
	return (kind == UnitItem || kind == UnitRow) && linkOnlyLine(stripMarker(text))
}

func stripMarker(text string) string {
	body := strings.TrimSpace(text)
	body = strings.TrimPrefix(body, "- ")
	body = strings.TrimPrefix(body, "> ")
	return numberedMarker.ReplaceAllString(body, "")
}

func linkOnlyLine(body string) bool {
	if body == "" {
		return false
	}
	for _, part := range strings.Split(body, " | ") {
		if !pureLink.MatchString(strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"pre": true, "blockquote": true, "figcaption": true, "address": true, "details": true,
}

var chromeTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "nav": true, "header": true,
	"footer": true, "aside": true, "form": true, "svg": true, "iframe": true,
	"button": true, "select": true, "template": true, "dialog": true, "head": true,
}

var chromeRoles = map[string]bool{
	"navigation": true, "banner": true, "contentinfo": true, "search": true, "complementary": true,
}

var chromeNames = []string{
	"sidebar", "navbar", "navigation", "menu", "footer", "cookie", "banner",
	"breadcrumb", "advert", "newsletter", "social", "subscribe", "related", "comments",
}

func chrome(n *html.Node) bool {
	if chromeTags[n.Data] || chromeRoles[attr(n, "role")] || attr(n, "aria-hidden") == "true" {
		return true
	}
	named := strings.ToLower(attr(n, "class") + " " + attr(n, "id"))
	for _, name := range chromeNames {
		if strings.Contains(named, name) {
			return true
		}
	}
	return false
}

func blocks(n *html.Node, base *url.URL) []Unit {
	var units []Unit
	var line strings.Builder
	flush := func() {
		if text := clean(line.String()); text != "" {
			units = append(units, Unit{Kind: UnitParagraph, Text: text})
		}
		line.Reset()
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && chrome(child) {
			continue
		}
		if child.Type != html.ElementNode || !blockTags[child.Data] {
			inline(child, base, &line)
			continue
		}
		flush()
		units = append(units, block(child, base)...)
	}
	flush()
	return units
}

func block(n *html.Node, base *url.URL) []Unit {
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		if text := clean(inlineOf(n, base)); text != "" {
			return []Unit{{Kind: UnitHeading, Text: strings.Repeat("#", int(n.Data[1]-'0')) + " " + text}}
		}
	case "pre":
		if code := strings.Trim(textOf(n), "\n"); strings.TrimSpace(code) != "" {
			return []Unit{{Kind: UnitCode, Text: "```" + language(n) + "\n" + code + "\n```"}}
		}
	case "li":
		return marked(n, base, UnitItem, "- ")
	case "ol":
		var units []Unit
		number := 0
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode || child.Data != "li" || chrome(child) {
				continue
			}
			number++
			units = append(units, marked(child, base, UnitItem, strconv.Itoa(number)+". ")...)
		}
		return units
	case "blockquote":
		return marked(n, base, UnitQuote, "> ")
	case "tr":
		if row := clean(cells(n, base)); strings.Trim(row, "| ") != "" {
			return []Unit{{Kind: UnitRow, Text: row}}
		}
	default:
		return blocks(n, base)
	}
	return nil
}

func marked(n *html.Node, base *url.URL, kind UnitKind, mark string) []Unit {
	inner := blocks(n, base)
	for i, unit := range inner {
		if unit.Kind == UnitParagraph {
			inner[i] = Unit{Kind: kind, Text: mark + unit.Text}
		}
	}
	return inner
}

func cells(n *html.Node, base *url.URL) string {
	var parts []string
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.Data == "td" || child.Data == "th") {
			parts = append(parts, clean(inlineOf(child, base)))
		}
	}
	return strings.Join(parts, " | ")
}

func inline(n *html.Node, base *url.URL, into *strings.Builder) {
	if n.Type == html.TextNode {
		into.WriteString(strings.ReplaceAll(strings.ReplaceAll(n.Data, "\n", " "), "\r", " "))
		return
	}
	if n.Type != html.ElementNode || chrome(n) {
		return
	}
	switch n.Data {
	case "br":
		into.WriteString("\n")
	case "img":
	case "code":
		if text := clean(textOf(n)); text != "" && !strings.Contains(text, "`") {
			into.WriteString("`" + text + "`")
		}
	case "a":
		text := clean(inlineOf(n, base))
		switch target := link(base, attr(n, "href")); {
		case text == "":
		case target == "" || target == text:
			into.WriteString(text)
		default:
			into.WriteString(text + " (" + target + ")")
		}
	default:
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			inline(child, base, into)
		}
	}
}

func inlineOf(n *html.Node, base *url.URL) string {
	var out strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		inline(child, base, &out)
	}
	return out.String()
}

func link(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:") {
		return ""
	}
	target, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		target = base.ResolveReference(target)
	}
	return target.String()
}

func language(n *html.Node) string {
	classes := attr(n, "class")
	if code := find(n, "code"); code != nil {
		classes += " " + attr(code, "class")
	}
	for _, class := range strings.Fields(classes) {
		for _, prefix := range []string{"language-", "lang-", "highlight-source-"} {
			if name, found := strings.CutPrefix(class, prefix); found && name != "" {
				return name
			}
		}
	}
	return attr(n, "data-lang")
}

func attr(n *html.Node, name string) string {
	for _, one := range n.Attr {
		if one.Key == name {
			return one.Val
		}
	}
	return ""
}

func find(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := find(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var out strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		out.WriteString(textOf(child))
	}
	return out.String()
}

func clean(text string) string {
	var out strings.Builder
	space, newline := false, false
	for _, letter := range text {
		switch {
		case letter == '\n':
			newline = true
		case unicode.IsSpace(letter):
			space = true
		default:
			if out.Len() > 0 && newline {
				out.WriteByte('\n')
			} else if out.Len() > 0 && space {
				out.WriteByte(' ')
			}
			space, newline = false, false
			out.WriteRune(letter)
		}
	}
	return out.String()
}

func textUnits(text string) []Unit {
	var units []Unit
	var held []string
	kind := UnitParagraph
	flush := func() {
		if strings.TrimSpace(strings.Join(held, "")) != "" {
			units = append(units, Unit{Kind: kind, Text: strings.Join(held, "\n")})
		}
		held, kind = nil, UnitParagraph
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		fenced := strings.HasPrefix(strings.TrimSpace(line), "```")
		switch {
		case fenced && kind == UnitCode:
			held = append(held, line)
			flush()
		case fenced:
			flush()
			kind = UnitCode
			held = append(held, line)
		case kind == UnitCode:
			held = append(held, line)
		case strings.TrimSpace(line) == "":
			flush()
		case strings.HasPrefix(line, "#") && len(held) == 0:
			units = append(units, Unit{Kind: UnitHeading, Text: strings.TrimSpace(line)})
		default:
			held = append(held, line)
		}
	}
	flush()
	return units
}
