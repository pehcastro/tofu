package ledger

import (
	"context"
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

type Asker interface {
	Ask(ctx context.Context, req Request) (Entry, error)
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

func (c *Cache) Resolve(ctx context.Context, req Request, asker Asker) (Entry, bool, error) {
	key, err := c.Key(req)
	if err != nil {
		return Entry{}, false, err
	}
	entry, hit, err := c.Load(key)
	if err != nil {
		return Entry{}, false, err
	}
	if hit {
		return entry, true, nil
	}
	fresh, err := asker.Ask(ctx, req)
	if err != nil {
		return Entry{}, false, err
	}
	if err := c.Store(key, req, fresh); err != nil {
		return Entry{}, false, err
	}
	return fresh, false, nil
}
