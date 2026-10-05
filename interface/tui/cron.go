package tui

import (
	"context"
	"slices"
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

type cronTickMsg []cron.Fire

type cronFiresMsg []cron.Fire

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
	title, lines := d.lines(a.options.Cron, a.options.Now())
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
	if a.options.Cron == nil || !cron.IsCommand(line) {
		return nil, false
	}
	a.view.Reset()
	return a.cronLine(line), true
}

func (a *App) cronLine(line string) tea.Cmd {
	reply, err := a.options.Cron.Command(line, a.options.Now())
	if err != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: "cron: " + err.Error()})
		return nil
	}
	if reply.Note != "" {
		a.view.Append(session.Entry{Kind: session.Note, Body: reply.Note})
	}
	var cmds []tea.Cmd
	if reply.View != cron.ViewNone {
		cmds = append(cmds, a.push(cronDialog{job: reply.Job}))
	}
	if reply.CheckGoals && !a.busy {
		cmds = append(cmds, a.checkGoals())
	}
	return tea.Batch(append(cmds, a.armCron())...)
}

func (a *App) armCron() tea.Cmd {
	book := a.options.Cron
	if book == nil {
		return nil
	}
	a.status.Crons = book.Live()
	if a.cronTicking || a.status.Crons == 0 {
		return nil
	}
	a.cronTicking = true
	return tea.Tick(cron.PollMillis*time.Millisecond, func(at time.Time) tea.Msg {
		return cronTickMsg(book.Due(context.Background(), at, cron.Tick))
	})
}

func (a *App) checkGoals() tea.Cmd {
	book, now := a.options.Cron, a.options.Now
	return func() tea.Msg { return cronFiresMsg(book.Due(context.Background(), now(), cron.TurnEnded)) }
}

func (a *App) fire(fires []cron.Fire) tea.Cmd {
	var started tea.Cmd
	for _, fired := range fires {
		a.view.Append(session.Entry{Kind: session.Note, Body: fired.Line})
		if fired.Prompt == "" {
			continue
		}
		a.fired = append(a.fired, fired.ID)
		if a.busy {
			a.steer(fired.Prompt)
			a.unread = append(a.unread, fired)
		} else {
			started = a.startAs(fired.Prompt, fired.ID)
		}
	}
	return tea.Batch(started, a.armCron())
}

func (a *App) cronTurnEnded(stopped bool) tea.Cmd {
	book := a.options.Cron
	if book == nil {
		return nil
	}
	answer, _ := a.view.LastAnswer()
	again := a.unread
	for _, id := range a.fired {
		if slices.ContainsFunc(again, func(fired cron.Fire) bool { return fired.ID == id }) {
			continue
		}
		if note := book.Finished(id, answer); note != "" {
			a.view.Append(session.Entry{Kind: session.Note, Body: note})
		}
	}
	a.fired, a.unread = nil, nil
	var refire tea.Cmd
	if len(again) > 0 {
		for index := range again {
			again[index].Line = again[index].ID + " fired during a turn that ended before reading it, so it starts its own"
		}
		refire = func() tea.Msg { return cronFiresMsg(again) }
	}
	if stopped {
		return tea.Batch(refire, a.armCron())
	}
	return tea.Batch(refire, a.checkGoals(), a.armCron())
}

func (a *App) steerRead(text string) {
	a.unread = slices.DeleteFunc(a.unread, func(fired cron.Fire) bool { return fired.Prompt == text })
}
