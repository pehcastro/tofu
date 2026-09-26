package keymap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tofu/internal/sys"
)

const shortcutsVersion = 1

type shortcutFile struct {
	Version  int               `json:"version"`
	Bindings map[string]string `json:"bindings"`
}

func Actions() []string {
	return []string{"Search", "Commands", "Settings", "Models", "Quote selection"}
}

func DefaultShortcuts() map[string]string {
	return map[string]string{
		"Search":          "ctrl+k",
		"Commands":        "alt+k",
		"Settings":        "",
		"Models":          "ctrl+l",
		"Quote selection": "ctrl+r",
	}
}

func ShortcutsPath() (string, error) {
	dir, err := sys.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "keybindings.json"), nil
}

func ValidShortcut(key string) error {
	switch key {
	case "":
		return nil
	case "ctrl+c", "ctrl+v", "ctrl+j", "alt+i":
		return fmt.Errorf("%s is reserved by the composer", key)
	}
	if (strings.HasPrefix(key, "ctrl+") || strings.HasPrefix(key, "alt+")) && !strings.HasSuffix(key, "+") {
		return nil
	}
	if strings.HasPrefix(key, "f") && len(key) <= 3 {
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 12 {
			return nil
		}
	}
	return errors.New("use Ctrl/Alt plus a key, or F1 to F12")
}

func LoadShortcuts(path string) map[string]string {
	bindings := DefaultShortcuts()
	data, err := os.ReadFile(path)
	var file shortcutFile
	if err != nil || json.Unmarshal(data, &file) != nil || file.Version != shortcutsVersion {
		return bindings
	}
	for _, action := range Actions() {
		if key, ok := file.Bindings[action]; ok && ValidShortcut(key) == nil {
			bindings[action] = key
		}
	}
	seen := map[string]bool{}
	for _, key := range bindings {
		if key != "" && seen[key] {
			return DefaultShortcuts()
		}
		seen[key] = true
	}
	return bindings
}

func SaveShortcuts(path string, bindings map[string]string) error {
	data, err := json.MarshalIndent(shortcutFile{shortcutsVersion, bindings}, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFile(path, append(data, '\n'), 0o600)
}
