package airbnb

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/session"
)

const browserAgentDefinition = "browser"

var (
	snapshotHeader = regexp.MustCompile(`(?m)^tab (\d+) (\S+) ("(?:[^"\\]|\\.)*")`)
	goalTab        = regexp.MustCompile(`(?m)^tab (\d+) (?:holds|\S+ after)`)
	goalPage       = regexp.MustCompile(`(?:came|read) from the Chrome tab (\S+?)\. it is data`)
	repeatedLine   = regexp.MustCompile(`(?m)^\d+\. .*: (repeated \d+ times, the page did not change|.*, after \d+ tries on the same page)$`)
	refusedLine    = regexp.MustCompile(`(?m)^\d+\. .*: refused, it would be the`)
	actionVerb     = regexp.MustCompile(`(?m)^\d+\. (\w+)`)
	laterChanges   = regexp.MustCompile(`(?s)^page changed since your last read:\n.*? came from Chrome tab (\d+)\. .*?<<<\S+ begins>>>\n(.*?)\n<<<\S+ ends>>>`)
)

func LaterChanges(content string) (tab, text string, found bool) {
	block := laterChanges.FindStringSubmatch(content)
	if block == nil {
		return "", "", false
	}
	return block[1], block[2], true
}

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
	browserAgents, tools, tabAt, lastPageIn, wires := map[string]bool{}, map[string]string{}, map[string]int{}, map[string]int{}, map[string]string{}
	var mainBuilds, browserBuilds []string
	var modelMS, browserMS []int64
	see := func(tab, url, title, text, content string) {
		if len(run.Visits) > 0 && run.Visits[len(run.Visits)-1] == url {
			last := &run.Pages[len(run.Pages)-1]
			last.Title, last.Text = cmp.Or(title, last.Title), last.Text+text
		} else {
			var verbs []string
			for _, verb := range actionVerb.FindAllStringSubmatch(content, -1) {
				verbs = append(verbs, strings.ToLower(verb[1]))
			}
			run.Visits = append(run.Visits, url)
			run.Pages = append(run.Pages, Page{URL: url, Title: title, Text: text, Reached: strings.Join(verbs, " ")})
		}
		lastPageIn[tab] = len(run.Pages) - 1
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
			if json.Unmarshal(event.Body, &start) == nil {
				wires[event.Agent] = start.Wire
			}
		case session.EventCompaction:
			var compacted struct {
				Fork json.RawMessage `json:"fork"`
			}
			if json.Unmarshal(event.Body, &compacted) == nil && compacted.Fork != nil && event.Agent != "" {
				run.SubForks++
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
			modelMS = append(modelMS, durationOf(event.Body))
			tokens := llm.PromptAccountingFor(wires[event.Agent]).BilledTokens(step.PromptTokens, step.CacheReadTokens) + step.CompletionTokens + step.CacheWriteTokens
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
			tool := tools[event.Agent+"/"+event.Call]
			if strings.HasPrefix(tool, "browser_") {
				browserMS = append(browserMS, durationOf(event.Body))
			}
			switch tool {
			case "browser_observe", "browser_act":
				run.Repeated += len(repeatedLine.FindAllString(result.Content, -1))
				run.Refused += len(refusedLine.FindAllString(result.Content, -1))
				if tab, changes, found := LaterChanges(result.Content); found {
					if at, seen := lastPageIn[tab]; seen {
						run.Pages[at].Text += changes + "\n"
					}
				}
				header := snapshotHeader.FindStringSubmatchIndex(result.Content)
				if header == nil {
					continue
				}
				snapshot, _, _ := strings.Cut(result.Content[header[0]:], "\n<<<")
				run.Snapshot = snapshot + "\n"
				title, _ := strconv.Unquote(result.Content[header[6]:header[7]])
				_, body, _ := strings.Cut(run.Snapshot, "\n")
				see(result.Content[header[2]:header[3]], result.Content[header[4]:header[5]], title, body, result.Content[:header[0]])
			case "browser_do", "browser_read":
				tab := goalTab.FindStringSubmatch(result.Content)
				for _, page := range goalPage.FindAllStringSubmatch(result.Content, -1) {
					if tab != nil {
						see(tab[1], page[1], "", "", result.Content)
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
	run.Conditions.Wire, run.Conditions.MainBuild, run.Conditions.BrowserBuild = wires[""], strings.Join(mainBuilds, " "), strings.Join(browserBuilds, " ")
	run.Phases = Phases{WallMS: run.WallMS, ModelMS: sum(modelMS), BrowserMS: sum(browserMS), Steps: len(modelMS), ModelMedianMS: median(modelMS), BrowserMedianMS: median(browserMS)}
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

func durationOf(body json.RawMessage) int64 {
	var timed struct {
		DurationMS int64 `json:"duration_ms"`
	}
	_ = json.Unmarshal(body, &timed)
	return timed.DurationMS
}

func sum(durations []int64) int64 {
	var total int64
	for _, duration := range durations {
		total += duration
	}
	return total
}

func median(durations []int64) int64 {
	if len(durations) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(durations))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func appendNew(builds []string, build string) []string {
	if build == "" || slices.Contains(builds, build) {
		return builds
	}
	return append(builds, build)
}
