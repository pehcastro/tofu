package recall

import (
	"fmt"
	"strings"

	"tofu/internal/konst"
)

type Bands struct {
	Identity   int `json:"identity"`
	Facts      int `json:"facts"`
	WorkingSet int `json:"working_set"`
	Recent     int `json:"recent"`
}

const bandShareOfWindow = konst.BandIdentityShare + konst.BandFactsShare + konst.BandWorkingSetShare + konst.BandRecentShare

func BandsOf(ceiling int) Bands {
	return Bands{
		Identity:   ceiling * konst.BandIdentityShare / konst.BandShareWhole,
		Facts:      ceiling * konst.BandFactsShare / konst.BandShareWhole,
		WorkingSet: ceiling * konst.BandWorkingSetShare / konst.BandShareWhole,
		Recent:     ceiling * konst.BandRecentShare / konst.BandShareWhole,
	}
}

func ShippedBands() Bands {
	return BandsOf(konst.ContextCeilingTokens)
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
	ToolSchemas  string
	Facts        []string
	Entries      []Entry
}

type Bill struct {
	CacheRead int
	Fresh     int
}

func (b Bill) Total() int {
	return b.CacheRead + b.Fresh
}

type Occupancy struct {
	Bands      Bands `json:"-"`
	Identity   int   `json:"identity"`
	Facts      int   `json:"facts"`
	WorkingSet int   `json:"working_set"`
	Recent     int   `json:"recent"`
	Target     int   `json:"target"`
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
	occupancy := Occupancy{Bands: bands, Target: bands.Target(), Identity: cfg.Tokens(c.Instructions) + cfg.Tokens(c.ToolSchemas)}
	for _, fact := range c.Facts {
		occupancy.Facts += cfg.Tokens(fact)
	}
	recent := recentFrom(cfg, bands, c.Entries)
	for i, entry := range c.Entries {
		if i < recent {
			occupancy.WorkingSet += cfg.MessageTokens(entry.Text)
			continue
		}
		occupancy.Recent += cfg.MessageTokens(entry.Text)
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
		tokens += cfg.MessageTokens(entries[i].Text)
		if tokens > bands.Recent && entries[i].Step != newestStep {
			return i + 1
		}
	}
	return 0
}
