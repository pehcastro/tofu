package bluemonday

import (
	"regexp"
)

func StrictPolicy() *Policy {
	return NewPolicy()
}

func StripTagsPolicy() *Policy {
	return StrictPolicy()
}

func UGCPolicy() *Policy {

	p := NewPolicy()

	p.AllowStandardAttributes()

	p.AllowStandardURLs()

	p.AllowElements("article", "aside")

	p.AllowAttrs(
		"open",
	).Matching(regexp.MustCompile(`(?i)^(|open)$`)).OnElements("details")

	p.AllowElements("figure")

	p.AllowElements("section")

	p.AllowElements("summary")

	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6")

	p.AllowElements("hgroup")

	p.AllowAttrs("cite").OnElements("blockquote")

	p.AllowElements("br", "div", "hr", "p", "span", "wbr")

	p.AllowAttrs("href").OnElements("a")

	p.AllowAttrs("name").Matching(
		regexp.MustCompile(`^([\p{L}\p{N}_-]+)$`),
	).OnElements("map")
	p.AllowAttrs("alt").Matching(Paragraph()).OnElements("area")
	p.AllowAttrs("coords").Matching(
		regexp.MustCompile(`^([0-9]+,)+[0-9]+$`),
	).OnElements("area")
	p.AllowAttrs("href").OnElements("area")
	p.AllowAttrs("rel").Matching(SpaceSeparatedTokens()).OnElements("area")
	p.AllowAttrs("shape").Matching(
		regexp.MustCompile(`(?i)^(default|circle|rect|poly)$`),
	).OnElements("area")
	p.AllowAttrs("usemap").Matching(
		regexp.MustCompile(`(?i)^#[\p{L}\p{N}_-]+$`),
	).OnElements("img")

	p.AllowElements("abbr", "acronym", "cite", "code", "dfn", "em",
		"figcaption", "mark", "s", "samp", "strong", "sub", "sup", "var")

	p.AllowAttrs("cite").OnElements("q")

	p.AllowAttrs("datetime").Matching(ISO8601()).OnElements("time")

	p.AllowElements("b", "i", "pre", "small", "strike", "tt", "u")

	p.AllowAttrs("dir").Matching(Direction()).OnElements("bdi", "bdo")

	p.AllowElements("rp", "rt", "ruby")

	p.AllowAttrs("cite").Matching(Paragraph()).OnElements("del", "ins")
	p.AllowAttrs("datetime").Matching(ISO8601()).OnElements("del", "ins")

	p.AllowLists()

	p.AllowTables()

	p.AllowAttrs(
		"value",
		"min",
		"max",
		"low",
		"high",
		"optimum",
	).Matching(Number()).OnElements("meter")

	p.AllowAttrs("value", "max").Matching(Number()).OnElements("progress")

	p.AllowImages()

	return p
}
