package budget

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/store/session/drain"
	"tofu/internal/store/walls"
	"tofu/internal/tools/agents/context/window"
	"tofu/internal/tools/contract"
)

const Name = "budget"

type Budget struct {
	Sessions *session.Store
	Drain    func(id string) (drain.Report, error)
	Input    func() (drain.Input, error)
}

func (Budget) Name() string { return Name }

func (Budget) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "reports what you have spent, counted exactly as tofu session drain counts it: your current turn, the whole session, the lead and each sub-agent, " +
			"each as calls, weighted tokens, USD and milliseconds, model time summed over requests and wall time from first to last event. " +
			"session names another recorded session by name, id or head; left out it is the one you run in.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"session": map[string]any{"type": "string"}},
		},
	}
}

func timing(events []session.Event, keep func(session.Event) bool) string {
	var model int64
	var first, last time.Time
	for _, event := range events {
		if !keep(event) {
			continue
		}
		if first.IsZero() || event.At.Before(first) {
			first = event.At
		}
		if event.At.After(last) {
			last = event.At
		}
		var body struct {
			DurationMS int64 `json:"duration_ms"`
		}
		if event.Kind == session.EventRequest && json.Unmarshal(event.Body, &body) == nil {
			model += body.DurationMS
		}
		if wall, err := session.DecodeWall(event); err == nil && wall.Op == string(walls.OpRequest) {
			model += wall.MS
		}
	}
	return fmt.Sprintf("model %d ms, wall %d ms", model, last.Sub(first).Milliseconds())
}

func spent(spend drain.Spend) string {
	return fmt.Sprintf("%d calls, %s weighted tokens, %s USD", spend.Calls, strconv.FormatFloat(spend.Weighted, 'f', -1, 64), strconv.FormatFloat(spend.USD, 'f', -1, 64))
}

func (b Budget) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Session string `json:"session"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return contract.Result{}, fmt.Errorf("budget: arguments are not the expected shape: %w", err)
		}
	}
	asked, err := window.Ask(ctx, b.Sessions, args.Session)
	if err != nil {
		return contract.Result{}, fmt.Errorf("budget: %w", err)
	}
	if asked.Header.ID == "" {
		return contract.Result{Content: "no session is recorded in this project yet: 0 calls, 0 weighted tokens, 0 USD\n", Command: "session " + cmp.Or(args.Session, session.HeadHandle)}, nil
	}
	whole, err := b.Drain(asked.Header.ID)
	if err != nil {
		return contract.Result{}, fmt.Errorf("budget: %w", err)
	}
	input, err := b.Input()
	if err != nil {
		return contract.Result{}, fmt.Errorf("budget: %w", err)
	}
	inTurn := func(event session.Event) bool { return event.Agent == asked.Agent && event.Turn == asked.Turn }
	var turn []session.Event
	for _, event := range asked.Events {
		if inTurn(event) {
			turn = append(turn, event)
		}
	}
	var said strings.Builder
	fmt.Fprintf(&said, "budget of session %s for %s, counted as tofu session drain counts it\n", asked.Handle, asked.Who())
	if !whole.CostRecorded {
		said.WriteString("no request recorded a cost, so every USD is 0\n")
	}
	tokens := whole.Tokens
	fmt.Fprintf(&said, "turn %s: %s, %s\n", cmp.Or(asked.Turn, "none"), spent(drain.Read(asked.Header.ID, turn, input).Total), timing(asked.Events, inTurn))
	fmt.Fprintf(&said, "session: %s, %s\n", spent(whole.Total), timing(asked.Events, func(session.Event) bool { return true }))
	fmt.Fprintf(&said, "  tokens: input %d, output %d, cache read %d, cache write 5m %d, cache write 1h %d\n", tokens.Input, tokens.Output, tokens.Read, tokens.Write5m, tokens.Write1h)
	fmt.Fprintf(&said, "lead: %s, %s\n", spent(whole.Lead), timing(asked.Events, func(event session.Event) bool { return event.Agent == "" }))
	fmt.Fprintf(&said, "memory writer, outside the session total: %s\n", spent(whole.Memory))
	fmt.Fprintf(&said, "sub-agents: %d of %d\n", min(len(whole.Agents), konst.BudgetSubAgentsShown), len(whole.Agents))
	for _, run := range whole.Agents[:min(len(whole.Agents), konst.BudgetSubAgentsShown)] {
		fmt.Fprintf(&said, "  %s (%s): %s, peak context %d, %s\n", run.Name, run.Type, spent(run.Spend), run.PeakContext,
			timing(asked.Events, func(event session.Event) bool { return event.Agent == run.Name }))
	}
	return contract.Result{Content: said.String(), Command: "session " + asked.Handle}, nil
}
