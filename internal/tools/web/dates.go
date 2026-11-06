package web

import (
	"bytes"
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Dates struct {
	Published string
	Modified  string
}

func (d Dates) String() string {
	var stated []string
	if d.Published != "" {
		stated = append(stated, "published "+d.Published)
	}
	if d.Modified != "" {
		stated = append(stated, "modified "+d.Modified)
	}
	if len(stated) == 0 {
		return "date: none stated"
	}
	return "date: " + strings.Join(stated, ", ")
}

func (d *Dates) note(key, value string) {
	value = day(value)
	if value == "" {
		return
	}
	switch strings.ToLower(key) {
	case "article:published_time", "og:published_time", "datepublished", "date", "dc.date", "dc.date.issued",
		"dcterms.date", "dcterms.issued", "dcterms.created":
		d.Published = cmp.Or(d.Published, value)
	case "article:modified_time", "og:updated_time", "datemodified", "dcterms.modified", "last-modified":
		d.Modified = cmp.Or(d.Modified, value)
	}
}

func datesOf(body []byte) Dates {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return Dates{}
	}
	var dates Dates
	var firstTime string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "meta":
				dates.note(cmp.Or(attr(n, "property"), attr(n, "itemprop"), attr(n, "name")), attr(n, "content"))
			case "script":
				if strings.EqualFold(attr(n, "type"), "application/ld+json") {
					var linked any
					if json.Unmarshal([]byte(textOf(n)), &linked) == nil {
						dates.linked(linked)
					}
				}
			case "time":
				firstTime = cmp.Or(firstTime, attr(n, "datetime"))
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if dates == (Dates{}) {
		dates.Published = day(firstTime)
	}
	return dates
}

func (d *Dates) linked(value any) {
	switch node := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(node)) {
			if text, isText := node[key].(string); isText {
				d.note(key, text)
			} else {
				d.linked(node[key])
			}
		}
	case []any:
		for _, inner := range node {
			d.linked(inner)
		}
	}
}

func day(value string) string {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC1123, time.RFC1123Z} {
		if at, err := time.Parse(layout, value); err == nil {
			return at.Format(time.DateOnly)
		}
	}
	if len(value) < len(time.DateOnly) {
		return ""
	}
	if _, err := time.Parse(time.DateOnly, value[:len(time.DateOnly)]); err != nil {
		return ""
	}
	return value[:len(time.DateOnly)]
}
