package main

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"tofu/internal/rule"
	"tofu/internal/session"
)

const (
	unreadRefusal      = "has not been read"
	typecheckRan       = "typecheck: "
	backgroundMark     = "background "
	learnedNothingLine = "learned nothing:"
	bashFailedLine     = "dismissed: bash failed"
)

type span struct {
	name       string
	start, end time.Time
}

type request struct {
	who        string
	index      int
	took       time.Duration
	firstToken time.Duration
	tokens     int
	attempt    int
	timed      bool
}

type firstRequest struct {
	who         string
	read, wrote int
}

type timing struct {
	turns          []span
	agents         []span
	longest        request
	backgrounds    int
	background     time.Duration
	firsts         []firstRequest
	staleRefusals  int
	typechecked    map[string]bool
	reports        int
	learnedNothing int
	bashFailed     int
}

type requestBody struct {
	Index            int   `json:"index"`
	CompletionTokens int   `json:"completion_tokens"`
	CacheReadTokens  int   `json:"cache_read_tokens"`
	CacheWriteTokens int   `json:"cache_write_tokens"`
	DurationMS       int64 `json:"duration_ms"`
	FirstTokenMS     int64 `json:"first_token_ms"`
}

type resultBody struct {
	Command     string `json:"command"`
	Content     string `json:"content"`
	ToolOutcome string `json:"tool_outcome"`
	DurationMS  int64  `json:"duration_ms"`
}

func timeline(events []session.Event) timing {
	measured := timing{typechecked: map[string]bool{}}
	last, gaps := map[string]time.Time{}, map[string]time.Duration{}
	turnStarts, agentStarts := map[string]time.Time{}, map[string]time.Time{}
	calls, readUnchanged, firstSeen := map[string]session.CallBody{}, map[string]bool{}, map[string]bool{}
	var turnOrder []string
	for _, event := range events {
		if _, seen := gaps[event.Request]; event.Request != "" && !seen {
			gaps[event.Request] = event.At.Sub(last[event.Agent])
		}
		last[event.Agent] = event.At
		switch event.Kind {
		case session.EventTurnStart:
			if event.Agent != "" {
				agentStarts[event.Agent] = event.At
				continue
			}
			turnStarts[event.Turn] = event.At
			turnOrder = append(turnOrder, event.Turn)
		case session.EventTurnEnd:
			if event.Agent == "" {
				measured.turns = append(measured.turns, span{name: fmt.Sprintf("turn %d", slices.Index(turnOrder, event.Turn)+1), start: turnStarts[event.Turn], end: event.At})
			}
		case session.EventAgentEnd:
			measured.agents = append(measured.agents, span{name: event.Agent, start: agentStarts[event.Agent], end: event.At})
		case session.EventRequest:
			var body requestBody
			_ = json.Unmarshal(event.Body, &body)
			who := event.Agent
			if who == "" {
				who = fmt.Sprintf("orchestrator turn %d", slices.Index(turnOrder, event.Turn)+1)
			}
			made := request{who: who, index: body.Index, took: gaps[event.Request], tokens: body.CompletionTokens, attempt: event.Attempt}
			if body.DurationMS > 0 {
				made.took, made.firstToken, made.timed = time.Duration(body.DurationMS)*time.Millisecond, time.Duration(body.FirstTokenMS)*time.Millisecond, true
			}
			if made.took > measured.longest.took {
				measured.longest = made
			}
			if !firstSeen[who] {
				firstSeen[who] = true
				measured.firsts = append(measured.firsts, firstRequest{who: who, read: body.CacheReadTokens, wrote: body.CacheWriteTokens})
			}
		case session.EventToolCall:
			var body session.CallBody
			_ = json.Unmarshal(event.Body, &body)
			calls[event.Call] = body
		case session.EventToolResult:
			var result resultBody
			_ = json.Unmarshal(event.Body, &result)
			measured.result(calls[event.Call], result, readUnchanged)
		}
	}
	return measured
}

func (t *timing) result(made session.CallBody, result resultBody, readUnchanged map[string]bool) {
	var args writeArgs
	_ = json.Unmarshal(made.Args, &args)
	ran := result.ToolOutcome == "ran"
	switch made.Tool {
	case "read":
		readUnchanged[args.Path] = readUnchanged[args.Path] || ran
	case "write", "edit":
		if !ran {
			if readUnchanged[args.Path] && strings.Contains(result.Content, unreadRefusal) {
				t.staleRefusals++
			}
			return
		}
		readUnchanged[args.Path] = false
		if rule.LanguageOf(args.Path) == "typescript" {
			t.typechecked[args.Path] = t.typechecked[args.Path] || strings.Contains(result.Content, typecheckRan)
		}
	case "bash":
		if strings.HasPrefix(result.Command, backgroundMark) {
			t.backgrounds++
			t.background += time.Duration(result.DurationMS) * time.Millisecond
		}
	case "spawn":
		t.reports++
		report := "\n" + result.Content
		if strings.Contains(report, "\n"+learnedNothingLine) {
			t.learnedNothing++
		}
		if strings.Contains(report, "\n"+bashFailedLine) {
			t.bashFailed++
		}
	}
}

func overlapping(agents []span) int {
	count := 0
	for i, one := range agents {
		for j, other := range agents {
			if i != j && one.start.Before(other.end) && other.start.Before(one.end) {
				count++
				break
			}
		}
	}
	return count
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return seconds(d)
	}
	return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.0fs", d.Seconds())
}

func reportTiming(w io.Writer, t timing) {
	line := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }
	line("where the time went")
	for _, turn := range t.turns {
		line("  %-30s %s", turn.name, clock(turn.end.Sub(turn.start)))
	}
	line("  sub-agents overlapping         %d of %d", overlapping(t.agents), len(t.agents))
	for _, agent := range t.agents {
		line("    %s: %s to %s, %s", agent.name, agent.start.Format(time.TimeOnly), agent.end.Format(time.TimeOnly), seconds(agent.end.Sub(agent.start)))
	}
	longest := t.longest
	source := "measured from the gap between events"
	if longest.timed {
		source = "first token at " + seconds(longest.firstToken)
	}
	line("  longest request                %s request %d, %s for %d tokens, attempt %d, %s", longest.who, longest.index, seconds(longest.took), longest.tokens, longest.attempt, source)
	each := 0.0
	if t.backgrounds > 0 {
		each = t.background.Seconds() / float64(t.backgrounds)
	}
	line("  background starts              %d, %s in all, %.1fs each", t.backgrounds, seconds(t.background), each)
	for _, first := range t.firsts {
		line("  first-request cache            %s: read %d, wrote %d", first.who, first.read, first.wrote)
	}
	line("  refusals on an unchanged read  %d", t.staleRefusals)
	checked := 0
	for _, ran := range t.typechecked {
		if ran {
			checked++
		}
	}
	line("  typescript files typechecked   %d of %d written", checked, len(t.typechecked))
	line("  reports with learned nothing   %d of %d", t.learnedNothing, t.reports)
	line("  reports with bash failed       %d of %d", t.bashFailed, t.reports)
}
