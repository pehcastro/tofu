package sys

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	ProjectRegistryName = "projects.json"
	ProjectFileName     = "project.toml"
	projectLockName     = "projects.lock"
	projectNameLimit    = 40
	projectHashLength   = 8
	registryLockWait    = 3 * time.Second
	registryRetry       = 50 * time.Millisecond
	registryWriteTries  = 10
)

type ProjectEntry struct {
	Folder  string    `json:"folder"`
	Paths   []string  `json:"paths"`
	Remote  string    `json:"remote,omitempty"`
	Root    string    `json:"root_commit,omitempty"`
	Legacy  string    `json:"legacy,omitempty"`
	Created time.Time `json:"created"`
}

type projectRegistry struct {
	Projects []ProjectEntry `json:"projects"`
}

type Project struct {
	Path       string
	State      string
	Legacy     string
	Registered bool
	Moved      *ProjectEntry
	home       string
	remote     string
	root       string
}

func projectKey(path string) string {
	return projectKeyAttempt(resolvedPath(path), 0)
}

func foldsCase() bool { return OS() == "windows" || OS() == "darwin" }

func projectKeyAttempt(resolved string, attempt int) string {
	folded := resolved
	if foldsCase() {
		folded = strings.ToLower(resolved)
	}
	if attempt > 0 {
		folded += "\x00" + strconv.Itoa(attempt)
	}
	sum := sha256.Sum256([]byte(folded))
	name := strings.Trim(dashed(filepath.Base(resolved)), "-")
	if name == "" {
		name = cmp.Or(strings.Trim(dashed(filepath.VolumeName(resolved)), "-"), "root")
	}
	return name[:min(len(name), projectNameLimit)] + "-" + hex.EncodeToString(sum[:])[:projectHashLength]
}

func resolvedPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if final, err := finalPath(abs); err == nil {
		return final
	}
	return abs
}

func legacyProjectKey(path string) string {
	volume := filepath.VolumeName(path)
	return dashed(strings.ToUpper(volume) + path[len(volume):])
}

func dashed(s string) string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
			return r
		}
		return '-'
	}, s)
}

func samePath(a, b string) bool {
	if foldsCase() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (r projectRegistry) holding(path string) *ProjectEntry {
	for i, entry := range r.Projects {
		if slices.ContainsFunc(entry.Paths, func(p string) bool { return samePath(p, path) }) {
			return &r.Projects[i]
		}
	}
	return nil
}

func (r projectRegistry) folder(name string) *ProjectEntry {
	for i, entry := range r.Projects {
		if entry.Folder == name {
			return &r.Projects[i]
		}
	}
	return nil
}

func readProjectRegistry(home string) (projectRegistry, error) {
	var registry projectRegistry
	raw, err := os.ReadFile(filepath.Join(home, ProjectRegistryName))
	if errors.Is(err, fs.ErrNotExist) {
		return registry, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &registry)
	}
	if err != nil {
		return registry, fmt.Errorf("the project registry %s: %w", filepath.Join(home, ProjectRegistryName), err)
	}
	return registry, nil
}

func locateProject(home, dir string) (Project, projectRegistry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Project{}, projectRegistry{}, err
	}
	registry, err := readProjectRegistry(home)
	if err != nil {
		return Project{}, registry, err
	}
	project := Project{Path: resolvedPath(abs), home: home}
	projects := filepath.Join(home, ProjectsDirName)
	if entry := registry.holding(project.Path); entry != nil {
		project.State, project.Registered = filepath.Join(projects, entry.Folder), true
		return project, registry, nil
	}
	key := projectKey(project.Path)
	for attempt := 1; registry.folder(key) != nil; attempt++ {
		key = projectKeyAttempt(project.Path, attempt)
	}
	project.State = filepath.Join(projects, key)
	for _, old := range []string{legacyProjectKey(abs), legacyProjectKey(project.Path)} {
		copied := slices.ContainsFunc(registry.Projects, func(entry ProjectEntry) bool { return entry.Legacy == old })
		if isDir, _ := IsDir(filepath.Join(projects, old)); isDir && !copied {
			project.Legacy = filepath.Join(projects, old)
			break
		}
	}
	return project, registry, nil
}

func (p Project) reading() string {
	if !p.Registered && p.Legacy != "" {
		return p.Legacy
	}
	return p.State
}

func projectStateIn(home, dir string) (string, error) {
	project, _, err := locateProject(home, dir)
	return project.reading(), err
}

func OpenProject(dir string) (Project, error) {
	home, err := HomeConfigDir()
	if err != nil {
		return Project{}, err
	}
	project, registry, err := locateProject(home, dir)
	if err != nil || project.Registered {
		return project, err
	}
	project.remote, project.root = gitIdentity(project.Path)
	for i, entry := range registry.Projects {
		sameRepository := project.remote != "" && entry.Remote == project.remote || project.root != "" && entry.Root == project.root
		if sameRepository && !slices.ContainsFunc(entry.Paths, pathExists) {
			project.Moved = &registry.Projects[i]
			break
		}
	}
	return project, nil
}

