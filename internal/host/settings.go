package host

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm/models"
	"tofu/internal/settings"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	shipped "tofu/library"
)

type SettingsChanged struct {
	Identity
	SettingsSet
}

type settingsFiles map[SettingScope]map[string]string

type settingsSeen struct {
	mu      sync.Mutex
	files   settingsFiles
	session heldSettings
}

type ModelsChange string

const (
	ChangedRole  ModelsChange = "role"
	ChangedAgent ModelsChange = "agent"
	ChangedModel ModelsChange = "model"
)

func (ModelsChange) enum() []string {
	return []string{string(ChangedRole), string(ChangedAgent), string(ChangedModel)}
}

type ModelsChanged struct {
	Identity
	What  ModelsChange `json:"what"`
	Name  string       `json:"name"`
	Model string       `json:"model"`
	Scope SettingScope `json:"scope"`
	File  string       `json:"file"`
}

const (
	rolesFolder   = "roles"
	modelsFolder  = "models"
	catalogFolder = "catalog"
)

type fileMark struct{ modified, size int64 }

type ConfigFiles map[string]fileMark

func configRoots(dir string) map[SettingScope]string {
	roots := map[SettingScope]string{SettingProject: sys.StateDir(dir)}
	if home, err := sys.HomeConfigDir(); err == nil {
		roots[SettingGlobal] = home
	}
	return roots
}

func ConfigSeen(dir string) ConfigFiles {
	seen := ConfigFiles{}
	for _, root := range configRoots(dir) {
		for _, name := range []string{settings.FileName, roster.AssignmentFile, rolesFolder, modelsFolder, catalogFolder} {
			_ = filepath.WalkDir(filepath.Join(root, name), func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return nil
				}
				if info, err := entry.Info(); err == nil {
					seen[path] = fileMark{info.ModTime().UnixNano(), info.Size()}
				}
				return nil
			})
		}
	}
	return seen
}

func libraryAt(dir string) models.Library {
	layers, _ := models.Layers(shipped.Files(), dir)
	loaded, _ := models.Load(layers)
	return loaded
}

func boundOrchestrator(dir string) Pick {
	library := libraryAt(dir)
	model, bound := library.Bound(models.RoleOrchestrator)
	if !bound {
		return Pick{}
	}
	return Pick{Wire: library.WireOf(model), Model: model.Slug()}
}

type agentModels map[SettingScope]map[string]string

func readAgentModels(dir string) agentModels {
	assigned := agentModels{}
	for scope, root := range configRoots(dir) {
		assigned[scope] = map[string]string{}
		body, _ := os.ReadFile(filepath.Join(root, roster.AssignmentFile))
		for line := range strings.Lines(string(body)) {
			name, model, named := strings.Cut(line, ":")
			if named && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				assigned[scope][strings.TrimSpace(name)] = strings.TrimSpace(model)
			}
		}
	}
	return assigned
}

func readSettingsFiles(dir string) settingsFiles {
	files := settingsFiles{}
	for scope, root := range configRoots(dir) {
		raw := map[string]json.RawMessage{}
		if body, err := os.ReadFile(filepath.Join(root, settings.FileName)); err == nil && json.Unmarshal(body, &raw) != nil {
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
	changed := map[string]SettingScope{}
	for _, scope := range []SettingScope{SettingGlobal, SettingProject} {
		for key := range moved(s.seen.files[scope], files[scope]) {
			changed[key] = scope
		}
	}
	s.seen.files = files
	if len(changed) == 0 {
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
		if scope, held := changed[row.Key]; held {
			set := SettingsSet{Key: row.Key, Value: row.Value, Scope: scope, Source: row.Source}
			s.box.push(merged("settings.changed", row.Key, &SettingsChanged{Identity: focus.identity("", ""), SettingsSet: set}))
		}
	}
}

func (s *server) watchSettings(quit <-chan struct{}) {
	files, assigned := ConfigSeen(s.Dir), readAgentModels(s.Dir)
	every := time.NewTicker(konst.ServeShellPollMillis * time.Millisecond)
	defer every.Stop()
	for {
		select {
		case <-quit:
			return
		case <-every.C:
			s.settingsMoved()
			s.sessionSettingsMoved()
			files, assigned = s.modelsMoved(files, assigned)
		}
	}
}

func (s *server) modelsMoved(was ConfigFiles, assigned agentModels) (ConfigFiles, agentModels) {
	now := ConfigSeen(s.Dir)
	if maps.Equal(now, was) {
		return was, assigned
	}
	library := sync.OnceValue(func() models.Library { return libraryAt(s.Dir) })
	files, roots := moved(was, now), configRoots(s.Dir)
	var changes []ModelsChanged
	for scope, root := range roots {
		for path := range files {
			rel, err := filepath.Rel(root, path)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
			switch first {
			case settings.FileName, roster.AssignmentFile:
			case rolesFolder:
				role := models.RoleID(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
				model, _ := library().Bound(role)
				changes = append(changes, ModelsChanged{What: ChangedRole, Name: string(role), Model: model.Slug(), Scope: scope, File: path})
			default:
				changes = append(changes, ModelsChanged{What: ChangedModel, Name: filepath.ToSlash(rel), Scope: scope, File: path})
			}
		}
	}
	nowAssigned := readAgentModels(s.Dir)
	for scope, names := range nowAssigned {
		for name := range moved(assigned[scope], names) {
			changes = append(changes, ModelsChanged{What: ChangedAgent, Name: name, Model: names[name], Scope: scope, File: filepath.Join(roots[scope], roster.AssignmentFile)})
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := s.focus().items
	for _, change := range changes {
		change.Identity = focus.identity("", "")
		s.box.push(merged("models.changed", string(change.What)+" "+string(change.Scope)+" "+change.Name, &change))
	}
	return now, nowAssigned
}

func moved[V comparable](was, now map[string]V) map[string]bool {
	keys := map[string]bool{}
	for key, value := range now {
		if old, held := was[key]; !held || old != value {
			keys[key] = true
		}
	}
	for key := range was {
		if _, held := now[key]; !held {
			keys[key] = true
		}
	}
	return keys
}

func (s *server) sessionSettingsMoved() {
	focus := s.focus()
	asking, pick := focus.host.Settings()
	s.seen.mu.Lock()
	differs := s.seen.session != heldSettings{Asking: asking, Pick: pick}
	s.seen.mu.Unlock()
	if differs {
		s.settingsShown(focus)
	}
}

func (s *server) settingsShown(focus *lane) {
	asking, pick := focus.host.Settings()
	s.seen.mu.Lock()
	s.seen.session = heldSettings{Asking: asking, Pick: pick}
	s.seen.mu.Unlock()
	s.mu.Lock()
	id := focus.items.identity("", "settings")
	s.mu.Unlock()
	s.box.push(merged("session.settings", "", &SessionSettings{Identity: id, Asking: asking, Pick: s.shown(pick)}))
}
