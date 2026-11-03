package main

import (
	"encoding/json"
	"regexp"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/store/session"
)

var shellName = regexp.MustCompile(`\bbash-\d+\b`)

type tofuCall struct {
	tool string
	args struct {
		Command string `json:"command"`
		Op      string `json:"op"`
		Name    string `json:"name"`
	}
}

type tofuResult struct {
	Content    string `json:"content"`
	DurationMS int64  `json:"duration_ms"`
}

func readTofu(project string, w window) (*tally, error) {
	store, err := session.OpenIn(project)
	if err != nil {
		return nil, err
	}
	listing, err := store.Listing()
	if err != nil {
		return nil, err
	}
	t := newTally()
	t.unreadable = len(listing.Skipped)
	seenRequests, seenCalls := map[string]bool{}, map[string]bool{}
	for _, family := range listing.Families() {
		before := t.generations
		for _, header := range family.Generations {
			events, err := store.Events(header.ID)
			if err != nil {
				t.unreadable++
				continue
			}
			exchanges, err := store.Exchanges(header.ID)
			if err != nil {
				return nil, err
			}
			if t.generation(events, exchanges, w, seenRequests, seenCalls) {
				t.generations++
			}
		}
		if t.generations > before {
			t.sessions++
		}
	}
	return t, nil
}

func (t *tally) generation(events []session.Event, exchanges []session.Exchange, w window, seenRequests, seenCalls map[string]bool) bool {
	used := false
	timed := map[string]bool{}
	for _, exchange := range exchanges {
		timed[exchange.Request] = true
		if seenRequests[exchange.Request] || !w.holds(exchange.At) {
			continue
		}
		seenRequests[exchange.Request], used = true, true
		t.requests++
		t.add(bucketAPI, "", time.Duration(exchange.DurationMS)*time.Millisecond, exchange.At)
	}
	type agentShell struct{ agent, name string }
	calls := map[string]tofuCall{}
	shells := map[agentShell]bucket{}
	for _, event := range events {
		switch event.Kind {
		case session.EventRequest:
			if !timed[event.ID] && !seenRequests[event.ID] && w.holds(event.At) {
				seenRequests[event.ID], used = true, true
				t.requests++
				t.untimedRequests++
			}
		case session.EventToolCall:
			var body session.CallBody
			_ = json.Unmarshal(event.Body, &body)
			call := tofuCall{tool: body.Tool}
			_ = json.Unmarshal(body.Args, &call.args)
			calls[event.Call] = call
		case session.EventToolResult:
			call, known := calls[event.Call]
			if !known || seenCalls[event.Call] || !w.holds(event.At) {
				continue
			}
			seenCalls[event.Call], used = true, true
			var result tofuResult
			_ = json.Unmarshal(event.Body, &result)
			kind := toolBucket(call.tool)
			switch call.tool {
			case "bash":
				kind = commandBucket(call.args.Command)
				for _, name := range shellName.FindAllString(result.Content, -1) {
					shells[agentShell{event.Agent, name}] = kind
				}
			case turn.ShellToolName:
				kind = bucketOtherBash
				if call.args.Op == "wait" {
					kind = bucketWait
					if started, found := shells[agentShell{event.Agent, call.args.Name}]; found {
						kind = started
					}
				}
			}
			t.calls++
			t.add(kind, call.tool, time.Duration(result.DurationMS)*time.Millisecond, event.At)
		}
	}
	return used
}
