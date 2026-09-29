package tasks

import (
	"net/url"
	"regexp"
)

const wikipediaHost = "en.wikipedia.org"

var (
	firstAppearedYear = regexp.MustCompile(`(?s)rowheader "First appeared".*?cell "[^"]*?\b(\d{4})\b`)
	nationalityCell   = regexp.MustCompile(`(?s)rowheader "Nationality".*?cell "([^"]+)"`)
)

func wikiArticle(title string) func(*url.URL) bool {
	return pathIs(wikipediaHost, "/wiki/"+title)
}

var goArticle, griesemerArticle = wikiArticle("Go_(programming_language)"), wikiArticle("Robert_Griesemer")

var wikipedia = Task{
	Name: "wikipedia",
	Prompt: "On en.wikipedia.org, start at the main page and use its search to reach the article on the Go programming language. " +
		"From it, follow the link to the article on its designer Robert Griesemer. Report the year Go first appeared and Griesemer's nationality, as the pages show them. Work in one tab.",
	Steps: []Step{
		{"the main page came before the Go article", func(e Evidence) bool {
			main := e.visited(wikiArticle("Main_Page"))
			return main >= 0 && main < e.visited(goArticle)
		}},
		{"the Go article was reached", func(e Evidence) bool { return e.visited(goArticle) >= 0 }},
		{"Griesemer's article was reached from it", func(e Evidence) bool {
			article := e.visited(goArticle)
			return article >= 0 && e.visited(griesemerArticle) > article
		}},
		{"the report gives the year Go first appeared, as its infobox shows", func(e Evidence) bool {
			page, seen := e.lastPage(goArticle)
			return seen && reportSays(e, firstOf(firstAppearedYear, page.Text))
		}},
		{"the report gives Griesemer's nationality, as his infobox shows", func(e Evidence) bool {
			page, seen := e.lastPage(griesemerArticle)
			return seen && reportSays(e, firstOf(nationalityCell, page.Text))
		}},
		{"the task stayed in one tab", oneTab},
	},
}
