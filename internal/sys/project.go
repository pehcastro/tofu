package sys

import (
	"bytes"
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
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
)

const (
	ProjectRegistryName = "projects.json"
	ProjectFileName     = "project.toml"
	projectLockName     = "projects.lock"
	reapedDirName       = ".reaped"
	projectOpenDirName  = "open"
	projectNameLimit    = 40
	projectHashLength   = 8
	registryLockWait    = 3 * time.Second
	registryRetry       = 50 * time.Millisecond
	registryWriteTries  = 10
)

type ProjectEntry struct {
	Folder    string    `json:"folder"`
	Paths     []string  `json:"paths"`
	Remote    string    `json:"remote,omitempty"`
	Root      string    `json:"root_commit,omitempty"`
	Legacy    string    `json:"legacy,omitempty"`
	Created   time.Time `json:"created"`
	Temporary bool      `json:"temporary,omitempty"`
	Used      time.Time `json:"used,omitzero"`
}

type projectRegistry struct {
	Projects []ProjectEntry `json:"projects"`
}

type Project struct {
	Path         string
	State        string
	Legacy       string
	Registered   bool
	Temporary    bool
	Moved        *ProjectEntry
	home         string
	remote       string
	root         string
	identified   bool
	wasTemporary bool
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

type registeredEntry struct {
	Folder    string   `json:"folder"`
	Paths     []string `json:"paths"`
	Temporary bool     `json:"temporary"`
}

func registeredFolder(home, path string) (registeredEntry, error) {
	name := filepath.Join(home, ProjectRegistryName)
	file, err := os.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return registeredEntry{}, nil
	}
	if err == nil {
		defer func() { _ = file.Close() }()
		var found registeredEntry
		if found, err = folderHolding(json.NewDecoder(file), path); err == nil {
			return found, nil
		}
	}
	return registeredEntry{}, fmt.Errorf("the project registry %s: %w", name, err)
}

func folderHolding(decoder *json.Decoder, path string) (registeredEntry, error) {
	var entry, found registeredEntry
	open, err := decoder.Token()
	if err != nil || open == nil {
		return found, err
	}
	if open != json.Delim('{') {
		return found, fmt.Errorf("it holds %v where an object belongs", open)
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return found, err
		}
		if key != "projects" {
			if err := decoder.Decode(&json.RawMessage{}); err != nil {
				return found, err
			}
			continue
		}
		if list, err := decoder.Token(); err != nil || list != json.Delim('[') {
			return found, err
		}
		for decoder.More() {
			entry = registeredEntry{Paths: entry.Paths[:0]}
			if err := decoder.Decode(&entry); err != nil {
				return found, err
			}
			if found.Folder == "" && slices.ContainsFunc(entry.Paths, func(p string) bool { return samePath(p, path) }) {
				found = registeredEntry{Folder: entry.Folder, Temporary: entry.Temporary}
			}
		}
		if _, err := decoder.Token(); err != nil {
			return found, err
		}
	}
	_, err = decoder.Token()
	return found, err
}

func locateProject(home, dir string) (Project, projectRegistry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Project{}, projectRegistry{}, err
	}
	project := Project{Path: resolvedPath(abs), home: home}
	projects := filepath.Join(home, ProjectsDirName)
	found, err := registeredFolder(home, project.Path)
	if err != nil {
		return Project{}, projectRegistry{}, err
	}
	if found.Folder != "" {
		project.State, project.Registered, project.wasTemporary = filepath.Join(projects, found.Folder), true, found.Temporary
		return project, projectRegistry{}, nil
	}
	registry, err := readProjectRegistry(home)
	if err != nil {
		return Project{}, registry, err
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
	if err != nil || project.Registered || !slices.ContainsFunc(registry.Projects, movedAway) {
		return project, err
	}
	project.identify()
	for i, entry := range registry.Projects {
		sameRepository := project.remote != "" && entry.Remote == project.remote || project.root != "" && entry.Root == project.root
		if sameRepository && movedAway(entry) {
			project.Moved = &registry.Projects[i]
			break
		}
	}
	return project, nil
}

func movedAway(entry ProjectEntry) bool { return !slices.ContainsFunc(entry.Paths, pathExists) }

func pathExists(path string) bool {
	found, _ := Exists(path)
	return found
}

func (p *Project) identify() {
	p.remote, p.root = gitIdentity(p.Path)
	p.identified = true
}

func gitIdentity(dir string) (remote, root string) {
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	}
	var both sync.WaitGroup
	both.Go(func() {
		_ = safe.Call(Crashes(dir), "git identity", func() error {
			root = git("rev-list", "--max-parents=0", "HEAD")
			return nil
		})
	})
	remote = git("remote", "get-url", "origin")
	both.Wait()
	if parsed, err := url.Parse(remote); err == nil && parsed.User != nil {
		parsed.User = nil
		remote = parsed.String()
	}
	return remote, root
}

func (p Project) Register() error {
	if !p.identified {
		p.identify()
	}
	return p.RegisterIdentityLater()
}

func RecordIdentity(dir string) error {
	home, err := HomeConfigDir()
	if err != nil {
		return err
	}
	project := Project{Path: resolvedPath(dir), home: home}
	registry, err := readProjectRegistry(home)
	if entry := registry.holding(project.Path); err != nil || entry == nil || entry.Remote != "" || entry.Root != "" {
		return err
	}
	project.identify()
	return project.editRegistry(func(registry *projectRegistry) (*ProjectEntry, error) {
		entry := registry.holding(project.Path)
		if entry == nil {
			return nil, fmt.Errorf("%s left the project registry while its git identity was read", project.Path)
		}
		entry.Remote, entry.Root = cmp.Or(entry.Remote, project.remote), cmp.Or(entry.Root, project.root)
		return entry, nil
	})
}

