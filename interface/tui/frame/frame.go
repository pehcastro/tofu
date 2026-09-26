package frame

import (
	"strings"
	"time"

	"tofu/interface/tui/look"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	ForkNotice  = "⟳ forking"
	develSuffix = "+dev"
	dirtyMark   = "-dirty"
)

type Head struct {
	Path        string
	Branch      string
	Provider    string
	Model       string
	SessionName string
	SessionID   string
	At          time.Time
	Started     time.Time
	Fresh       bool
	Notice      string
}

type Quota struct {
	Label    string
	Fraction float64
	Reported bool
	ResetsAt time.Time
}

type Context struct {
	Used   int
	Budget int
}

type StatusMode string

const (
	StatusCompact  StatusMode = "compact"
	StatusDetailed StatusMode = "detailed"
	StatusHidden   StatusMode = "hidden"
)

type Status struct {
	Context   Context
	TokensIn  int
	TokensOut int
	CacheRead int
	Decisions int
	Quotas    []Quota
	Agents    int
	At        time.Time
	Note      string
	Release   string
	Mode      StatusMode
	Fresh     bool
}

func Release(buildVersion, buildRevision string) string {
	if buildVersion == sys.DevelVersion || strings.HasSuffix(buildRevision, dirtyMark) {
		return konst.Version + develSuffix
	}
	return konst.Version
}

func Header(head Head, width int) string {
	row, _ := Top(head, nil, 0, width)
	return row
}

func Bar(status Status, width int) string {
	return Footer(status, width, "")
}

type View struct {
	Digit rune
	Name  string
}

type Strip struct {
	Views   []View
	Notice  string
	Current int
	hits    []Hit
}

func (s *Strip) Render(width int) string {
	labels := make([]string, len(s.Views))
	for index, view := range s.Views {
		labels[index] = view.Name
	}
	var nav []span
	nav, s.hits = chips(labels, s.Current)
	return look.ChromeRow(width, look.Background, draw(nav), look.Painted(s.Notice, look.Amber, look.Background))
}

func (s Strip) Hit(column int) (int, bool) {
	for _, hit := range s.hits {
		if column >= hit.Start && column < hit.End {
			return hit.Index, true
		}
	}
	return 0, false
}

type span struct {
	text    string
	fg      look.Color
	bg      look.Color
	session string
}

func fill(cells int, bg look.Color) span {
	return span{text: strings.Repeat(" ", max(0, cells)), fg: look.Text, bg: bg}
}

func panel(text string, fg look.Color) span {
	return span{text: text, fg: fg, bg: look.Panel}
}

func cells(spans []span) int {
	total := 0
	for _, part := range spans {
		total += widget.Cells(part.text)
	}
	return total
}

func draw(spans []span) string {
	var out strings.Builder
	for index := 0; index < len(spans); index++ {
		run := spans[index]
		if run.session != "" {
			out.WriteString(look.TypedID("session", run.session))
			continue
		}
		for index+1 < len(spans) && joins(run, spans[index+1]) {
			index++
			if blank(run.text) {
				run.fg = spans[index].fg
			}
			run.text += spans[index].text
		}
		if run.text != "" {
			out.WriteString(look.Painted(run.text, run.fg, run.bg))
		}
	}
	return out.String()
}

func joins(run, next span) bool {
	return next.session == "" && next.bg == run.bg && (next.fg == run.fg || blank(run.text) || blank(next.text))
}

func blank(text string) bool {
	return strings.TrimLeft(text, " ") == ""
}
