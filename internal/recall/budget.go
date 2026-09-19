package recall

import (
	"fmt"
	"strings"

	"boji/internal/konst"
)

type Bands struct {
	Identity   int
	Facts      int
	WorkingSet int
	Recent     int
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

func (o Occupancy) String() string {
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
	}
	var report strings.Builder
	report.WriteString("band          tokens       cap   fill\n")
	for _, row := range rows {
		fill := 0
		if row.limit > 0 {
			fill = row.tokens * 100 / row.limit
		}
		fmt.Fprintf(&report, "%-11s %8d  %8d   %3d%%\n", row.band, row.tokens, row.limit, fill)
	}
	fmt.Fprintf(&report, "ceiling     %8d  %8d   %3d%%\n",
		o.Total(), konst.ContextCeilingTokens, o.Total()*100/konst.ContextCeilingTokens)
	return report.String()
}