func (p Project) RegisterIdentityLater() error {
	return p.editRegistry(func(registry *projectRegistry) (*ProjectEntry, error) {
		if entry := registry.holding(p.Path); entry != nil {
			return entry, nil
		}
		if registry.folder(filepath.Base(p.State)) != nil {
			return nil, fmt.Errorf("the project folder %s was taken by another project while %s was opening", p.State, p.Path)
		}
		entry := ProjectEntry{Folder: filepath.Base(p.State), Paths: []string{p.Path}, Remote: p.remote, Root: p.root, Created: time.Now().UTC(), Temporary: p.Temporary, Used: usedNow(p.Temporary)}
		if p.Legacy != "" {
			entry.Legacy = filepath.Base(p.Legacy)
		}
		registry.Projects = append(registry.Projects, entry)
		return &registry.Projects[len(registry.Projects)-1], nil
	})
}

func usedNow(temporary bool) time.Time {
	if temporary {
		return time.Now().UTC()
	}
	return time.Time{}
}

func (p Project) Reopen() error {
	if !p.wasTemporary {
		return nil
	}
	return p.editRegistry(func(registry *projectRegistry) (*ProjectEntry, error) {
		entry := registry.holding(p.Path)
		if entry == nil {
			return nil, fmt.Errorf("%s left the project registry while it was opening", p.Path)
		}
		entry.Temporary, entry.Used = p.Temporary, usedNow(p.Temporary)
		return entry, nil
	})
}

func ReapTemporaryProjects(home string, now time.Time) ([]string, error) {
	projects := filepath.Join(home, ProjectsDirName)
	reaped, err := reapIdle(home, projects, now)
	if trash := filepath.Join(projects, reapedDirName); pathExists(trash) {
		err = errors.Join(err, os.RemoveAll(trash))
	}
	return reaped, err
}

func reapIdle(home, projects string, now time.Time) ([]string, error) {
	name := filepath.Join(home, ProjectRegistryName)
	raw, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !bytes.Contains(raw, []byte(`"temporary": true`)) {
		return nil, nil
	}
	var read projectRegistry
	if err == nil {
		err = json.Unmarshal(raw, &read)
	}
	if err != nil {
		return nil, fmt.Errorf("the project registry %s: %w", name, err)
	}
	idleSince := now.Add(-konst.TemporaryProjectIdleHours * time.Hour)
	idle := slices.DeleteFunc(read.Projects, func(entry ProjectEntry) bool {
		state := filepath.Join(projects, entry.Folder)
		return !entry.Temporary || !ownStateFolder(entry.Folder) || slices.ContainsFunc(entry.Paths, func(path string) bool { return inside(state, path) }) || usedSince(entry, state, idleSince)
	})
	if len(idle) == 0 {
		return nil, nil
	}
	var reaped []string
	var failed []error
	trash := filepath.Join(projects, reapedDirName)
	err = Project{home: home}.editRegistry(func(registry *projectRegistry) (*ProjectEntry, error) {
		if err := os.MkdirAll(trash, 0o755); err != nil {
			return nil, err
		}
		registry.Projects = slices.DeleteFunc(registry.Projects, func(entry ProjectEntry) bool {
			if !entry.Temporary || !slices.ContainsFunc(idle, func(was ProjectEntry) bool { return was.Folder == entry.Folder && was.Used.Equal(entry.Used) }) {
				return false
			}
			state := filepath.Join(projects, entry.Folder)
			if err := os.Rename(state, filepath.Join(trash, entry.Folder)); err != nil && pathExists(state) {
				failed = append(failed, err)
				return false
			}
			reaped = append(reaped, entry.Folder)
			return true
		})
		return nil, nil
	})
	return reaped, errors.Join(append(failed, err)...)
}

func ownStateFolder(folder string) bool {
	return folder != "." && filepath.IsLocal(folder) && filepath.Base(folder) == folder
}

func usedSince(entry ProjectEntry, state string, since time.Time) bool {
	if entry.Used.After(since) {
		return true
	}
	used := false
	_ = filepath.WalkDir(state, func(path string, file fs.DirEntry, err error) error {
		var info fs.FileInfo
		if err == nil {
			info, err = file.Info()
		}
		if used = err != nil && !errors.Is(err, fs.ErrNotExist) || err == nil && (info.ModTime().After(since) || !file.IsDir() && strings.HasSuffix(file.Name(), "lock") && heldLock(path)); used {
			return fs.SkipAll
		}
		return nil
	})
	return used
}

func HoldProjectOpen(dir string) (release func()) {
	release = func() {}
	home, err := HomeConfigDir()
	if err != nil {
		return release
	}
	project, _, err := locateProject(home, dir)
	open := filepath.Join(project.State, projectOpenDirName)
	if err != nil || !project.Registered || os.MkdirAll(open, 0o755) != nil {
		return release
	}
	stale, _ := filepath.Glob(filepath.Join(open, "*.lock"))
	for _, path := range stale {
		if !heldLock(path) {
			_ = os.Remove(path)
		}
	}
	lock, held, err := tryLock(filepath.Join(open, strconv.Itoa(os.Getpid())+".lock"))
	if err != nil || held {
		return release
	}
	return func() { _ = unlock(lock) }
}

func heldLock(path string) bool {
	file, held, err := tryLock(path)
	if err == nil && !held {
		_ = file.Close()
	}
	return held || err != nil
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
	if entry != nil {
		if err := WriteFile(filepath.Join(p.home, ProjectsDirName, entry.Folder, ProjectFileName), projectTOML(*entry), 0o644); err != nil {
			return err
		}
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
