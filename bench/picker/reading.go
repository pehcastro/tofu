package picker

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/llm/quota"
)

type Reading struct {
	Row    int64
	Report quota.Report
}

type Skip struct {
	Path  string
	Field string
}

type Corpus struct {
	Roots    []string
	Files    int
	Rows     int
	Readings []Reading
	Accounts int
	Skips    []Skip
}

type recordedWindow struct {
	ID       string   `json:"id"`
	Used     *float64 `json:"used_fraction"`
	ResetsAt string   `json:"resets_at"`
}

type recordedReading struct {
	Provider string           `json:"provider"`
	Account  *int64           `json:"account"`
	At       string           `json:"at"`
	Windows  []recordedWindow `json:"windows"`
}

func Gather(roots ...string) (Corpus, error) {
	corpus := Corpus{Roots: roots}
	seen := map[int64]bool{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case entry.IsDir():
				return nil
			case !strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".jsonl"):
				return nil
			}
			corpus.Files++
			return corpus.scan(path, seen)
		})
		if err != nil && !os.IsNotExist(err) {
			return Corpus{}, err
		}
	}
	corpus.Accounts = len(seen)
	return corpus, nil
}

func (c *Corpus) scan(path string, seen map[int64]bool) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	windowsKey := []byte(`"windows"`)
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		c.Rows++
		if !bytes.Contains(line, windowsKey) {
			continue
		}
		var recorded recordedReading
		if json.Unmarshal(line, &recorded) != nil || recorded.Windows == nil {
			continue
		}
		reading, field := readingOf(recorded)
		if field != "" {
			c.Skips = append(c.Skips, Skip{Path: path, Field: field})
			continue
		}
		seen[reading.Row] = true
		c.Readings = append(c.Readings, reading)
	}
	return nil
}

func readingOf(recorded recordedReading) (Reading, string) {
	switch {
	case recorded.Provider == "":
		return Reading{}, "provider"
	case recorded.Account == nil:
		return Reading{}, "account"
	}
	at, err := time.Parse(time.RFC3339, recorded.At)
	if err != nil {
		return Reading{}, "at"
	}
	report := quota.Report{Provider: quota.Provider(recorded.Provider), FetchedAt: at}
	for _, window := range recorded.Windows {
		if window.Used == nil {
			return Reading{}, "used_fraction"
		}
		resets, err := time.Parse(time.RFC3339, window.ResetsAt)
		if err != nil {
			return Reading{}, "resets_at"
		}
		report.Windows = append(report.Windows, quota.Window{
			ID:       window.ID,
			Used:     quota.Used{Fraction: *window.Used, Reported: true},
			ResetsAt: resets,
		})
	}
	return Reading{Row: *recorded.Account, Report: report}, ""
}
