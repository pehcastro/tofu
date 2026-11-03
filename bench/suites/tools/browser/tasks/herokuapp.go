package tasks

import (
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	herokuHost         = "the-internet.herokuapp.com"
	herokuLoadedText   = "Hello World!"
	herokuDropdownPick = "Option 2"
	checkedControl     = "checked"
)

var (
	checkboxLine = regexp.MustCompile(`(?m)^\s*(?:\* )?[-*] checkbox\b.*\bchecked=(\w+)`)
	comboboxLine = regexp.MustCompile(`(?m)^\s*(?:\* )?[-*] combobox\b.*: (.+)$`)
	stepRead     = regexp.MustCompile(`^(e\d+) now reads ("(?:[^"\\]|\\.)*")$`)
	comboboxRead = regexp.MustCompile(`^(?:[~+] )?\s*(?:\* )?[-*] combobox\b.*\bref=(e\d+)\]: (.+)$`)
	checkboxRead = regexp.MustCompile(`^(?:[~+] )?\s*(?:\* )?[-*] checkbox\b.*\bchecked=(\w+), ref=(e\d+)`)
)

var herokuapp = Task{
	Name: "herokuapp",
	Prompt: "On the-internet.herokuapp.com: log in on /login with the username and password the page itself shows. Then on /dropdown choose Option 2, " +
		"on /checkboxes make both boxes checked, and on /dynamic_loading/1 start the load and report the text that appears. Work in one tab.",
	Steps: []Step{
		{"the login reached the secure area", func(e Evidence) bool {
			login := e.visited(herokuPathIs("/login"))
			return login >= 0 && e.visited(herokuPathIs("/secure")) > login
		}},
		{"the dropdown reads Option 2", func(e Evidence) bool {
			page, seen := e.lastPage(herokuPathIs("/dropdown"))
			return seen && strings.TrimSpace(firstOf(comboboxLine, page.Text)) == herokuDropdownPick ||
				slices.Contains(slices.Collect(maps.Values(controlsLastRead(e, "/dropdown"))), herokuDropdownPick)
		}},
		{"both checkboxes are checked", func(e Evidence) bool {
			page, seen := e.lastPage(herokuPathIs("/checkboxes"))
			boxes := checkboxLine.FindAllStringSubmatch(page.Text, -1)
			snapshotChecked := seen && len(boxes) == 2 && !slices.ContainsFunc(boxes, func(box []string) bool { return box[1] != "true" })
			read := controlsLastRead(e, "/checkboxes")
			return snapshotChecked || len(read) == 2 && !slices.ContainsFunc(slices.Collect(maps.Values(read)), func(box string) bool { return box != checkedControl })
		}},
		{"the dynamic loading page was opened", func(e Evidence) bool { return e.visited(herokuPathIs("/dynamic_loading/1")) >= 0 }},
		{"the report gives the text that appeared", func(e Evidence) bool { return reportSays(e, herokuLoadedText) }},
		{"the task stayed in one tab", oneTab},
	},
}

func herokuPathIs(path string) func(*url.URL) bool {
	return func(address *url.URL) bool {
		return strings.HasSuffix(address.Host, herokuHost) && address.Path == path
	}
}

func controlsLastRead(e Evidence, path string) map[string]string {
	read := map[string]string{}
	for _, page := range slices.Backward(e.Run.Pages) {
		address, err := url.Parse(page.URL)
		if err != nil || !herokuPathIs(path)(address) {
			continue
		}
		for line := range strings.Lines(page.Text) {
			line = strings.TrimSuffix(line, "\n")
			if found := stepRead.FindStringSubmatch(line); found != nil {
				read[found[1]], _ = strconv.Unquote(found[2])
			} else if found := comboboxRead.FindStringSubmatch(line); found != nil {
				read[found[1]] = strings.TrimSpace(found[2])
			} else if found := checkboxRead.FindStringSubmatch(line); found != nil {
				read[found[2]] = "unchecked"
				if found[1] == "true" {
					read[found[2]] = checkedControl
				}
			}
		}
		return read
	}
	return read
}
