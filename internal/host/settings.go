package host

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/settings"
	"tofu/internal/sys"
)

type SettingsChanged struct {
	Identity
	SettingsSet
}

type settingsFiles map[SettingScope]map[string]string

type settingsSeen struct {
	mu    sync.Mutex
	files settingsFiles
}

func readSettingsFiles(dir string) settingsFiles {
	home, _ := sys.HomeConfigDir()
	files := settingsFiles{}
	for scope, path := range map[SettingScope]string{SettingGlobal: filepath.Join(home, settings.FileName), SettingProject: filepath.Join(sys.StateDir(dir), settings.FileName)} {
		raw := map[string]json.RawMessage{}
		if body, err := os.ReadFile(path); err == nil && json.Unmarshal(body, &raw) != nil {
			return nil
		}
		files[scope] = map[string]string{}
		for key, value := range raw {
			files[scope][key] = string(value)
		}
	}
	return files
}

func (s *server) setSetting(p SettingsSetParams) (any, error) {
	scope := cmp.Or(p.Scope, SettingGlobal)
	if _, err := verbAs[WriteReceipt](s, "settings", "set", "--scope", string(scope), p.Key, p.Value); err != nil {
		return nil, err
	}
	now, err := verbAs[SettingValue](s, "settings", "get", p.Key)
	if err != nil {
		return nil, err
	}
	s.settingsMoved()
	return SettingsSet{Key: now.Key, Value: now.Value, Scope: scope, Source: now.Source}, nil
}

func (s *server) settingsMoved() {
	s.seen.mu.Lock()
	defer s.seen.mu.Unlock()
	files := readSettingsFiles(s.Dir)
	if files == nil {
		return
	}
	moved := map[string]SettingScope{}
	for _, scope := range []SettingScope{SettingGlobal, SettingProject} {
		for key, value := range files[scope] {
			if was, held := s.seen.files[scope][key]; !held || was != value {
				moved[key] = scope
			}
		}
		for key := range s.seen.files[scope] {
			if _, held := files[scope][key]; !held {
				moved[key] = scope
			}
		}
	}
	s.seen.files = files
	if len(moved) == 0 {
		return
	}
	report, err := verbAs[SettingsReport](s, "settings")
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := s.focus().items
	for _, row := range report.Settings {
		if scope, changed := moved[row.Key]; changed {
			set := SettingsSet{Key: row.Key, Value: row.Value, Scope: scope, Source: row.Source}
			s.box.push(merged("settings.changed", row.Key, &SettingsChanged{Identity: focus.identity("", ""), SettingsSet: set}))
		}
	}
}

func (s *server) watchSettings(quit <-chan struct{}) {
	every := time.NewTicker(konst.ServeShellPollMillis * time.Millisecond)
	defer every.Stop()
	for {
		select {
		case <-quit:
			return
		case <-every.C:
			s.settingsMoved()
		}
	}
}
