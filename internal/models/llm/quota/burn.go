package quota

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const readingSuffix = ".jsonl"

func ReadReadings(dir string, since time.Time) ([]Reading, int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var readings []Reading
	torn := 0
	for _, entry := range entries {
		day, isDay := strings.CutSuffix(entry.Name(), readingSuffix)
		if _, err := time.Parse(time.DateOnly, day); !isDay || err != nil || day < since.UTC().Format(time.DateOnly) {
			continue
		}
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, 0, err
		}
		lines := bufio.NewScanner(file)
		for lines.Scan() {
			var reading Reading
			if err := json.Unmarshal(lines.Bytes(), &reading); err != nil {
				torn++
				continue
			}
			if !reading.At.Before(since) {
				readings = append(readings, reading)
			}
		}
		err = cmp.Or(lines.Err(), file.Close())
		if err != nil {
			return nil, 0, err
		}
	}
	slices.SortStableFunc(readings, func(a, b Reading) int { return a.At.Compare(b.At) })
	return readings, torn, nil
}

type Burn struct {
	Provider Provider   `json:"provider"`
	Account  int64      `json:"account"`
	Window   string     `json:"window"`
	Used     float64    `json:"used_fraction"`
	ResetsAt time.Time  `json:"resets_at"`
	Samples  int        `json:"samples"`
	PerHour  *float64   `json:"per_hour"`
	FullAt   *time.Time `json:"full_at"`
}

type sample struct {
	at   time.Time
	used float64
}

func Burns(readings []Reading) []Burn {
	type key struct {
		provider Provider
		account  int64
		window   string
	}
	series, latest := map[key][]sample{}, map[key]ReadingWindow{}
	for _, reading := range readings {
		for _, window := range reading.Windows {
			at := key{reading.Provider, reading.Account, window.ID}
			series[at], latest[at] = append(series[at], sample{reading.At, window.Used}), window
		}
	}
	var burns []Burn
	for at, samples := range series {
		since := len(samples) - 1
		for since > 0 && samples[since-1].used <= samples[since].used {
			since--
		}
		first, last := samples[since], samples[len(samples)-1]
		burn := Burn{Provider: at.provider, Account: at.account, Window: at.window, Used: last.used, ResetsAt: latest[at].ResetsAt, Samples: len(samples) - since}
		if hours := last.at.Sub(first.at).Hours(); hours > 0 {
			perHour := (last.used - first.used) / hours
			burn.PerHour = &perHour
			if perHour > 0 {
				if full := last.at.Add(time.Duration((1 - last.used) / perHour * float64(time.Hour))); burn.ResetsAt.IsZero() || full.Before(burn.ResetsAt) {
					burn.FullAt = &full
				}
			}
		}
		burns = append(burns, burn)
	}
	slices.SortFunc(burns, func(a, b Burn) int {
		return cmp.Or(strings.Compare(string(a.Provider), string(b.Provider)), cmp.Compare(a.Account, b.Account), strings.Compare(a.Window, b.Window))
	})
	return burns
}
