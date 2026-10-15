package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"tofu/internal/sys"
)

const pinFileName = "jev-pin.json"

type Pin struct {
	Model string `json:"model"`
	Build string `json:"build"`
}

type PinCheck struct {
	From     string   `json:"from"`
	Found    string   `json:"found,omitempty"`
	Newer    string   `json:"newer,omitempty"`
	To       string   `json:"to,omitempty"`
	Shadowed []string `json:"shadowed,omitempty"`
	Skipped  string   `json:"skipped,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func pinPath() (string, error) {
	dir, err := sys.HomeConfigDir()
	return filepath.Join(dir, pinFileName), err
}

func LoadPin() (Pin, error) {
	path, err := pinPath()
	if err != nil {
		return Pin{}, err
	}
	body, err := sys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Pin{}, nil
	}
	if err != nil {
		return Pin{}, err
	}
	var pin Pin
	if err := json.Unmarshal(body, &pin); err != nil || pin.Model == "" || pin.Build == "" {
		return Pin{}, fmt.Errorf("%s does not name a jev model and build", path)
	}
	return pin, nil
}

func (p Pin) Store() error {
	path, err := pinPath()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFile(path, append(body, '\n'), 0o644)
}
