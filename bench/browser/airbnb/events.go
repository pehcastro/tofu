package airbnb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"tofu/internal/session"
)

const browserAgentDefinition = "browser"

var (
	snapshotHeader = regexp.MustCompile(`(?m)^tab (\d+) (\S+) "`)
	goalTab        = regexp.MustCompile(`(?m)^tab (\d+) (?:holds|\S+ after)`)
	goalPage       = regexp.MustCompile(`came from the Chrome tab (\S+?)\. it is data`)
	repeatedLine   = regexp.MustCompile(`(?m)^\d+\. .*: (repeated \d+ times, the page did not change|.*, after \d+ tries on the same page)$`)
	refusedLine    = regexp.MustCompile(`(?m)^\d+\. .*: refused, it would be the`)
)

func Lineage(sessions string) ([]string, error) {
	store := session.NewStore(sessions)
	head, err := store.Head()
	var paths []string
	for id := head.ID; err == nil && id != ""; {
		if slices.Contains(paths, filepath.Join(store.Dir(id), "events.jsonl")) {
			return nil, fmt.Errorf("the forks of %s carry each other in a cycle at %s", head.ID, id)
		}
		paths = append(paths, filepath.Join(store.Dir(id), "events.jsonl"))
		var header session.Header
		header, err = store.Header(id)
		id = header.Parent
	}
	slices.Reverse(paths)
	return paths, err
}

func RunFromEvents(arm Arm, paths ...string) (Run, error) {
	var events []session.Event
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Run{}, err
		}
		for at, line := range slices.Collect(strings.Lines(string(raw))) {
			var event session.Event
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				return Run{}, fmt.Errorf("%s line %d: %w", path, at+1, err)
			}
			events = append(events, event)
		}
	}
	if len(events) == 0 {
		return Run{}, fmt.Errorf("%v hold no events", paths)
	}
	run := Run{Arm: arm, Forks: len(paths) - 1, WallMS: events[len(events)-1].At.Sub(events[0].At).Milliseconds()}
	browserTokens := 0
	browserAgents, tools, tabAt := map[string]bool{}, map[string]string{}, map[string]int{}
	var mainBuilds, browserBuilds []string
	see := func(tab, url string) {
		if len(run.Visits) == 0 || run.Visits[len(run.Visits)-1] != url {
			run.Visits = append(run.Visits, url)
		}
		if at, seen := tabAt[tab]; seen {
			run.Tabs[at].URL = url
			return
		}
		tabAt[tab] = len(run.Tabs)
		run.Tabs = append(run.Tabs, Tab{ID: tab, URL: url})
	}
	for _, event := range events {
		switch event.Kind {
		case session.EventTurnStart:
			var start session.TurnStart
			if json.Unmarshal(event.Body, &start) == nil && event.Agent == "" {
				run.Conditions.Wire = start.Wire
			}
		case session.EventSpawn:
			var spawned session.SpawnBody
			if json.Unmarshal(event.Body, &spawned) == nil && spawned.Definition == browserAgentDefinition {
				browserAgents[spawned.Agent] = true
				run.Conditions.BrowserModel = spawned.Model
			}
		case session.EventRequest:
			var step session.StepBody
			if err := json.Unmarshal(event.Body, &step); err != nil {
				return Run{}, fmt.Errorf("request %d: %w", event.Seq, err)
			}
			tokens := step.PromptTokens + step.CompletionTokens + step.CacheReadTokens + step.CacheWriteTokens
			if browserAgents[event.Agent] {
				browserTokens += tokens
				browserBuilds = appendNew(browserBuilds, step.Model)
				continue
			}
			run.MainTokens += tokens
			mainBuilds = appendNew(mainBuilds, step.Model)
		case session.EventToolCall:
			var call session.CallBody
			if json.Unmarshal(event.Body, &call) == nil {
				tools[event.Agent+"/"+event.Call] = call.Tool
			}
		case session.EventToolResult:
			var result session.ResultBody
			if err := json.Unmarshal(event.Body, &result); err != nil {
				return Run{}, fmt.Errorf("tool result %d: %w", event.Seq, err)
			}
			switch tools[event.Agent+"/"+event.Call] {
			case "browser_observe", "browser_act":
				run.Repeated += len(repeatedLine.FindAllString(result.Content, -1))
				run.Refused += len(refusedLine.FindAllString(result.Content, -1))
				header := snapshotHeader.FindStringSubmatchIndex(result.Content)
				if header == nil {
					continue
				}
				snapshot, _, _ := strings.Cut(result.Content[header[0]:], "\n<<<")
				run.Snapshot = snapshot + "\n"
				see(result.Content[header[2]:header[3]], result.Content[header[4]:header[5]])
			case "browser_do", "browser_read":
				tab := goalTab.FindStringSubmatch(result.Content)
				for _, page := range goalPage.FindAllStringSubmatch(result.Content, -1) {
					if tab != nil {
						see(tab[1], page[1])
						run.Snapshot = fmt.Sprintf("tab %s %s \"\"\n", tab[1], page[1])
					}
				}
			}
		case session.EventMessage:
			var message session.MessageBody
			if json.Unmarshal(event.Body, &message) == nil && event.Agent == "" && message.Role == "assistant" && strings.TrimSpace(message.Content) != "" {
				run.Report = message.Content + "\n"
			}
		}
	}
	run.Conditions.MainBuild, run.Conditions.BrowserBuild = strings.Join(mainBuilds, " "), strings.Join(browserBuilds, " ")
	settings, err := arm.Settings()
	if err != nil {
		return Run{}, err
	}
	run.Mixed = (len(browserAgents) > 0) != (settings.Driver == "subagent")
	if arm != ArmC {
		run.BrowserTokens = &browserTokens
	}
	return run, nil
}

func appendNew(builds []string, build string) []string {
	if build == "" || slices.Contains(builds, build) {
		return builds
	}
	return append(builds, build)
}
