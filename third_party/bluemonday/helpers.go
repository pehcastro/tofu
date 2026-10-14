package bluemonday

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"sync"
)

func lazyRegexp(pattern string) func() *regexp.Regexp {
	return sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(pattern) })
}

var (
	CellAlign = lazyRegexp(`(?i)^(center|justify|left|right|char)$`)

	CellVerticalAlign = lazyRegexp(`(?i)^(baseline|bottom|middle|top)$`)

	Direction = lazyRegexp(`(?i)^(rtl|ltr)$`)

	ImageAlign = lazyRegexp(
		`(?i)^(left|right|top|texttop|middle|absmiddle|baseline|bottom|absbottom)$`,
	)

	Integer = lazyRegexp(`^[0-9]+$`)

	ISO8601 = lazyRegexp(
		`^[0-9]{4}(-[0-9]{2}(-[0-9]{2}([ T][0-9]{2}(:[0-9]{2}){1,2}(.[0-9]{1,6})` +
			`?Z?([\+-][0-9]{2}:[0-9]{2})?)?)?)?$`,
	)

	ListType = lazyRegexp(`(?i)^(circle|disc|square|a|A|i|I|1)$`)

	SpaceSeparatedTokens = lazyRegexp(`^([\s\p{L}\p{N}_-]+)$`)

	Number = lazyRegexp(`^[-+]?[0-9]*\.?[0-9]+([eE][-+]?[0-9]+)?$`)

	NumberOrPercent = lazyRegexp(`^[0-9]+[%]?$`)

	Paragraph = lazyRegexp(`^[\p{L}\p{N}\s\-_',\[\]!\./\\\(\)]*$`)

	dataURIImagePrefix = lazyRegexp(
		`^image/(gif|jpeg|png|svg\+xml|webp);base64,`,
	)
)

func (p *Policy) AllowStandardURLs() {

	p.RequireParseableURLs(true)

	p.AllowRelativeURLs(true)

	p.AllowURLSchemes("mailto", "http", "https")

	p.RequireNoFollowOnLinks(true)
}

func (p *Policy) AllowStandardAttributes() {

	p.AllowAttrs("dir").Matching(Direction()).Globally()
	p.AllowAttrs(
		"lang",
	).Matching(regexp.MustCompile(`[a-zA-Z]{2,20}`)).Globally()

	p.AllowAttrs("id").Matching(
		regexp.MustCompile(`[a-zA-Z0-9\:\-_\.]+`),
	).Globally()

	p.AllowAttrs("title").Matching(Paragraph()).Globally()
}

func (p *Policy) AllowStyling() {

	p.AllowAttrs("class").Matching(SpaceSeparatedTokens()).Globally()
}

func (p *Policy) AllowImages() {

	p.AllowAttrs("align").Matching(ImageAlign()).OnElements("img")
	p.AllowAttrs("alt").Matching(Paragraph()).OnElements("img")
	p.AllowAttrs("height", "width").Matching(NumberOrPercent()).OnElements("img")

	p.AllowStandardURLs()
	p.AllowAttrs("src").OnElements("img")
}

func (p *Policy) AllowDataURIImages() {

	p.RequireParseableURLs(true)

	p.AllowURLSchemeWithCustomPolicy(
		"data",
		func(url *url.URL) (allowUrl bool) {
			if url.RawQuery != "" || url.Fragment != "" {
				return false
			}

			matched := dataURIImagePrefix().FindString(url.Opaque)
			if matched == "" {
				return false
			}

			_, err := base64.StdEncoding.DecodeString(url.Opaque[len(matched):])
			return err == nil
		},
	)
}

func (p *Policy) AllowLists() {

	p.AllowAttrs("type").Matching(ListType()).OnElements("ol", "ul")

	p.AllowAttrs("type").Matching(ListType()).OnElements("li")
	p.AllowAttrs("value").Matching(Integer()).OnElements("li")

	p.AllowElements("dl", "dt", "dd")
}

func (p *Policy) AllowTables() {

	p.AllowAttrs("height", "width").Matching(NumberOrPercent()).OnElements("table")
	p.AllowAttrs("summary").Matching(Paragraph()).OnElements("table")

	p.AllowElements("caption")

	p.AllowAttrs("align").Matching(CellAlign()).OnElements("col", "colgroup")
	p.AllowAttrs("height", "width").Matching(
		NumberOrPercent(),
	).OnElements("col", "colgroup")
	p.AllowAttrs("span").Matching(Integer()).OnElements("colgroup", "col")
	p.AllowAttrs("valign").Matching(
		CellVerticalAlign(),
	).OnElements("col", "colgroup")

	p.AllowAttrs("align").Matching(CellAlign()).OnElements("thead", "tr")
	p.AllowAttrs("valign").Matching(CellVerticalAlign()).OnElements("thead", "tr")

	p.AllowAttrs("abbr").Matching(Paragraph()).OnElements("td", "th")
	p.AllowAttrs("align").Matching(CellAlign()).OnElements("td", "th")
	p.AllowAttrs("colspan", "rowspan").Matching(Integer()).OnElements("td", "th")
	p.AllowAttrs("headers").Matching(
		SpaceSeparatedTokens(),
	).OnElements("td", "th")
	p.AllowAttrs("height", "width").Matching(
		NumberOrPercent(),
	).OnElements("td", "th")
	p.AllowAttrs(
		"scope",
	).Matching(
		regexp.MustCompile(`(?i)(?:row|col)(?:group)?`),
	).OnElements("td", "th")
	p.AllowAttrs("valign").Matching(CellVerticalAlign()).OnElements("td", "th")
	p.AllowAttrs("nowrap").Matching(
		regexp.MustCompile(`(?i)|nowrap`),
	).OnElements("td", "th")

	p.AllowAttrs("align").Matching(CellAlign()).OnElements("tbody", "tfoot")
	p.AllowAttrs("valign").Matching(
		CellVerticalAlign(),
	).OnElements("tbody", "tfoot")
}

func (p *Policy) AllowIFrames(vals ...SandboxValue) {
	p.AllowAttrs("sandbox").OnElements("iframe")

	p.RequireSandboxOnIFrame(vals...)
}
