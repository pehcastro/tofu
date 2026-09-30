package tasks

import (
	"regexp"
	"strings"
)

const (
	herokuHost         = "the-internet.herokuapp.com"
	herokuLoadedText   = "Hello World!"
	herokuDropdownPick = "Option 2"
)

var (
	checkboxLine = regexp.MustCompile(`(?m)^\s*(?:\* )?[-*] checkbox\b.*\bchecked=(\w+)`)
	comboboxLine = regexp.MustCompile(`(?m)^\s*(?:\* )?[-*] combobox\b.*: (.+)$`)
)

var herokuapp = Task{
	Name: "herokuapp",
	Prompt: "On the-internet.herokuapp.com: log in on /login with the username and password the page itself shows. Then on /dropdown choose Option 2, " +
		"on /checkboxes make both boxes checked, and on /dynamic_loading/1 start the load and report the text that appears. Work in one tab.",
	Steps: []Step{
		{"the login reached the secure area", func(e Evidence) bool {
			login := e.visited(pathIs(herokuHost, "/login"))
			return login >= 0 && e.visited(pathIs(herokuHost, "/secure")) > login
		}},
		{"the dropdown reads Option 2", func(e Evidence) bool {
			page, seen := e.lastPage(pathIs(herokuHost, "/dropdown"))
			return seen && strings.TrimSpace(firstOf(comboboxLine, page.Text)) == herokuDropdownPick
		}},
		{"both checkboxes are checked", func(e Evidence) bool {
			page, seen := e.lastPage(pathIs(herokuHost, "/checkboxes"))
			boxes := checkboxLine.FindAllStringSubmatch(page.Text, -1)
			for _, box := range boxes {
				if box[1] != "true" {
					return false
				}
			}
			return seen && len(boxes) == 2
		}},
		{"the dynamic loading page was opened", func(e Evidence) bool { return e.visited(pathIs(herokuHost, "/dynamic_loading/1")) >= 0 }},
		{"the report gives the text that appeared", func(e Evidence) bool { return reportSays(e, herokuLoadedText) }},
		{"the task stayed in one tab", oneTab},
	},
}
