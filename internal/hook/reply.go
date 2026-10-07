package hook

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"tofu/internal/judge/ledger"
)

type GateFacts struct {
	Verdict   ledger.Verdict  `json:"verdict"`
	Risk      *ledger.Reason  `json:"risk,omitempty"`
	Questions []ledger.Answer `json:"questions"`
}

type SpawnFacts struct {
	Definition string   `json:"definition"`
	Mission    string   `json:"mission"`
	Owns       []string `json:"owns"`
}

type Run struct {
	Event      Event  `json:"event"`
	Command    string `json:"command"`
	File       string `json:"file"`
	Level      Level  `json:"level"`
	Exit       int    `json:"exit"`
	DurationMS int64  `json:"duration_ms"`
	Decision   string `json:"decision,omitempty"`
	Problem    string `json:"problem,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
}

type hookOutput struct {
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	SystemMessage string `json:"systemMessage"`
	Specific      struct {
		EventName        Event           `json:"hookEventName"`
		Permission       string          `json:"permissionDecision"`
		PermissionReason string          `json:"permissionDecisionReason"`
		UpdatedInput     json.RawMessage `json:"updatedInput"`
		Context          string          `json:"additionalContext"`
		Owns             *[]string       `json:"owns"`
	} `json:"hookSpecificOutput"`
}

func fieldsOf(event Event) (top, specific []string) {
	common := []string{"systemMessage", "suppressOutput"}
	switch event {
	case PreToolUse:
		return append(common, "decision", "reason", "hookSpecificOutput"), []string{"permissionDecision", "permissionDecisionReason", "updatedInput", "additionalContext"}
	case PostToolUse, UserPromptSubmit:
		return append(common, "decision", "reason", "hookSpecificOutput"), []string{"additionalContext"}
	case Stop, SubagentStop:
		return append(common, "decision", "reason"), nil
	case SessionStart:
		return append(common, "hookSpecificOutput"), []string{"additionalContext"}
	case SessionEnd:
		return common, nil
	case GateVerdict:
		return append(common, "hookSpecificOutput"), []string{"permissionDecision", "permissionDecisionReason"}
	case SubagentSpawn:
		return append(common, "decision", "reason", "hookSpecificOutput"), []string{"owns"}
	}
	panic("hook: unknown event " + string(event))
}

func parseReply(event Event, out string) (hookOutput, error) {
	var reply hookOutput
	top, specific := fieldsOf(event)
	var fields, inner map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		return reply, err
	}
	if err := onlyFields(event, "", fields, top); err != nil {
		return reply, err
	}
	if raw, held := fields["hookSpecificOutput"]; held {
		if err := json.Unmarshal(raw, &inner); err != nil {
			return reply, fmt.Errorf("hookSpecificOutput is not an object: %w", err)
		}
		if err := onlyFields(event, "hookSpecificOutput.", inner, append(specific, "hookEventName")); err != nil {
			return reply, err
		}
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		return reply, err
	}
	switch {
	case inner != nil && reply.Specific.EventName != event:
		return reply, fmt.Errorf("hookSpecificOutput.hookEventName is %q, and the hook ran on %s", reply.Specific.EventName, event)
	case reply.Decision != "" && reply.Decision != "block":
		return reply, fmt.Errorf("decision is %q, and the only decision is block", reply.Decision)
	case reply.Specific.Permission != "" && rank(ledger.Verdict(reply.Specific.Permission)) < 0:
		return reply, fmt.Errorf("permissionDecision is %q, and it is one of allow, ask or deny", reply.Specific.Permission)
	}
	return reply, nil
}

func onlyFields(event Event, prefix string, fields map[string]json.RawMessage, accepted []string) error {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(accepted, name) {
			return fmt.Errorf("%s does not accept the field %s%s", event, prefix, name)
		}
	}
	return nil
}

func rank(verdict ledger.Verdict) int {
	return slices.Index([]ledger.Verdict{ledger.VerdictAllow, ledger.VerdictAsk, ledger.VerdictDeny}, verdict)
}

func outsideOf(given, asked []string) string {
	for _, glob := range given {
		if !slices.ContainsFunc(asked, func(held string) bool { return inside(glob, held) }) {
			return glob
		}
	}
	return ""
}

func inside(glob, held string) bool {
	given, owned := ownsForm(glob), ownsForm(held)
	if slices.Contains(strings.Split(given, "/"), "..") {
		return false
	}
	given, owned = path.Clean(given), path.Clean(owned)
	dir, tree := strings.CutSuffix(owned, "/**")
	if !tree && !strings.Contains(owned, "*") && path.Ext(owned) == "" {
		dir, tree = owned, true
	}
	return given == owned || owned == "**" || tree && strings.HasPrefix(given, dir+"/")
}

func ownsForm(glob string) string {
	form := strings.ToLower(strings.ReplaceAll(glob, `\`, "/"))
	if dir, isDir := strings.CutSuffix(form, "/"); isDir {
		return dir + "/**"
	}
	return form
}
