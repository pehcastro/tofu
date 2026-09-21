package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	sessionstore "tofu/internal/session"
	"tofu/internal/turn"
)

type recordedCall struct {
	session string
	step    int
	attempt int
	call    turn.ToolCallRow
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
					return recordedCall{session: header.ID, step: step.Index, attempt: event.Attempt, call: call}, true, nil
				}
			}
		}
	}
	return recordedCall{}, false, nil
}

func printRecordedCall(out io.Writer, found recordedCall) {
	call, attempt := found.call, "attempt "+strconv.Itoa(found.attempt)
	if found.attempt < sessionstore.FirstAttempt {
		attempt = "recorded before an attempt was written down"
	}
	_, _ = fmt.Fprintf(out, "%s  %s  in %s, step %d, %s\n", call.ID, call.Tool, found.session, found.step, attempt)
	line, _, _ := strings.Cut(call.Command, "\n")
	if line = strings.TrimSpace(line); line != "" {
		_, _ = fmt.Fprintf(out, "  %s\n", line)
	}
	_, _ = fmt.Fprintf(out, "  %d bytes back in %d ms\n", call.ResultBytes, call.DurationMS)
	if call.Error != "" {
		_, _ = fmt.Fprintf(out, "  failed: %s\n", call.Error)
	}
	if call.GateVerdict != "" {
		_, _ = fmt.Fprintf(out, "  the gate said %s\n", call.GateVerdict)
	}
	if call.GateDecisionID == "" {
		_, _ = fmt.Fprintln(out, "  no gate decision was recorded for this call")
	}
}
