package output

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	JSONFlag        = "--json"
	JSONLinesSuffix = ".jsonl"
)

func JSONAsked(args []string) bool { return slices.Contains(args, JSONFlag) }

func WithoutJSON(args []string) []string {
	return slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == JSONFlag })
}

func WriteJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func JSONLRecords[T any](dir string, wanted func(name string) bool) ([]T, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	var records []T
	unreadable := 0
	for _, entry := range entries {
		if entry.IsDir() || !wanted(entry.Name()) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, 0, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record T
			if json.Unmarshal([]byte(line), &record) != nil {
				unreadable++
				continue
			}
			records = append(records, record)
		}
	}
	return records, unreadable, nil
}
