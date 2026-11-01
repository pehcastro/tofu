package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"tofu/internal/sys"
)

const cacheSuffix = ".json"

type Request struct {
	State     any
	Questions string
	Model     string
	Version   int
}

type Entry struct {
	RowID     string
	Build     string
	RequestID string
	Answers   []Answer
}

type Cache struct {
	dir string
}

func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

type cacheEntry struct {
	Key       string   `json:"key"`
	Schema    int      `json:"schema"`
	Questions string   `json:"questions"`
	Model     string   `json:"model"`
	Version   int      `json:"version"`
	RowID     string   `json:"row_id"`
	Build     string   `json:"build"`
	RequestID string   `json:"request_id"`
	Answers   []Answer `json:"answers"`
}

func (c *Cache) Key(req Request) (string, error) {
	sum, err := Hash(map[string]any{
		"state":     req.State,
		"questions": req.Questions,
		"model":     req.Model,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v%d-%s", req.Version, sum), nil
}

func (c *Cache) Load(key string) (Entry, bool, error) {
	raw, err := sys.ReadFile(filepath.Join(c.dir, key+cacheSuffix))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Entry{}, false, nil
		}
		return Entry{}, false, err
	}
	var stored cacheEntry
	if err := json.Unmarshal(raw, &stored); err != nil {
		return Entry{}, false, fmt.Errorf("ledger: cache entry %s: %w", key, err)
	}
	if stored.Schema != SchemaVersion {
		return Entry{}, false, nil
	}
	return Entry{RowID: stored.RowID, Build: stored.Build, RequestID: stored.RequestID, Answers: stored.Answers}, true, nil
}

func (c *Cache) Store(key string, req Request, entry Entry) error {
	body, err := Canonical(cacheEntry{
		Key:       key,
		Schema:    SchemaVersion,
		Questions: req.Questions,
		Model:     req.Model,
		Version:   req.Version,
		RowID:     entry.RowID,
		Build:     entry.Build,
		RequestID: entry.RequestID,
		Answers:   entry.Answers,
	})
	if err != nil {
		return err
	}
	return sys.WriteFile(filepath.Join(c.dir, key+cacheSuffix), body, 0o644)
}
