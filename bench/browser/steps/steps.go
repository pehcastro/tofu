package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"tofu/bench/stat"
	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
)

type Move struct {
	Op    browser.Op
	Label string
	Value string
	Goal  string
}

func Script() []Move {
	return []Move{
		{browser.OpTypeText, "Destination", "Lis", "Start typing Lis as the destination"},
		{browser.OpClick, "Lisbon", "", "Pick Lisbon from the suggestions"},
		{browser.OpScrollDown, "", "", "Scroll down to the stays"},
		{browser.OpClick, "Casa Flora", "", "Open the first stay in the list"},
		{browser.OpClick, "Casa Azul", "", "Open the second stay in the list"},
		{browser.OpWait, "", "", "Wait for the prices to refresh"},
		{browser.OpScrollDown, "", "", "Scroll down to the booking form"},
		{browser.OpClick, "12", "", "Check in on the twelfth"},
		{browser.OpSelect, "Guests", "2", "Book for two guests"},
		{browser.OpTypeText, "Name", "Ada Lovelace", "Book under the name Ada Lovelace"},
		{browser.OpClick, "Book", "", "Submit the booking"},
		{browser.OpClick, "Back to search", "", "Return to the search page"},
	}
}

func (m Move) String() string {
	switch {
	case m.Label == "":
		return m.Op.String()
	case m.Value == "":
		return fmt.Sprintf("%s %q", m.Op, m.Label)
	}
	return fmt.Sprintf("%s %q %q", m.Op, m.Label, m.Value)
}

func (m Move) on(page browser.Page) (browser.Action, error) {
	action := browser.Action{Op: m.Op, Value: m.Value}
	if m.Label == "" {
		return action, nil
	}
	for _, element := range page.Elements {
		if m.Op.Accepts(element.Role) && strings.EqualFold(strings.TrimSpace(element.Label), m.Label) {
			action.Element = element.Index
			return action, nil
		}
	}
	return action, fmt.Errorf("the page offers no %s target labelled %q", m.Op, m.Label)
}

type Phases struct {
	Wall     float64 `json:"wall_ms"`
	Socket   float64 `json:"socket_ms"`
	Native   float64 `json:"native_ms"`
	Evaluate float64 `json:"evaluate_ms"`
	Settle   float64 `json:"settle_ms"`
	Act      float64 `json:"act_ms"`
	Jev      float64 `json:"jev_ms"`
}

type Row struct {
	Step           int           `json:"step"`
	Move           string        `json:"move"`
	Chooser        string        `json:"chooser"`
	JevChose       string        `json:"jev_chose,omitempty"`
	Stale          browser.Stale `json:"stale,omitempty"`
	Error          string        `json:"error,omitempty"`
	Calls          int           `json:"calls"`
	ExtensionTimed bool          `json:"extension_timed"`
	Phases
	InputTokens  int `json:"jev_input_tokens"`
	OutputTokens int `json:"jev_output_tokens"`
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func Run(ctx context.Context, tab browser.SharedTab, judge *jevloop.Jev, moves []Move) []Row {
	var calls []browser.CallTime
	tab.Client.Timed = func(call browser.CallTime) { calls = append(calls, call) }
	defer func() { tab.Client.Timed = nil }()
	chooser := "scripted"
	if judge != nil {
		chooser = "jev"
	}
	rows := make([]Row, 0, len(moves))
	for i, move := range moves {
		calls = calls[:0]
		row := Row{Step: i + 1, Move: move.String(), Chooser: chooser}
		started := time.Now()
		if err := step(ctx, tab, judge, move, &row); err != nil {
			row.Error = err.Error()
		}
		row.Wall = millis(time.Since(started))
		row.Calls, row.ExtensionTimed = len(calls), len(calls) > 0
		for _, call := range calls {
			row.Socket += millis(call.Wall - call.Host)
			row.Native += millis(call.Host)
			if call.Extension == nil {
				row.ExtensionTimed = false
				continue
			}
			spent := call.Extension
			row.Evaluate += spent.EvaluateMS
			row.Settle += spent.SettleMS
			row.Act += spent.ActMS
			row.Native -= spent.EvaluateMS + spent.SettleMS + spent.ActMS
		}
		rows = append(rows, row)
	}
	return rows
}

func step(ctx context.Context, tab browser.SharedTab, judge *jevloop.Jev, move Move, row *Row) error {
	page, err := tab.Snapshot(ctx)
	if err != nil {
		return err
	}
	action, err := move.on(page)
	if err != nil {
		return err
	}
	if judge != nil {
		choice, err := judge.Choose(ctx, move.Goal, page, nil)
		if err != nil {
			return err
		}
		row.JevChose = choice.Action.Op.String()
		if element, targeted := page.Element(choice.Action.Element); targeted {
			row.JevChose += fmt.Sprintf(" %q", element.Label)
		}
		usage := choice.Decision.Usage
		row.Jev, row.InputTokens, row.OutputTokens = millis(choice.Decision.Latency), usage.InputTokens, usage.OutputTokens
	}
	row.Stale, err = tab.Act(ctx, page, action)
	return err
}

func Write(w io.Writer, rows []Row) error {
	out := json.NewEncoder(w)
	for _, row := range rows {
		if err := out.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func Table(w io.Writer, rows []Row) error {
	columns := []struct {
		name  string
		value func(Row) float64
	}{
		{"wall ms", func(r Row) float64 { return r.Wall }},
		{"socket ms", func(r Row) float64 { return r.Socket }},
		{"native ms", func(r Row) float64 { return r.Native }},
		{"evaluate ms", func(r Row) float64 { return r.Evaluate }},
		{"settle ms", func(r Row) float64 { return r.Settle }},
		{"act ms", func(r Row) float64 { return r.Act }},
		{"jev ms", func(r Row) float64 { return r.Jev }},
		{"jev input tokens", func(r Row) float64 { return float64(r.InputTokens) }},
		{"jev output tokens", func(r Row) float64 { return float64(r.OutputTokens) }},
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %10s %10s\n", "phase", "p50", "p90")
	for _, column := range columns {
		values := make([]float64, len(rows))
		for i, row := range rows {
			values[i] = column.value(row)
		}
		fmt.Fprintf(&b, "%-18s %10.2f %10.2f\n", column.name, stat.Percentile(values, 50), stat.Percentile(values, 90))
	}
	stale, failed, untimed := 0, 0, 0
	for _, row := range rows {
		if row.Stale != browser.StaleNone {
			stale++
		}
		if row.Error != "" {
			failed++
		}
		if !row.ExtensionTimed {
			untimed++
		}
	}
	fmt.Fprintf(&b, "%d steps, %d did not run, %d failed\n", len(rows), stale, failed)
	if untimed > 0 {
		fmt.Fprintf(&b, "the extension timed no phase on %d steps: evaluate, settle and act read 0 there, and native holds them\n", untimed)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
