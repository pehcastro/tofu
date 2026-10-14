package tui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/frame"
	"tofu/internal/host"
	"tofu/internal/llm/quota"
)

func (a *App) followAccount(spent *host.TurnAccount) {
	a.serving[spent.Source] = "#" + strconv.FormatInt(spent.AccountID, 10)
	a.status.Quotas = inSpendOrder(a.status.Quotas, a.serving, a.options.Now())
}

func inSpendOrder(readings []frame.Quota, serving map[string]string, now time.Time) []frame.Quota {
	var sources []string
	var picks []quota.Choice
	for _, reading := range readings {
		if source := reading.Source(); !slices.Contains(sources, source) {
			sources = append(sources, source)
			picks = append(picks, quota.InPickOrder(candidates(readings, source), quota.Provider(source), nil, now)...)
		}
	}
	rank := func(reading frame.Quota) int {
		if held, known := serving[reading.Source()]; known && held == reading.Account {
			return -1
		}
		return slices.IndexFunc(picks, func(pick quota.Choice) bool { return pick.ID == reading.AccountID() })
	}
	ordered := slices.Clone(readings)
	slices.SortStableFunc(ordered, func(one, other frame.Quota) int {
		return cmp.Or(cmp.Compare(slices.Index(sources, one.Source()), slices.Index(sources, other.Source())), cmp.Compare(rank(one), rank(other)))
	})
	return ordered
}

func candidates(readings []frame.Quota, source string) []quota.Candidate {
	var held []quota.Candidate
	for _, reading := range readings {
		if reading.Source() != source {
			continue
		}
		at := slices.IndexFunc(held, func(candidate quota.Candidate) bool { return candidate.ID == reading.AccountID() })
		if at < 0 {
			held = append(held, quota.Candidate{ID: reading.AccountID(), Provider: quota.Provider(source)})
			at = len(held) - 1
		}
		_, window, _ := strings.Cut(reading.Label, " ")
		held[at].Report.Windows = append(held[at].Report.Windows, quota.Window{
			ID: window, Used: quota.Used{Fraction: reading.Fraction, Reported: reading.Reported}, ResetsAt: reading.ResetsAt,
		})
	}
	return held
}