func pathExists(path string) bool {
	found, _ := Exists(path)
	return found
}

func gitIdentity(dir string) (remote, root string) {
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	}
	remote = git("remote", "get-url", "origin")
	if parsed, err := url.Parse(remote); err == nil && parsed.User != nil {
		parsed.User = nil
		remote = parsed.String()
	}
	return remote, git("rev-list", "--max-parents=0", "HEAD")
}

func (p Project) Register() error {
	return p.editRegistry(func(registry *projectRegistry) (*ProjectEntry, error) {
		if entry := registry.holding(p.Path); entry != nil {
			return entry, nil
		}
		if registry.folder(filepath.Base(p.State)) != nil {
			return nil, fmt.Errorf("the project folder %s was taken by another project while %s was opening", p.State, p.Path)
		}
		entry := ProjectEntry{Folder: filepath.Base(p.State), Paths: []string{p.Path}, Remote: p.remote, Root: p.root, Created: time.Now().UTC()}
		if p.Legacy != "" {
			entry.Legacy = filepath.Base(p.Legacy)
		}
		registry.Projects = append(registry.Projects, entry)
		return &registry.Projects[len(registry.Projects)-1], nil
	})
}

func (p Project) MoveLegacy() (replacedHalfCopy bool, err error) {
	replacedHalfCopy = pathExists(p.State)
	if err := os.RemoveAll(p.State); err != nil {
		return false, err
	}
	return replacedHalfCopy, os.Rename(p.Legacy, p.State)
}

func (p Project) Relink() (string, error) {
	if p.Moved == nil {
		return "", fmt.Errorf("%s is not a moved copy of a project tofu knows", p.Path)
	}
	err := p.editRegistry(func(registry *projectRegistry) (*ProjectEntry, error) {
		entry := registry.folder(p.Moved.Folder)
		if entry == nil {
			return nil, fmt.Errorf("the project folder %s is no longer in the registry", p.Moved.Folder)
		}
		entry.Paths = append(slices.DeleteFunc(entry.Paths, func(path string) bool { return !pathExists(path) }), p.Path)
		return entry, nil
	})
	return filepath.Join(p.home, ProjectsDirName, p.Moved.Folder), err
}

func (p Project) editRegistry(edit func(*projectRegistry) (*ProjectEntry, error)) error {
	if err := os.MkdirAll(p.home, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(p.home, projectLockName)
	var lock *os.File
	for deadline := time.Now().Add(registryLockWait); lock == nil; {
		file, held, err := tryLock(lockPath)
		switch {
		case err != nil:
			return err
		case !held:
			lock = file
		case time.Now().After(deadline):
			return fmt.Errorf("the project registry lock %s is held by another tofu", lockPath)
		default:
			time.Sleep(registryRetry)
		}
	}
	defer func() { _ = unlock(lock) }()
	registry, err := readProjectRegistry(p.home)
	if err != nil {
		return err
	}
	entry, err := edit(&registry)
	if err != nil {
		return err
	}
	if err := WriteFile(filepath.Join(p.home, ProjectsDirName, entry.Folder, ProjectFileName), projectTOML(*entry), 0o644); err != nil {
		return err
	}
	body, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	for try := 1; ; try++ {
		err := WriteFile(filepath.Join(p.home, ProjectRegistryName), append(body, '\n'), 0o644)
		if err == nil || try == registryWriteTries {
			return err
		}
		time.Sleep(registryRetry)
	}
}

func projectTOML(entry ProjectEntry) []byte {
	quoted := make([]string, len(entry.Paths))
	for i, path := range entry.Paths {
		quoted[i] = strconv.Quote(path)
	}
	return fmt.Appendf(nil, "paths = [%s]\nremote = %s\nroot_commit = %s\ncreated = %s\n",
		strings.Join(quoted, ", "), strconv.Quote(entry.Remote), strconv.Quote(entry.Root), entry.Created.Format(time.RFC3339))
}

func ProjectGlob(projects string, pattern ...string) ([]string, error) {
	found, err := filepath.Glob(filepath.Join(append([]string{projects}, pattern...)...))
	if err != nil {
		return nil, err
	}
	registry, err := readProjectRegistry(filepath.Dir(projects))
	return slices.DeleteFunc(found, func(path string) bool {
		rel, _ := filepath.Rel(projects, path)
		folder, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		return slices.ContainsFunc(registry.Projects, func(entry ProjectEntry) bool { return entry.Legacy == folder })
	}), err
}
