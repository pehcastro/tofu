package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/session"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
)

const (
	askTool       = "ask"
	bashTool      = "bash"
	shellTool     = "shell"
	stalledDetail = "stalled: no new output, step or request"
)

type movement struct {
	steps, output string
	moved         time.Time
}

func (a *App) stepped(row subagent.Row) {
	steps := fmt.Sprint(row.Steps, row.Calls)
	held, seen := a.moves[row.Name]
	if !seen || held.steps != steps {
		held.moved = a.options.Now()
	}
	held.steps = steps
	a.moves[row.Name] = held
}

func (a *App) printed(entries []shells.Entry) {
	a.liveShells = slices.DeleteFunc(slices.Clone(entries), func(entry shells.Entry) bool { return entry.State != shells.Running })
	outputs := map[string]string{}
	for _, entry := range a.liveShells {
		outputs[entry.Owner] += entry.Name + "\n" + entry.Log + "\n"
	}
	for owner, output := range outputs {
		held, seen := a.moves[owner]
		if !seen || held.output == output {
			continue
		}
		if held.output != "" {
			held.moved = a.options.Now()
		}
		held.output = output
		a.moves[owner] = held
	}
}

func (a *App) watchSubAgents() {
	now, after := a.options.Now(), time.Duration(a.number(isettings.SubAgentWatchSeconds))*time.Second
	var named []session.Activity
	for _, row := range a.subAgents {
		if row.State != roster.Working {
			continue
		}
		doing, shown := a.activityOf(row, now, after)
		if shown {
			named = append(named, doing)
		}
		for _, call := range row.Calls {
			a.markStalled(call, doing.Doing == session.NoProgress)
		}
	}
	a.view.Activity = named
}

func (a *App) activityOf(row subagent.Row, now time.Time, after time.Duration) (session.Activity, bool) {
	var open subagent.Call
	if at := slices.IndexFunc(row.Calls, func(call subagent.Call) bool { return call.Result == "" && !call.At.IsZero() }); at >= 0 {
		open = row.Calls[at]
	}
	opened := now.Sub(open.At) >= after
	moved := a.moves[row.Name].moved
	commanding := open.Tool == bashTool || open.Tool == shellTool
	command, ran := open.Text, open.At
	for _, entry := range a.liveShells {
		if commanding && entry.Owner == row.Name {
			command, ran = entry.Command, open.At
			if entry.Started.Before(open.At) {
				ran = entry.Started
			}
		}
	}
	switch {
	case open.Tool == askTool && opened:
		return session.Activity{Name: row.Name, Doing: session.WaitingOnLead, Since: open.At, What: open.Text}, true
	case !moved.IsZero() && now.Sub(moved) >= after:
		return session.Activity{Name: row.Name, Doing: session.NoProgress, Since: moved, What: strings.TrimSpace(open.Tool + " " + open.Text)}, true
	case commanding && now.Sub(ran) >= after:
		doing := session.RunningBash
		if building(command) {
			doing = session.Building
		}
		return session.Activity{Name: row.Name, Doing: doing, Since: ran, What: command}, true
	case open.Tool != "" && opened:
		return session.Activity{Name: row.Name, Doing: session.RunningTool, Since: open.At, Tool: open.Tool, What: open.Text}, true
	}
	return session.Activity{}, false
}

func (a *App) markStalled(call subagent.Call, stalled bool) {
	at := a.happenedAt(short(call.ID))
	if at < 0 || a.happened[at].State != feed.StateRunning || slices.Contains(a.happened[at].Detail, stalledDetail) == stalled {
		return
	}
	held := a.happened[at]
	held.Detail = slices.DeleteFunc(slices.Clone(held.Detail), func(line string) bool { return line == stalledDetail })
	if stalled {
		held.Detail = append(held.Detail, stalledDetail)
	}
	a.record(held)
}

func building(command string) bool {
	for _, segment := range strings.FieldsFunc(command, func(r rune) bool { return r == '&' || r == '|' || r == ';' }) {
		words := strings.Fields(segment)
		for len(words) > 0 && (words[0] == "rtk" || words[0] == "proxy" || strings.Contains(words[0], "=")) {
			words = words[1:]
		}
		words = append(words, "", "", "")
		tool, verb := words[0], words[1]
		if verb == "run" {
			verb = words[2]
		}
		switch tool {
		case "make", "msbuild", "gradle", "./gradlew", "mvn", "tsc", "ninja":
			return true
		case "go":
			if verb == "build" || verb == "install" {
				return true
			}
		case "cargo":
			if verb == "build" || verb == "check" || verb == "clippy" {
				return true
			}
		case "npm", "pnpm", "bun", "yarn", "dotnet", "zig", "docker", "vite", "cmake":
			if verb == "build" || verb == "--build" {
				return true
			}
		}
	}
	return false
}
