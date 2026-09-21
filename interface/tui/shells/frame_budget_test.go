package shells

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/frametime"
)

func TestFrameBudgetWithManyProcessesAndALongLog(t *testing.T) {
	var m Model
	m.SetSize(120, 36)
	log := strings.Repeat("listening on :3000\n", 200)
	entries := make([]Entry, 0, 64)
	for index := range 64 {
		entries = append(entries, Entry{
			Name:    "process-" + strconv.Itoa(index),
			Command: "npm run dev -- --port " + strconv.Itoa(3000+index),
			State:   Running,
			Started: time.Now(),
			Log:     log,
		})
	}
	m.Set(entries)
	m.View()
	frametime.Samples(t, "shells, "+strconv.Itoa(len(entries))+" processes", func() []time.Duration {
		taken := make([]time.Duration, 0, len(entries))
		for pick := range entries {
			m.pick = pick
			start := time.Now()
			m.View()
			taken = append(taken, time.Since(start))
		}
		return taken
	})
}
