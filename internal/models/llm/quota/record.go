package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReadingWindow struct {
	ID       string    `json:"id"`
	Used     float64   `json:"used_fraction"`
	ResetsAt time.Time `json:"resets_at"`
}

type Reading struct {
	Provider Provider        `json:"provider"`
	Account  int64           `json:"account"`
	At       time.Time       `json:"at"`
	Windows  []ReadingWindow `json:"windows"`
}

func ReadingOf(account int64, report Report) Reading {
	windows := make([]ReadingWindow, 0, len(report.Windows))
	for _, window := range report.Windows {
		if !window.Used.Reported {
			continue
		}
		windows = append(windows, ReadingWindow{
			ID:       window.ID,
			Used:     window.Used.Fraction,
			ResetsAt: window.ResetsAt,
		})
	}
	return Reading{Provider: report.Provider, Account: account, At: report.FetchedAt.UTC(), Windows: windows}
}

func AppendReading(dir string, reading Reading) error {
	line, err := json.Marshal(reading)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(dir, reading.At.UTC().Format(time.DateOnly)+readingSuffix)
	file, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	dropDaysBefore(dir, reading.At.Add(-loggedLookback))
	return nil
}

func dropDaysBefore(dir string, cut time.Time) {
	entries, _ := os.ReadDir(dir)
	oldest := cut.UTC().Format(time.DateOnly)
	for _, entry := range entries {
		day, isDay := strings.CutSuffix(entry.Name(), readingSuffix)
		if _, err := time.Parse(time.DateOnly, day); isDay && err == nil && day < oldest {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
