package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

type spawn struct {
	call       string
	agent      string
	definition string
	announced  bool
	answered   bool
	verified   bool
}

type turnLines struct {
	turn  string
	lines int
}

type role struct {
	calls          int
	writes         int
	shellWrites    int
	refusedSource  int
	lines          []turnLines
	firstSpawn     int
	firstSpawnTook time.Duration
	spawns         []*spawn
	texts          []string
	narrating      int
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Old     string `json:"old_string"`
	New     string `json:"new_string"`
	Command string `json:"command"`
}

func main() {
	dir := flag.String("session", "", "a session directory, the folder holding session.json and events.jsonl")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "orchestrator: -session names the session directory to measure")
		os.Exit(2)
	}
	events, err := session.NewStore(filepath.Dir(*dir)).Events(filepath.Base(*dir))
	if err != nil {
		fmt.Fprintln(os.Stderr, "orchestrator:", err)
		os.Exit(1)
	}
	report(os.Stdout, filepath.Base(*dir), measure(events))
}

func measure(events []session.Event) role {
	measured := role{firstSpawn: -1}
	texts, toolsIn := map[string]string{}, map[string][]string{}
	for _, event := range events {
		switch {
		case event.Agent != "":
		case event.Kind == session.EventRequest || event.Kind == session.EventStep:
			var step session.StepBody
			if json.Unmarshal(event.Body, &step) == nil && strings.TrimSpace(step.AssistantText) != "" {
				texts[event.ID] = step.AssistantText
				measured.texts = append(measured.texts, step.AssistantText)
			}
		case event.Kind == session.EventToolCall:
			var body session.CallBody
			_ = json.Unmarshal(event.Body, &body)
			toolsIn[event.Request] = append(toolsIn[event.Request], body.Tool)
		}
	}
	for request := range texts {
		if len(toolsIn[request]) > 0 && !slices.Contains(toolsIn[request], "spawn") {
			measured.narrating++
		}
	}
	calls := map[string]session.CallBody{}
	var started time.Time
	for _, event := range events {
		if event.Agent != "" {
			continue
		}
		switch event.Kind {
		case session.EventTurnStart:
			if started.IsZero() {
				started = event.At
			}
		case session.EventToolCall:
			var body session.CallBody
			_ = json.Unmarshal(event.Body, &body)
			calls[event.Call] = body
			measured.calls++
			if body.Tool == "spawn" {
				if measured.firstSpawn < 0 {
					measured.firstSpawn, measured.firstSpawnTook = measured.calls-1, event.At.Sub(started)
				}
				measured.spawns = append(measured.spawns, &spawn{call: event.Call, announced: texts[event.Request] != ""})
				continue
			}
			if slices.Contains([]string{"read", "bash", "search", "glob"}, body.Tool) {
				for _, child := range measured.spawns {
					child.verified = child.verified || child.answered
				}
			}
		case session.EventSpawn:
			var body session.SpawnBody
			_ = json.Unmarshal(event.Body, &body)
			for _, child := range measured.spawns {
				if child.call == event.Call {
					child.agent, child.definition = body.Agent, body.Definition
				}
			}
		case session.EventToolResult:
			var result session.ResultBody
			_ = json.Unmarshal(event.Body, &result)
			measured.count(event.Turn, calls[event.Call], result.ToolOutcome == "ran")
			for _, child := range measured.spawns {
				child.answered = child.answered || child.call == event.Call
			}
		}
	}
	return measured
}

func (r *role) count(turn string, made session.CallBody, ran bool) {
	var args writeArgs
	_ = json.Unmarshal(made.Args, &args)
	lines := 0
	switch made.Tool {
	case "write", "edit":
		r.writes++
		if sourceFile(args.Path) {
			lines = max(linesIn(args.Content), linesIn(args.Old), linesIn(args.New))
		}
	case "bash":
		written, unreadable := subagent.ShellWrites(args.Command)
		if unreadable != nil || slices.ContainsFunc(written, sourceFile) {
			r.shellWrites++
			lines = konst.OrchestratorSourceLinesPerTurn
		}
	}
	if lines == 0 {
		return
	}
	if !ran {
		r.refusedSource++
		return
	}
	if len(r.lines) == 0 || r.lines[len(r.lines)-1].turn != turn {
		r.lines = append(r.lines, turnLines{turn: turn})
	}
	r.lines[len(r.lines)-1].lines += lines
}

func sourceFile(path string) bool {
	language := rule.LanguageOf(path)
	return language != "" && language != "markdown" && language != "yaml"
}

func linesIn(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
}

func report(w io.Writer, id string, r role) {
	line := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }
	line("session %s, the orchestrator alone", id)
	line("  calls                          %d", r.calls)
	line("  write and edit calls           %d", r.writes)
	line("  shell writes to source         %d", r.shellWrites)
	line("  source writes that did not run %d", r.refusedSource)
	if len(r.lines) == 0 {
		line("  source lines changed           0 in every turn, budget %d", konst.OrchestratorSourceLinesPerTurn)
	}
	for _, turn := range r.lines {
		line("  source lines changed           %d in %s, budget %d", turn.lines, turn.turn, konst.OrchestratorSourceLinesPerTurn)
	}
	if r.firstSpawn < 0 {
		line("  calls before the first spawn   none, it never spawned")
	} else {
		line("  calls before the first spawn   %d, %s after the task", r.firstSpawn, r.firstSpawnTook.Round(time.Second))
	}
	announced, verified := 0, 0
	for _, child := range r.spawns {
		if child.announced {
			announced++
		}
		if child.verified {
			verified++
		}
	}
	line("  sub-agents spawned             %d", len(r.spawns))
	line("  text messages                  %d, %d narrate tool work", len(r.texts), r.narrating)
	line("  spawns announced               %d of %d", announced, len(r.spawns))
	line("  child results verified         %d of %d", verified, len(r.spawns))
	for _, child := range r.spawns {
		line("    %s (%s): announced %t, answered %t, verified %t", child.agent, child.definition, child.announced, child.answered, child.verified)
	}
	for i, text := range r.texts {
		line("  text %d: %s", i+1, strings.ReplaceAll(text, "\n", " "))
	}
}
