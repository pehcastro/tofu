package quote

import (
	"slices"
	"strconv"
	"strings"

	"tofu/interface/tui/trace"
	isession "tofu/internal/session"
)

type Turn struct {
	Event string
	From  string
	Text  string
}

const (
	stepScope  = "step:"
	youLabel   = "you"
	agentLabel = "the agent"
	toolLabel  = "a tool result"
	openMark   = "[quote"
	closeMark  = "]"
)

func Ref(event string) string { return openMark + trace.Short(event) + closeMark }

func Collect(talk isession.Conversation) []Turn {
	var turns []Turn
	for index, one := range talk.Said {
		var from string
		switch one.Role {
		case isession.RoleUser:
			from = youLabel
		case isession.RoleAssistant:
			from = agentLabel
		case isession.RoleTool:
			from = toolLabel
		default:
			continue
		}
		gist := gistOf(one)
		if gist == "" {
			continue
		}
		event := one.Event
		if event == "" {
			event = isession.EventIDFor(talk.Session, stepScope+strconv.Itoa(index))
		}
		turns = append(turns, Turn{Event: event, From: from, Text: gist})
	}
	slices.Reverse(turns)
	return turns
}

func gistOf(one isession.Utterance) string {
	for line := range strings.SplitSeq(one.Text, "\n") {
		if said := strings.Join(strings.Fields(line), " "); said != "" {
			return said
		}
	}
	names := make([]string, 0, len(one.Calls))
	for _, call := range one.Calls {
		names = append(names, call.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return "ran " + strings.Join(names, ", ")
}
