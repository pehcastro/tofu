package tasks

import (
	"regexp"
	"slices"
)

const (
	mainPageTitle  = "Wikipedia, the free encyclopedia"
	goTitle        = "Go (programming language) - Wikipedia"
	griesemerTitle = "Robert Griesemer - Wikipedia"
)

var (
	firstAppearedYear = regexp.MustCompile(`(?s)rowheader "First appeared".*?cell "[^"]*?\b(\d{4})\b`)
	nationalityCell   = regexp.MustCompile(`(?s)rowheader "Nationality".*?cell "([^"]+)"`)
	leadNationality   = regexp.MustCompile(`\bis an? ([A-Z][\p{L}-]+) `)
)

func (e Evidence) firstTitled(title string) int {
	return slices.IndexFunc(e.Pages, func(page Page) bool { return page.Title == title })
}

func (e Evidence) shownOn(title string, patterns ...*regexp.Regexp) string {
	for _, pattern := range patterns {
		for _, page := range slices.Backward(e.Pages) {
			if found := firstOf(pattern, page.Text); page.Title == title && found != "" {
				return found
			}
		}
	}
	return ""
}

var wikipedia = Task{
	Name: "wikipedia",
	Prompt: "On en.wikipedia.org, start at the main page and use its search to reach the article on the Go programming language. " +
		"From it, follow the link to the article on its designer Robert Griesemer. Report the year Go first appeared and Griesemer's nationality, as the pages show them. Work in one tab.",
	Steps: []Step{
		{"the main page came before the Go article", func(e Evidence) bool {
			main := e.firstTitled(mainPageTitle)
			return main >= 0 && main < e.firstTitled(goTitle)
		}},
		{"the Go article was reached", func(e Evidence) bool { return e.firstTitled(goTitle) >= 0 }},
		{"Griesemer's article was reached from it", func(e Evidence) bool {
			article := e.firstTitled(goTitle)
			return article >= 0 && e.firstTitled(griesemerTitle) > article
		}},
		{"the report gives the year Go first appeared, as its infobox shows", func(e Evidence) bool {
			return reportSays(e, e.shownOn(goTitle, firstAppearedYear))
		}},
		{"the report gives Griesemer's nationality, from his infobox or the lead sentence", func(e Evidence) bool {
			return reportSays(e, e.shownOn(griesemerTitle, nationalityCell, leadNationality))
		}},
		{"the task stayed in one tab", oneTab},
	},
}
