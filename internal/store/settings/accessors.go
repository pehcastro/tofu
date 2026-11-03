package settings

import (
	"fmt"
	"path/filepath"

	"tofu/internal/sys"
)

func Paths(dir string) (global, project string) {
	home, _ := sys.HomeConfigDir()
	return filepath.Join(home, FileName), filepath.Join(sys.StateDir(dir), FileName)
}

func OpenFor(dir string) (*Store, error) {
	global, project := Paths(dir)
	return Open(global, project)
}

func IntFor(dir, key string) (value int, unreadable string) {
	store, err := OpenFor(dir)
	if err != nil {
		fallback := DeclaredDefault(key)
		return fallback, fmt.Sprintf("%s fell back to its default of %d because the settings file could not be read: %v", key, fallback, err)
	}
	return store.Int(key), ""
}

func TextFor(dir, key string) (value string, unreadable string) {
	store, err := OpenFor(dir)
	if err != nil {
		fallback := DeclaredDefaultText(key)
		return fallback, fmt.Sprintf("%s fell back to its default of %q because the settings file could not be read: %v", key, fallback, err)
	}
	return store.Text(key), ""
}

func IntSaying(dir, key string, say func(string)) int {
	value, unreadable := IntFor(dir, key)
	if unreadable != "" && say != nil {
		say(unreadable)
	}
	return value
}

func TextSaying(dir, key string, say func(string)) string {
	value, unreadable := TextFor(dir, key)
	if unreadable != "" && say != nil {
		say(unreadable)
	}
	return value
}
