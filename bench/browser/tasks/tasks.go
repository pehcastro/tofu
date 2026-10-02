package tasks

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/bench/browser/airbnb"
	"tofu/internal/session"
)

type Page struct {
	URL   *url.URL
	Title string
	Text  string
}

type Evidence struct {
	Run   airbnb.Run
	Pages []Page
}

type Step struct {
	Says   string
	Passes func(Evidence) bool
}

type Task struct {
	Name   string
	Prompt string
	Steps  []Step
}

var (
	snapshotHeader = regexp.MustCompile(`(?m)^tab (\d+) (\S+) ("(?:[^"\\]|\\.)*")`)
	snapshotRef    = regexp.MustCompile(`ref=(e\d+)[,\]]`)
)

func Named(name string, seed int64, drawnOn time.Time) (Task, error) {
	for _, task := range []Task{books, herokuapp, wikipedia, drawFlights(seed, drawnOn), drawYouTube(seed, drawnOn), drawNPM(seed), drawIMDb(seed)} {
		if task.Name == name {
			return task, nil
		}
	}
	return Task{}, fmt.Errorf("unknown task %q: airbnb, books, herokuapp, wikipedia, flights, youtube, npm or imdb", name)
}

func Read(arm airbnb.Arm, paths ...string) (Evidence, error) {
	run, err := airbnb.RunFromEvents(arm, paths...)
	if err != nil {
		return Evidence{}, err
	}
	evidence := Evidence{Run: run}
	tools, windowed, lastPageIn := map[string]string{}, map[string]bool{}, map[string]int{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Evidence{}, err
		}
		for line := range strings.Lines(string(raw)) {
			var event session.Event
			var call session.CallBody
			var result session.ResultBody
			if json.Unmarshal([]byte(line), &event) != nil {
				continue
			}
			key := event.Agent + "/" + event.Call
			if event.Kind == session.EventToolCall && json.Unmarshal(event.Body, &call) == nil {
				var args struct {
					From int `json:"from"`
				}
				_ = json.Unmarshal(call.Args, &args)
				tools[key], windowed[key] = call.Tool, args.From > 1
			}
			tool := tools[key]
			if event.Kind != session.EventToolResult || tool != "browser_observe" && tool != "browser_act" || json.Unmarshal(event.Body, &result) != nil {
				continue
			}
			if tab, changes, found := airbnb.LaterChanges(result.Content); found {
				if at, seen := lastPageIn[tab]; seen {
					evidence.Pages[at].Text += "\n" + changes
				}
			}
			header := snapshotHeader.FindStringSubmatchIndex(result.Content)
			if header == nil {
				continue
			}
			address, err := url.Parse(result.Content[header[4]:header[5]])
			if err != nil {
				continue
			}
			title, _ := strconv.Unquote(result.Content[header[6]:header[7]])
			text, _, _ := strings.Cut(result.Content[header[0]:], "\n<<<")
			if before, seen := evidence.lastPage(func(page *url.URL) bool { return *page == *address }); seen && windowed[key] {
				_, window, _ := strings.Cut(text, "\n")
				text = before.Text + "\n" + window
			} else if seen {
				text = patched(before.Text, text)
			}
			lastPageIn[result.Content[header[2]:header[3]]] = len(evidence.Pages)
			evidence.Pages = append(evidence.Pages, Page{URL: address, Title: title, Text: text})
		}
	}
	return evidence, nil
}

func patched(before, delta string) string {
	header, body, _ := strings.Cut(delta, "\n")
	if strings.HasPrefix(body, "nothing changed since the last snapshot") {
		return before
	}
	if !strings.HasPrefix(body, "changed since the last snapshot") {
		return delta
	}
	page := strings.Split(before, "\n")
	page[0] = header
	_, changes, _ := strings.Cut(body, "\n")
	for _, change := range strings.Split(changes, "\n") {
		ref := snapshotRef.FindStringSubmatch(change)
		if ref == nil {
			continue
		}
		at := slices.IndexFunc(page, func(line string) bool {
			found := snapshotRef.FindStringSubmatch(line)
			return found != nil && found[1] == ref[1]
		})
		switch {
		case (strings.HasPrefix(change, "+ ") || strings.HasPrefix(change, "~ ")) && at >= 0:
			page[at] = change[2:]
		case strings.HasPrefix(change, "+ "):
			page = append(page, change[2:])
		case strings.HasPrefix(change, "x gone: ") && at >= 0:
			page = slices.Delete(page, at, at+1)
		}
	}
	return strings.Join(page, "\n")
}

func Score(task Task, evidence Evidence) airbnb.Row {
	row := airbnb.Score(airbnb.Task{}, evidence.Run)
	for at, step := range task.Steps {
		passed := step.Passes(evidence)
		if passed {
			row.Passed++
		}
		row.Steps = append(row.Steps, airbnb.Result{Step: at + 1, Passed: passed})
	}
	return row
}

func parsedAs(match func(*url.URL) bool) func(string) bool {
	return func(visited string) bool {
		parsed, err := url.Parse(visited)
		return err == nil && match(parsed)
	}
}

func (e Evidence) visited(match func(*url.URL) bool) int {
	return slices.IndexFunc(e.Run.Visits, parsedAs(match))
}

func (e Evidence) visitedAfter(at int, match func(*url.URL) bool) bool {
	return at >= 0 && slices.ContainsFunc(e.Run.Visits[at+1:], parsedAs(match))
}

func (e Evidence) lastPage(match func(*url.URL) bool) (Page, bool) {
	for _, page := range slices.Backward(e.Pages) {
		if match(page.URL) {
			return page, true
		}
	}
	return Page{}, false
}

func (e Evidence) final() (Page, bool) {
	if len(e.Pages) == 0 {
		return Page{}, false
	}
	return e.Pages[len(e.Pages)-1], true
}

func oneTab(e Evidence) bool { return len(e.Run.Tabs) == 1 }

func firstAfter(label *regexp.Regexp, page string, values func(string) []string) []string {
	at := label.FindStringIndex(page)
	if at == nil {
		return nil
	}
	found := values(page[at[1]:])
	return found[:min(1, len(found))]
}

func reportSays(e Evidence, text string) bool {
	return text != "" && strings.Contains(strings.ToLower(e.Run.Report), strings.ToLower(text))
}
