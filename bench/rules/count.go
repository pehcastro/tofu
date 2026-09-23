package rules

import (
	"sort"
	"strings"
)

type RuleCount struct {
	RuleID          string
	Mode            string
	Fires           int
	DistinctTargets int
	Days            int
	FirstDay        string
	LastDay         string
}

func (c RuleCount) NeverFired() bool { return c.Fires == 0 }

func Count(catalog []CatalogRule, fires []Fire) []RuleCount {
	byRule := map[string][]Fire{}
	for _, fire := range fires {
		byRule[fire.RuleID] = append(byRule[fire.RuleID], fire)
	}
	counts := make([]RuleCount, len(catalog))
	for i, entry := range catalog {
		counts[i] = countOne(entry, byRule[entry.ID])
	}
	return counts
}

func countOne(entry CatalogRule, fires []Fire) RuleCount {
	c := RuleCount{RuleID: entry.ID, Mode: entry.Mode, Fires: len(fires)}
	if len(fires) == 0 {
		return c
	}
	targets := map[string]bool{}
	days := map[string]bool{}
	for _, fire := range fires {
		targets[strings.ReplaceAll(fire.Target, "\\", "/")] = true
		days[fire.At.Format("2006-01-02")] = true
	}
	c.DistinctTargets = len(targets)
	sortedDays := make([]string, 0, len(days))
	for day := range days {
		sortedDays = append(sortedDays, day)
	}
	sort.Strings(sortedDays)
	c.Days = len(sortedDays)
	c.FirstDay = sortedDays[0]
	c.LastDay = sortedDays[len(sortedDays)-1]
	return c
}
