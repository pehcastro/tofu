package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const claimsName = "claims"

type Claim struct {
	Session string    `json:"session"`
	Name    string    `json:"name"`
	Owns    []string  `json:"owns"`
	PID     int       `json:"pid"`
	Since   time.Time `json:"since"`
}

func (s *Store) Claim(side Header) (func() error, error) {
	path := filepath.Join(s.State(), claimsName, side.ID+".json")
	body, err := json.Marshal(Claim{Session: side.ID, Name: side.Named(), Owns: side.Owns, PID: os.Getpid(), Since: time.Now()})
	if err == nil {
		err = s.writeWhole(path, body)
	}
	return func() error { return os.Remove(path) }, err
}

func (s *Store) Claims() ([]Claim, error) {
	entries, err := os.ReadDir(filepath.Join(s.State(), claimsName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var live []Claim
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.State(), claimsName, entry.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var claim Claim
		if err == nil {
			err = json.Unmarshal(raw, &claim)
		}
		if err != nil {
			return nil, err
		}
		if claim.PID == os.Getpid() || alive(claim.PID) {
			live = append(live, claim)
		}
	}
	return live, nil
}
