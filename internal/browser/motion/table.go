package motion

import (
	"fmt"
	"math"
	"strings"

	"tofu/internal/konst"
)

type Window struct {
	FromMs float64
	ToMs   float64
}

func WindowOf(takes []Take, from, to *float64) (Window, error) {
	w := Window{FromMs: math.Inf(1), ToMs: math.Inf(-1)}
	widen := func(at *float64) {
		if at != nil {
			w.FromMs, w.ToMs = min(w.FromMs, math.Floor(*at)), max(w.ToMs, math.Ceil(*at))
		}
	}
	for _, t := range takes {
		if t.Manifest.Trigger == nil || t.Manifest.Trigger.WallMs == 0 {
			return w, fmt.Errorf("take %s has no trigger time, the trigger never fired; capture it again", t.Manifest.TakeID)
		}
		for _, f := range t.Frames {
			widen(f.MsFromTrigger)
		}
		for _, s := range t.Trace {
			widen(s.MsFromTrigger)
		}
	}
	if from != nil {
		w.FromMs = *from
	}
	if to != nil {
		w.ToMs = *to
	}
	if !(w.FromMs < w.ToMs) {
		return w, fmt.Errorf("from_ms must be less than to_ms, got %g and %g", w.FromMs, w.ToMs)
	}
	return w, nil
}

func (w Window) holds(at *float64, lagMs float64) bool {
	return at != nil && *at >= w.FromMs && *at <= w.ToMs+lagMs
}

type column struct {
	name     string
	element  string
	geometry bool
	read     func(Element) any
}

func elementColumns() []column {
	return []column{
		{name: "x", geometry: true, read: func(e Element) any { return e.X }},
		{name: "y", geometry: true, read: func(e Element) any { return e.Y }},
		{name: "width", geometry: true, read: func(e Element) any { return e.Width }},
		{name: "height", geometry: true, read: func(e Element) any { return e.Height }},
		{name: "opacity", read: func(e Element) any { return e.Opacity }},
		{name: "display", read: func(e Element) any { return e.Display }},
		{name: "visibility", read: func(e Element) any { return e.Visibility }},
		{name: "hidden", read: func(e Element) any { return e.Hidden }},
		{name: "text", read: func(e Element) any { return e.Text }},
	}
}

func columnsOf(watch []Watch) []column {
	var cols []column
	for _, w := range watch {
		base := elementColumns()
		for _, attribute := range w.Attributes {
			base = append(base, column{name: attribute, read: func(e Element) any { return mapped(e.Attributes, attribute) }})
		}
		for _, style := range w.Styles {
			base = append(base, column{name: style, read: func(e Element) any { return mapped(e.Styles, style) }})
		}
		for _, c := range base {
			c.element, c.name = w.Name, w.Name+" "+c.name
			cols = append(cols, c)
		}
	}
	return cols
}

func mapped(values map[string]string, key string) any {
	if v, ok := values[key]; ok {
		return v
	}
	return nil
}

func (c column) of(e *Element) any {
	if e == nil {
		return nil
	}
	return c.read(*e)
}

func (c column) value(s Sample) any {
	return c.of(s.Elements[c.element])
}

func (c column) differs(a, b Sample) bool {
	return c.unlike(c.value(a), c.value(b))
}

func (c column) unlike(va, vb any) bool {
	if c.geometry && va != nil && vb != nil {
		return math.Abs(va.(float64)-vb.(float64)) >= konst.MotionGeometryThresholdPx
	}
	return va != vb
}

func Table(watch []Watch, trace []Sample, w Window) ([]string, int) {
	var samples []Sample
	for _, s := range trace {
		if w.holds(s.MsFromTrigger, 0) {
			samples = append(samples, s)
		}
	}
	if len(watch) == 0 || len(samples) == 0 {
		return nil, len(samples)
	}
	all := columnsOf(watch)
	printed := []Sample{samples[0]}
	for i := 1; i < len(samples)-1; i++ {
		for _, c := range all {
			if c.differs(printed[len(printed)-1], samples[i]) {
				printed = append(printed, samples[i])
				break
			}
		}
	}
	if len(samples) > 1 {
		printed = append(printed, samples[len(samples)-1])
	}
	var cols []column
	for _, c := range all {
		for i := 1; i < len(printed); i++ {
			if c.differs(printed[i-1], printed[i]) {
				cols = append(cols, c)
				break
			}
		}
	}
	cells := [][]string{{"ms"}}
	for _, c := range cols {
		cells[0] = append(cells[0], c.name)
	}
	for _, s := range printed {
		row := []string{fmt.Sprintf("%+.1f", *s.MsFromTrigger)}
		for _, c := range cols {
			row = append(row, show(c.value(s)))
		}
		cells = append(cells, row)
	}
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, cell := range row {
			widths[i] = max(widths[i], len(cell))
		}
	}
	rows := make([]string, len(cells))
	for r, row := range cells {
		for i, cell := range row {
			row[i] = cell + strings.Repeat(" ", widths[i]-len(cell))
		}
		rows[r] = strings.TrimRight(strings.Join(row, "  "), " ")
	}
	return rows, len(samples)
}

func show(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case float64:
		return fmt.Sprintf("%.2f", v)
	case bool:
		return fmt.Sprint(v)
	case string:
		return v
	}
	panic(fmt.Sprintf("motion: no cell format for %T", v))
}
