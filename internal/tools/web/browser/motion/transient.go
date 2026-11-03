package motion

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
)

type Discovered struct {
	Selector string `json:"selector"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Parent   int    `json:"parent"`
}

type Discovery struct {
	Seen     int          `json:"seen"`
	Cap      int          `json:"cap"`
	Elements []Discovered `json:"elements"`
}

type shift int

const (
	shown shift = iota
	sized
	moved
)

type transient struct {
	element               int
	shift                 shift
	fromMs, toMs          float64
	paints                int
	before, during, after *Element
	score                 float64
}

type scale struct{ area, width, height float64 }

func presence(e *Element) float64 {
	if e == nil || e.Display == "none" || e.Visibility == "hidden" {
		return 0
	}
	return e.Width * e.Height * e.Opacity
}

func scaleOf(elements ...*Element) scale {
	var s scale
	for _, e := range elements {
		if presence(e) > 0 {
			s = scale{max(s.area, presence(e)), max(s.width, e.Width), max(s.height, e.Height)}
		}
	}
	return s
}

func (s scale) apart(a, b *Element) float64 {
	gap := math.Abs(presence(a)-presence(b)) / s.area
	switch {
	case presence(a) == 0 || presence(b) == 0:
		return gap
	case a.Text != b.Text:
		return 1
	}
	return max(gap, math.Abs(a.X-b.X)/s.width, math.Abs(a.Width-b.Width)/s.width, math.Abs(a.Y-b.Y)/s.height, math.Abs(a.Height-b.Height)/s.height)
}

func shiftOf(before *Element, later ...*Element) shift {
	kind := moved
	for _, e := range later {
		switch {
		case presence(before) == 0 || presence(e) == 0 || e.Opacity != before.Opacity || e.Text != before.Text:
			return shown
		case math.Abs(e.Width-before.Width) >= konst.MotionGeometryThresholdPx || math.Abs(e.Height-before.Height) >= konst.MotionGeometryThresholdPx:
			kind = sized
		}
	}
	return kind
}

func transientsOf(trace []Sample, element int) []transient {
	key := strconv.Itoa(element)
	at := func(k int) *Element { return trace[k].Elements[key] }
	var found []transient
	for i := 1; i+1 < len(trace); i++ {
		if s := scaleOf(at(i-1), at(i)); s.area == 0 || s.apart(at(i-1), at(i)) < konst.MotionTransientJump {
			continue
		}
		for j := i; j+1 < len(trace) && *trace[j+1].MsFromTrigger-*trace[i].MsFromTrigger <= konst.MotionTransientMaxMillis; j++ {
			var span []*Element
			for k := max(i-2, 0); k < min(j+3, len(trace)); k++ {
				span = append(span, at(k))
			}
			s := scaleOf(span...)
			entry, exit := s.apart(at(i-1), at(i)), s.apart(at(j), at(j+1))
			steadyBefore := i < 2 || s.apart(at(i-2), at(i-1)) <= konst.MotionTransientReturn
			steadyAfter := j+2 >= len(trace) || s.apart(at(j+1), at(j+2)) <= konst.MotionTransientReturn
			jump := konst.MotionTransientJump
			if entry < jump || exit < jump || s.apart(at(i-1), at(j+1)) > konst.MotionTransientReturn || !steadyBefore || !steadyAfter {
				continue
			}
			from, to := *trace[i].MsFromTrigger, *trace[j+1].MsFromTrigger
			found = append(found, transient{element, shiftOf(at(i-1), at(i), at(j+1)), from, to, j + 1 - i, at(i - 1), at(i), at(j + 1), s.area * (to - from) * min(entry, exit, 1)})
			i = j
			break
		}
	}
	return found
}

func counted(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func eventsOf(t Take) [][]transient {
	elements := t.Manifest.Discovery.Elements
	depth := make([]int, len(elements))
	var all []transient
	for i, el := range elements {
		if el.Parent >= 0 {
			depth[i] = depth[el.Parent] + 1
		}
		all = append(all, transientsOf(t.Trace, i)...)
	}
	slices.SortFunc(all, func(a, b transient) int { return cmp.Compare(a.fromMs, b.fromMs) })
	var events [][]transient
	end := math.Inf(-1)
	for _, tr := range all {
		if tr.fromMs >= end {
			events = append(events, nil)
		}
		events[len(events)-1], end = append(events[len(events)-1], tr), max(end, tr.toMs)
	}
	for _, event := range events {
		slices.SortStableFunc(event, func(a, b transient) int {
			deeper := cmp.Compare(depth[a.element], depth[b.element])
			if a.shift == sized {
				deeper = -deeper
			}
			return cmp.Or(cmp.Compare(a.shift, b.shift), deeper, cmp.Compare(b.score, a.score))
		})
	}
	peak := func(event []transient) float64 {
		return slices.MaxFunc(event, func(a, b transient) int { return cmp.Compare(a.score, b.score) }).score
	}
	slices.SortStableFunc(events, func(a, b []transient) int { return cmp.Compare(peak(b), peak(a)) })
	return events
}

func (t Take) table(blinked []Watch, w Window) ([]string, int) {
	if t.Manifest.Discovery == nil {
		return Table(t.Manifest.Scenario.Watch, t.Trace, w)
	}
	trace := make([]Sample, len(t.Trace))
	for i, s := range t.Trace {
		trace[i] = Sample{MsFromTrigger: s.MsFromTrigger, Elements: map[string]*Element{}}
		for key, e := range s.Elements {
			n, _ := strconv.Atoi(key)
			trace[i].Elements[t.Manifest.Discovery.Elements[n].Selector] = e
		}
	}
	return Table(blinked, trace, w)
}

func blinking(takes []Take) []Watch {
	var watch []Watch
	for _, t := range takes {
		if t.Manifest.Discovery == nil {
			continue
		}
		for _, event := range eventsOf(t) {
			selector := t.Manifest.Discovery.Elements[event[0].element].Selector
			if !slices.ContainsFunc(watch, func(w Watch) bool { return w.Name == selector }) {
				watch = append(watch, Watch{Name: selector, Selector: selector})
			}
		}
	}
	return watch
}

func Report(t Take) []string {
	d := t.Manifest.Discovery
	if d == nil {
		return nil
	}
	lines := []string{fmt.Sprintf("watched %d of %d elements seen", len(d.Elements), d.Seen)}
	if d.Seen > len(d.Elements) {
		lines[0] += fmt.Sprintf(", capped at %d in page order: the rest were not watched", d.Cap)
	}
	events := eventsOf(t)
	if len(events) == 0 {
		return append(lines, fmt.Sprintf("nothing blinked: no watched element changed and changed back within %d ms", konst.MotionTransientMaxMillis))
	}
	for rank, event := range events {
		lead, el := event[0], d.Elements[event[0].element]
		var frames, changes []string
		for i, f := range t.Frames {
			if *f.MsFromTrigger >= lead.fromMs && *f.MsFromTrigger <= lead.toMs+konst.MotionFrameLagMillis {
				frames = append(frames, "#"+strconv.Itoa(i+1))
			}
		}
		for _, c := range elementColumns() {
			before, during, after := c.of(lead.before), c.of(lead.during), c.of(lead.after)
			if c.unlike(before, during) || c.unlike(during, after) {
				changes = append(changes, fmt.Sprintf("%s %s -> %s -> %s", c.name, show(before), show(during), show(after)))
			}
		}
		line := fmt.Sprintf("%d. %s %q at %s: %+.1f ms for %.1f ms (%s), frames %s; %s", rank+1, el.Role, el.Name, el.Selector,
			lead.fromMs, lead.toMs-lead.fromMs, counted(lead.paints, "paint"), cmp.Or(strings.Join(frames, " "), "none"), strings.Join(changes, ", "))
		if len(event) > 1 {
			line += "; " + counted(len(event)-1, "more element") + " changed with it"
		}
		lines = append(lines, line)
	}
	return lines
}
