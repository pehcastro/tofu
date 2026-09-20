package recall

import (
	"fmt"
	"strings"

	"boji/internal/konst"
)

type Bands struct {
	Identity   int `json:"identity"`
	Facts      int `json:"facts"`
	WorkingSet int `json:"working_set"`
	Recent     int `json:"recent"`
}

func ShippedBands() Bands {
	return Bands{
		Identity:   konst.BandIdentityTokens,
		Facts:      konst.BandFactsTokens,
		WorkingSet: konst.BandWorkingSetTokens,
		Recent:     konst.BandRecentTokens,
	}
}

func (b Bands) Target() int {
	return b.Identity + b.Facts + b.WorkingSet + b.Recent
}

type Entry struct {
	Step         int
	Tool         string
	SupersedeKey string
	Text         string
	Handle       string
}

type Conversation struct {
	Instructions string
	Facts        []string
	Entries      []Entry
}

type Occupancy struct {
	Bands      Bands
	Identity   int
	Facts      int
	WorkingSet int
	Recent     int
}

func (o Occupancy) Total() int {
	return o.Identity + o.Facts + o.WorkingSet + o.Recent
}

func FillPercent(tokens, capacity int) int {
	if capacity <= 0 {
		return 0
	}
	return tokens * 100 / capacity
}

func OccupancyTable(o Occupancy) string {
	rows := []struct {
		band   string
		tokens int
		limit  int
	}{
		{"identity", o.Identity, o.Bands.Identity},
		{"facts", o.Facts, o.Bands.Facts},
		{"working set", o.WorkingSet, o.Bands.WorkingSet},
		{"recent", o.Recent, o.Bands.Recent},
		{"total", o.Total(), o.Bands.Target()},
		{"ceiling", o.Total(), konst.ContextCeilingTokens},
	}
	var report strings.Builder
	report.WriteString("band          tokens       cap   fill\n")
	for _, row := range rows {
		fmt.Fprintf(&report, "%-11s %8d  %8d   %3d%%\n", row.band, row.tokens, row.limit, FillPercent(row.tokens, row.limit))
	}
	return report.String()
}

func Measure(cfg Config, bands Bands, c Conversation) Occupancy {
	occupancy := Occupancy{Bands: bands, Identity: cfg.Tokens(c.Instructions)}
	for _, fact := range c.Facts {
		occupancy.Facts += cfg.Tokens(fact)
	}
	recent := recentFrom(cfg, bands, c.Entries)
	for i, entry := range c.Entries {
		if i < recent {
			occupancy.WorkingSet += cfg.Tokens(entry.Text)
			continue
		}
		occupancy.Recent += cfg.Tokens(entry.Text)
	}
	return occupancy
}

func recentFrom(cfg Config, bands Bands, entries []Entry) int {
	if len(entries) == 0 {
		return 0
	}
	newestStep := entries[len(entries)-1].Step
	tokens := 0
	for i := len(entries) - 1; i >= 0; i-- {
		tokens += cfg.Tokens(entries[i].Text)
		if tokens > bands.Recent && entries[i].Step != newestStep {
			return i + 1
		}
	}
	return 0
}
