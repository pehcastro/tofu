package airbnb

import (
	"encoding/json"
	"fmt"
	"os"
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

func readEvents(path string) ([]session.Event, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []session.Event
	for line := range strings.Lines(string(raw)) {
		var event session.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, len(events)+1, err)
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%s holds no events", path)
	}
	return events, nil
}

func RunFromEvents(arm Arm, path string) (Run, error) {
	events, err := readEvents(path)
	if err != nil {
		return Run{}, err
	}
	run := Run{Arm: arm, WallMS: events[len(events)-1].At.Sub(events[0].At).Milliseconds()}
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
