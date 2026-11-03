package recall

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"

	"tofu/internal/sys"
)

const storeIDBytes = 16

const storeSuffix = ".bin"

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) Fetch(id string) ([]byte, error) {
	return sys.ReadFile(s.path(id))
}

func (s *Store) put(body []byte) (string, error) {
	id, err := newStoreID()
	if err != nil {
		return "", err
	}
	if err := sys.WriteFile(s.path(id), body, 0o644); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+storeSuffix)
}

func newStoreID() (string, error) {
	noise := make([]byte, storeIDBytes)
	if _, err := rand.Read(noise); err != nil {
		return "", err
	}
	return hex.EncodeToString(noise), nil
}
