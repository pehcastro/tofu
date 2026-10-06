package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/session"
	"tofu/internal/cron"
)

const (
	cronDialogMargin  = 6
	cronDialogPadding = 2
	cronDialogWidest  = 110
	cronListTitle     = "Crons"
	cronNoJob         = "no cron job is open. /loop 10m <prompt> makes one"
	cronCloseHint     = "esc"
)

type cronDialog struct{ job string }

func (d cronDialog) lines(book *cron.Book, now time.Time) (string, []string) {
	if d.job != "" {
		job, err := book.Job(d.job)
		if err != nil {
			return d.job, []string{err.Error()}
		}
		return "History of " + job.Noun() + " " + job.ID, job.History()
	}
	var lines []string
	for _, job := range book.Jobs() {
		lines = append(lines, job.Line(now))
	}
	if len(lines) == 0 {
		lines = []string{cronNoJob}
	}
	return cronListTitle, lines
}

func (d cronDialog) over(a *App, base string) string {
	title, lines := d.lines(a.options.Host.Cron(), a.options.Now())
	width := min(cronDialogWidest, a.width-cronDialogMargin)
	inner := width - 2*cronDialogPadding
	content := look.Sides(look.Title(title), look.Faint(cronCloseHint), inner) + "\n\n" + strings.Join(lines, "\n")
	rows := lipgloss.Height(lipgloss.NewStyle().Width(inner).Render(content)) + 2
	modal := look.ModalPane(width, min(rows, a.height-cronDialogMargin), look.Panel, cronDialogPadding, content)
	return look.Over(look.Dim(base), modal, max(1, (a.width-lipgloss.Width(modal))/2), max(1, (a.height-lipgloss.Height(modal))/2))
}

func (cronDialog) key(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "enter", "q":
		return a.pop()
	}
	return nil
}

func (cronDialog) click(*App, int, int) tea.Cmd { return nil }

func (a *App) cronCommand() (tea.Cmd, bool) {
	line := a.view.Value()
	if a.options.Host == nil || !cron.IsCommand(line) {
		return nil, false
	}
	a.view.Reset()
	return a.cronLine(line), true
}

func (a *App) cronLine(line string) tea.Cmd {
	reply, err := a.options.Host.CronCommand(line)
	if err != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: "cron: " + err.Error()})
		return nil
	}
	if reply.Note != "" {
		a.view.Append(session.Entry{Kind: session.Note, Body: reply.Note})
	}
	a.countCrons()
	if reply.View != cron.ViewNone {
		return a.push(cronDialog{job: reply.Job})
	}
	return nil
}

func (a *App) countCrons() {
	if a.options.Host != nil {
		a.status.Crons = a.options.Host.Cron().Live()
	}
}
