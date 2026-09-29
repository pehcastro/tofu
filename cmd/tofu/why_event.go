package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"tofu/interface/cli"
	sessionstore "tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

type recordedCall struct {
	Session string           `json:"session"`
	Step    int              `json:"step"`
	Attempt int              `json:"attempt"`
	Call    turn.ToolCallRow `json:"call"`
}

func recordedCallByHash(hash string) (recordedCall, bool, error) {
	if hash == "" {
		return recordedCall{}, false, nil
	}
	store, err := sessionstore.Open()
	if err != nil {
		return recordedCall{}, false, err
	}
	listing, err := store.Listing()
	if err != nil {
		return recordedCall{}, false, err
	}
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			continue
		}
		for _, event := range events {
			if event.Kind != sessionstore.EventStep {
				continue
			}
			var step turn.StepRow
			if err := json.Unmarshal(event.Body, &step); err != nil {
				continue
			}
			for _, call := range step.ToolCalls {
				if sessionstore.DrawnAs(call.ID, hash) {
					return recordedCall{Session: header.ID, Step: step.Index, Attempt: event.Attempt, Call: call}, true, nil
				}
			}
		}
	}
	return recordedCall{}, false, nil
}

func callLines(page cli.Page, found recordedCall) []string {
	call := found.Call
	verdict := cli.Verdict{Mark: cli.Done, Text: widget.Size(call.ResultBytes) + " in " + strconv.FormatInt(call.DurationMS, 10) + " ms"}
	if call.Error != "" {
		verdict = cli.Verdict{Mark: cli.Fail, Text: "failed"}
	}
	attempt := strconv.Itoa(found.Attempt)
	if found.Attempt < sessionstore.FirstAttempt {
		attempt = "not recorded"
	}
	command, _, _ := strings.Cut(call.Command, "\n")
	decision := call.GateDecisionID
	if decision == "" {
		decision = "none recorded"
	}
	facts := []cli.Fact{
		{Label: "session", Text: found.Session},
		{Label: "step", Text: strconv.Itoa(found.Step)},
		{Label: "attempt", Text: attempt},
		{Label: "command", Text: strings.TrimSpace(command)},
		{Label: "error", Text: call.Error},
		{Label: "gate", Text: call.GateVerdict},
		{Label: "decision", Text: decision},
	}
	return append(append(page.Title(call.Tool, []string{call.ID}, verdict), ""), cli.Indent(page.Facts(facts)...)...)
}
