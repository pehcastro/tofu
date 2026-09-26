package session

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/internal/widget"
)

const (
	planRows        = 8
	planPendingMark = "◇ "
	planRunningMark = "◆ "
	planDoneMark    = "✓ "
	planDroppedMark = "· "
	planPhaseIndent = "  "
)

type PlanState int

const (
	PlanPending PlanState = iota
	PlanRunning
	PlanDone
	PlanDropped
)

type PlanItem struct {
	Phase string
	Text  string
	State PlanState
}

func (i PlanItem) drawn() (string, lipgloss.Style) {
	switch i.State {
	case PlanPending:
		return planPendingMark, look.Style(look.MutedColor)
	case PlanRunning:
		return planRunningMark, look.Style(look.Mint)
	case PlanDone:
		return planDoneMark, look.Style(look.FaintColor)
	case PlanDropped:
		return planDroppedMark, look.Style(look.FaintColor)
	}
	panic("session: unknown plan state")
}

func (m *Model) SetPlan(items []PlanItem) { m.plan = items }

func (m *Model) planLines() []string {
	if len(m.plan) == 0 {
		return nil
	}
	lines := m.planItemLines(m.plan)
	if len(lines) <= planRows {
		return lines
	}
	left := make([]PlanItem, 0, len(m.plan))
	finished := 0
	for _, item := range m.plan {
		if item.State == PlanDone || item.State == PlanDropped {
			finished++
			continue
		}
		left = append(left, item)
	}
	if finished > 0 {
		lines = append([]string{m.planNote(strconv.Itoa(finished) + " finished")}, m.planItemLines(left)...)
		if len(lines) <= planRows {
			return lines
		}
	}
	kept := lines[:planRows-1]
	return append(kept, m.planNote(strconv.Itoa(len(lines)-len(kept))+" more"))
}

func (m *Model) feed() ([]string, int) {
	rows := m.transcriptRows()
	plan := m.planLines()
	if len(plan) >= rows {
		plan = plan[:max(rows-1, 0)]
	}
	return plan, rows - len(plan)
}

func (m *Model) planNote(words string) string {
	return look.Faint(widget.Fit(noteMarker+words, m.textWidth()))
}

func (m *Model) planItemLines(items []PlanItem) []string {
	lines := make([]string, 0, len(items)+1)
	phase := ""
	for _, item := range items {
		if item.Phase != "" && item.Phase != phase {
			lines = append(lines, look.Faint(widget.Fit(planPhaseIndent+item.Phase, m.textWidth())))
		}
		phase = item.Phase
		marker, style := item.drawn()
		indent := ""
		if item.Phase != "" {
			indent = planPhaseIndent
		}
		lines = append(lines, style.Render(widget.Fit(indent+marker+strings.TrimSpace(item.Text), m.textWidth())))
	}
	return lines
}
