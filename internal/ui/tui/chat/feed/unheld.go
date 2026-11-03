package feed

import (
	"slices"
	"time"
)

type unheldKey struct {
	who            identity
	thinking, talk bool
}

type Unheld struct{ at map[unheldKey][]int64 }

func unheldKeys(e *Event) []unheldKey {
	keys := []unheldKey{{who: actor(e), thinking: e.Kind == KindThinking}}
	if with, _, talking := talk(e); talking {
		keys = append(keys, unheldKey{who: with, talk: true})
	}
	return keys
}

func (u *Unheld) Add(e Event) {
	if u.at == nil {
		u.at = map[unheldKey][]int64{}
	}
	for _, key := range unheldKeys(&e) {
		times := u.at[key]
		place, _ := slices.BinarySearch(times, e.At.UnixNano())
		u.at[key] = slices.Insert(times, place, e.At.UnixNano())
	}
}

func (u *Unheld) Remove(e Event) {
	for _, key := range unheldKeys(&e) {
		times := u.at[key]
		place, _ := slices.BinarySearch(times, e.At.UnixNano())
		switch {
		case place < len(times):
			u.at[key] = slices.Delete(times, place, place+1)
		case len(times) > 0:
			u.at[key] = times[:len(times)-1]
		}
	}
}

func (m *Model) SetUnheld(unheld Unheld) { m.unheld = unheld }

func (m Model) unheldShown() (shown, all [][]int64) {
	if m.retention != KeepAll {
		return nil, nil
	}
	listed := map[identity]bool{{name: orchestrator}: true}
	for _, a := range m.agents {
		listed[identity{a.Name, a.Instance}] = true
	}
	for key, times := range m.unheld.at {
		visible := !key.thinking || !m.thinkingHidden
		if listed[key.who] && !key.talk && visible {
			all = append(all, times)
		}
		if m.filter != (identity{}) && key.who == m.filter && visible {
			shown = append(shown, times)
		}
	}
	switch m.filter {
	case identity{}:
		return all, all
	case identity{name: foldedName}:
		return nil, all
	}
	return shown, all
}

func between(unheld [][]int64, after, before time.Time) int {
	count := 0
	for _, times := range unheld {
		from, to := 0, len(times)
		if !after.IsZero() {
			from, _ = slices.BinarySearch(times, after.UnixNano())
		}
		if !before.IsZero() {
			to, _ = slices.BinarySearch(times, before.UnixNano()+1)
		}
		count += max(0, to-from)
	}
	return count
}

func (s *sheet) place(index int) int {
	at := s.drafts[index].event.At
	timed := func(d draft) bool { return !d.event.At.IsZero() }
	if at.IsZero() && !slices.ContainsFunc(s.drafts[:index], timed) {
		return index + 1
	}
	return index + 1 + between(s.unheld, time.Time{}, at.Add(-time.Nanosecond))
}

func (s *sheet) drawn(index, top, height int) bool {
	return s.starts[index] < top+height && s.starts[index]+s.heights[index] > top
}

type Gap struct {
	Actor         string
	After, Before time.Time
	Upward        bool
}

func (m Model) Gap() (Gap, bool) {
	s, height := m.laid(m.depth(), ""), m.pageHeight()
	top := fromTop(s.rows, height, m.scroll)
	var timed []int
	for index, d := range s.drafts {
		if !d.event.At.IsZero() {
			timed = append(timed, index)
		}
	}
	for at, index := range timed {
		if !s.drawn(index, top, height) {
			continue
		}
		var older, newer time.Time
		if at > 0 {
			older = s.drafts[timed[at-1]].event.At
		}
		if at+1 < len(timed) {
			newer = s.drafts[timed[at+1]].event.At
		}
		here := s.drafts[index].event.At
		if between(s.unheld, older, here) > 0 {
			return Gap{Actor: m.filter.name, After: older, Before: here, Upward: true}, true
		}
		if at+1 < len(timed) && between(s.unheld, here, newer) > 0 {
			return Gap{Actor: m.filter.name, After: here, Before: newer}, true
		}
	}
	return Gap{}, false
}
