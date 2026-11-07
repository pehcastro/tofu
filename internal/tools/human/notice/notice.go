package notice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

const (
	Name     = "heads_up"
	InfoHead = "heads up: "
	WarnHead = "heads up, warning: "
	info     = "info"
	warn     = "warn"
)

type HeadsUp struct {
	Say func(string)
}

func (HeadsUp) Name() string { return Name }

func (HeadsUp) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "tells the person one line at once, on their screen, while you keep working: a thing they should know now, such as a slow step starting, a risk you found, or a result worth seeing before the turn ends. " +
			"it never waits for an answer and never stops the turn; to ask and wait, use ask_person. level is info, or warn for something that may need them. line is one line.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"level": map[string]any{"type": "string", "enum": []string{info, warn}},
				"line":  map[string]any{"type": "string"},
			},
			"required": []string{"level", "line"},
		},
	}
}

func (h HeadsUp) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Level string `json:"level"`
		Line  string `json:"line"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("heads_up: arguments are not the expected shape: %w", err)
	}
	line := strings.TrimSpace(args.Line)
	switch {
	case line == "":
		return contract.Result{}, errors.New("heads_up: line is empty, so nothing was shown; give the one line the person should read")
	case strings.ContainsAny(line, "\r\n"):
		return contract.Result{}, errors.New("heads_up: line holds more than one line, so nothing was shown; say it in one line")
	case h.Say == nil:
		return contract.Result{}, errors.New("heads_up: no screen is attached to this run, so nothing was shown")
	}
	switch args.Level {
	case info:
		h.Say(InfoHead + line)
	case warn:
		h.Say(WarnHead + line)
	default:
		return contract.Result{}, fmt.Errorf("heads_up: level %q is neither %s nor %s, so nothing was shown", args.Level, info, warn)
	}
	return contract.Result{Content: "shown to the person; the turn goes on", Command: args.Level + " " + line}, nil
}
