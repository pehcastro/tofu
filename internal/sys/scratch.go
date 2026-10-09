package sys

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ScratchDirName      = "scratchpad"
	ScratchShared       = "shared"
	ScratchCache        = "cache"
	ScratchSessions     = "sessions"
	ScratchLead         = "lead"
	ScratchAgents       = "agents"
	ScratchRootsSetting = "scratchRoots"
	scratchSessionFile  = ".session"
	scratchRootsFolder  = "tofu"
	settingsFileName    = "settings.json"
)

type ScratchPlace struct {
	Root      string
	Session   string
	SessionID string
	Agent     string
}

func ScratchRootAt(project string) (string, error) {
	state, err := ProjectStateDirAt(project)
	if err != nil {
		return "", err
	}
	home, err := HomeConfigDir()
	if err != nil {
		return "", err
	}
	if root := firstScratchRoot(filepath.Join(home, settingsFileName)); root != "" {
		return filepath.Join(root, scratchRootsFolder, filepath.Base(state), ScratchDirName), nil
	}
	return filepath.Join(state, ScratchDirName), nil
}

func firstScratchRoot(settings string) string {
	raw, err := os.ReadFile(settings)
	if err != nil {
		return ""
	}
	var values map[string]json.RawMessage
	var roots string
	if json.Unmarshal(raw, &values) != nil || json.Unmarshal(values[ScratchRootsSetting], &roots) != nil {
		return ""
	}
	for _, root := range filepath.SplitList(roots) {
		if filepath.IsAbs(root) {
			return root
		}
	}
	return ""
}

func ScratchFor(project, sessionID, name, tag string) (ScratchPlace, error) {
	root, err := ScratchRootAt(project)
	if err != nil {
		return ScratchPlace{}, err
	}
	folder := tag
	if name != "" {
		folder = name + "-" + tag
	}
	entries, _ := os.ReadDir(filepath.Join(root, ScratchSessions))
	for _, entry := range entries {
		if entry.IsDir() && (entry.Name() == tag || strings.HasSuffix(entry.Name(), "-"+tag)) {
			folder = entry.Name()
		}
	}
	return ScratchPlace{Root: root, Session: folder, SessionID: sessionID}, nil
}

func (p ScratchPlace) SessionDir() string {
	return filepath.Join(p.Root, ScratchSessions, p.Session)
}

func (p ScratchPlace) Dir() string {
	switch {
	case p.Root == "":
		return ""
	case p.Agent == "":
		return filepath.Join(p.SessionDir(), ScratchLead)
	default:
		return filepath.Join(p.SessionDir(), ScratchAgents, p.Agent)
	}
}

func (p ScratchPlace) Tmp() string    { return filepath.Join(p.Dir(), "tmp") }
func (p ScratchPlace) Shared() string { return filepath.Join(p.Root, ScratchShared) }
func (p ScratchPlace) Cache() string  { return filepath.Join(p.Root, ScratchCache) }

func (p ScratchPlace) For(agent string) ScratchPlace {
	p.Agent = agent
	return p
}

func (p ScratchPlace) Make() error {
	for _, dir := range []string{p.Tmp(), filepath.Join(p.Dir(), "logs"), filepath.Join(p.Dir(), "out"), p.Shared(), p.Cache()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if p.SessionID == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(p.SessionDir(), scratchSessionFile), []byte(p.SessionID), 0o644)
}

type scratchKey struct{}

func WithScratch(ctx context.Context, place ScratchPlace) context.Context {
	return context.WithValue(ctx, scratchKey{}, place)
}

func ScratchOf(ctx context.Context) (ScratchPlace, bool) {
	place, found := ctx.Value(scratchKey{}).(ScratchPlace)
	return place, found
}

func ScratchEnv(ctx context.Context) []string {
	place, found := ScratchOf(ctx)
	if !found {
		return nil
	}
	tmp, cache := place.Tmp(), place.Cache()
	pytestCache := filepath.ToSlash(filepath.Join(cache, "pytest"))
	env := []string{
		"TMPDIR=" + tmp,
		"GOTMPDIR=" + tmp,
		"CARGO_TARGET_DIR=" + filepath.Join(cache, "cargo"),
		"PYTHONPYCACHEPREFIX=" + filepath.Join(cache, "pycache"),
		"RUFF_CACHE_DIR=" + filepath.Join(cache, "ruff"),
		"MYPY_CACHE_DIR=" + filepath.Join(cache, "mypy"),
		"PYTEST_ADDOPTS=" + strings.TrimSpace(os.Getenv("PYTEST_ADDOPTS")+` -o "cache_dir=`+pytestCache+`"`),
	}
	if runtime.GOOS == "windows" {
		env = append(env, "TMP="+tmp, "TEMP="+tmp)
	}
	return env
}
