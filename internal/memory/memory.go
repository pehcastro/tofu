package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	dirName     = "memory"
	entryPrefix = "m"
	idSeparator = "-"
	localIDTags = 1 << 16
	lastIDFile  = "last-id"
	mintedDir   = "minted"
)

type Scope string

const (
	Global       Scope = "user-global"
	Project      Scope = "project-global"
	UserLocal    Scope = "user-local"
	ProjectLocal Scope = "project-local"
)

func ParseScope(name string) (Scope, error) {
	switch scope := Scope(name); scope {
	case Global, Project, UserLocal, ProjectLocal:
		return scope, nil
	case "global":
		return Global, nil
	case "project":
		return Project, nil
	}
	return "", fmt.Errorf("a scope is %s, %s, %s or %s, not %q", UserLocal, ProjectLocal, Project, Global, name)
}

func (s *Scope) UnmarshalText(text []byte) error {
	parsed, err := ParseScope(string(text))
	*s = parsed
	return err
}

func (s Scope) local() bool { return s == UserLocal || s == ProjectLocal }

type Kind string

const (
	KindPerson    Kind = "person"
	KindProject   Kind = "project"
	KindReference Kind = "reference"
)

func (k Kind) valid() bool { return k == KindPerson || k == KindProject || k == KindReference }

func (k Kind) Scope() Scope {
	if k == KindPerson {
		return Global
	}
	return Project
}

type By string

const (
	ByPerson By = "person"
	ByOffer  By = "offer"
	ByLead   By = "lead"
)

type Entry struct {
	ID      string    `json:"id"`
	Scope   Scope     `json:"scope"`
	Kind    Kind      `json:"kind"`
	Text    string    `json:"text"`
	Said    string    `json:"said,omitempty"`
	Session string    `json:"session,omitempty"`
	At      time.Time `json:"at"`
	By      By        `json:"by"`
	Author  string    `json:"author"`
	Yours   bool      `json:"yours"`
	File    string    `json:"file"`
}

func (e Entry) Ref() string { return "[memory#" + e.ID + "]" }

func (e Entry) Line() string { return e.Ref() + " " + e.Text }

func (e Entry) viewed() string { return string(e.Kind) + ": " + e.Line() }

func (e Entry) Undo() string { return "tofu memory remove --scope " + string(e.Scope) + " " + e.ID }

func (e Entry) Saved() string {
	return fmt.Sprintf("%s saved to memory, %s, by %s: %s. The words it came from: %q. This session holds it in this note, and the memory that opens every later session carries it, so the person does not need to save it again. Undo: %s",
		e.Ref(), e.Scope, e.By, strings.TrimSuffix(e.Text, "."), e.Said, e.Undo())
}

func (e Entry) number() int {
	counted, _, _ := strings.Cut(strings.TrimPrefix(e.ID, entryPrefix), idSeparator)
	n, _ := strconv.Atoi(counted)
	return n
}

type Shelf struct {
	Scope   Scope   `json:"scope"`
	Dir     string  `json:"dir"`
	Weight  Weight  `json:"weight"`
	Entries []Entry `json:"entries"`
	stores  []string
}

func (s Shelf) Bytes() int {
	total := 0
	for _, e := range s.Entries {
		total += len(e.viewed()) + 1
	}
	return total
}

type Memory struct {
	Global, Project, UserLocal, ProjectLocal Shelf
	Notices                                  []string
	Copied                                   []string
	home, state, repo, project, mine         string
	allUsers                                 bool
	who                                      *who
}

func Open(project string) (Memory, error) {
	full, err := filepath.Abs(project)
	if err != nil {
		return Memory{}, err
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return Memory{}, err
	}
	state, err := sys.ProjectStateDirAt(full)
	if err != nil {
		return Memory{}, err
	}
	weights, err := loadWeights()
	if err != nil {
		return Memory{}, err
	}
	repo := sys.StateDir(full)
	chosen, err := settings.Open(filepath.Join(home, settings.FileName), filepath.Join(repo, settings.FileName))
	if err != nil {
		return Memory{}, err
	}
	m := Memory{home: home, state: state, repo: repo, project: full, allUsers: chosen.Text(settings.MemoryFromAllUsers) == settings.AllUsersOn, who: &who{authors: map[string]string{}}}
	m.Global = Shelf{Scope: Global, Dir: filepath.Join(home, dirName, "user")}
	m.Project = Shelf{Scope: Project, Dir: filepath.Join(state, dirName)}
	m.UserLocal = Shelf{Scope: UserLocal, Dir: filepath.Join(repo, dirName, "user")}
	m.ProjectLocal = Shelf{Scope: ProjectLocal, Dir: filepath.Join(repo, dirName, "project")}
	if err := m.migrate(filepath.Join(home, dirName), m.Project.Dir); err != nil {
		return m, err
	}
	for _, shelf := range m.all() {
		shelf.Weight = weights[shelf.Scope]
		if err := m.read(shelf); err != nil {
			return m, err
		}
	}
	return m, nil
}

func Block(project string) (string, error) {
	m, err := Open(project)
	if err != nil {
		return "", err
	}
	return m.Block()
}

func (m *Memory) all() []*Shelf {
	shelves := []*Shelf{&m.UserLocal, &m.ProjectLocal, &m.Project, &m.Global}
	slices.SortStableFunc(shelves, func(a, b *Shelf) int { return a.Weight.Precedence - b.Weight.Precedence })
	return shelves
}

func (m Memory) Shelves() []Shelf {
	var shelves []Shelf
	for _, shelf := range m.all() {
		shelves = append(shelves, *shelf)
	}
	return shelves
}

func (m Memory) All() []Entry {
	var entries []Entry
	for _, shelf := range m.all() {
		entries = append(entries, shelf.Entries...)
	}
	return entries
}

func (m *Memory) shelf(scope Scope) *Shelf {
	switch scope {
	case Global:
		return &m.Global
	case Project:
		return &m.Project
	case UserLocal:
		return &m.UserLocal
	case ProjectLocal:
		return &m.ProjectLocal
	}
	panic("memory: unknown scope " + string(scope))
}

func (m *Memory) Add(e Entry, replace string) (Entry, error) {
	if strings.TrimSpace(e.Text) == "" || strings.ContainsAny(e.Text, "\r\n") {
		return Entry{}, errors.New("a memory entry is one line of text")
	}
	if !e.Kind.valid() {
		return Entry{}, fmt.Errorf("kind is %s, %s or %s, not %q", KindPerson, KindProject, KindReference, e.Kind)
	}
	kept := m.All()
	old := slices.IndexFunc(kept, func(o Entry) bool { return replace != "" && o.ID == replace })
	if replace != "" && old < 0 {
		return Entry{}, fmt.Errorf("no memory entry is %s, so nothing replaces it", replace)
	}
	inPlace := old >= 0 && kept[old].Scope == e.Scope
	var minted string
	var err error
	if inPlace {
		e.ID, e.File = replace, kept[old].File
	} else {
		if minted, err = m.mint(); err != nil {
			return Entry{}, err
		}
		e.ID, e.File = minted, ""
		if e.Scope.local() {
			e.ID = fmt.Sprintf("%s%s%04x", e.ID, idSeparator, rand.IntN(localIDTags))
		}
	}
	if len(e.viewed()) > konst.MemtreeLineBytes {
		err = fmt.Errorf("the entry is %d words, and its words are too long to fit one memory line; say it in shorter words", len(strings.Fields(e.Text)))
	} else {
		e, err = m.keep(e, false)
	}
	if err != nil && minted != "" {
		err = errors.Join(err, os.Remove(filepath.Join(m.home, dirName, mintedDir, minted)))
	}
	if err != nil {
		return Entry{}, err
	}
	shelf := m.shelf(e.Scope)
	shelf.Entries = append(slices.DeleteFunc(shelf.Entries, func(o Entry) bool { return o.ID == e.ID }), e)
	if old >= 0 && !inPlace {
		if _, err := m.Remove(kept[old].Scope, replace); err != nil {
			return e, fmt.Errorf("kept %s but %s is still there: %w", e.ID, replace, err)
		}
	}
	return e, nil
}

func (m Memory) Find(scope Scope, id string) (Entry, error) {
	shelf := m.shelf(scope)
	at := slices.IndexFunc(shelf.Entries, func(e Entry) bool { return e.ID == id })
	if at < 0 {
		return Entry{}, fmt.Errorf("the %s memory holds no %s", scope, id)
	}
	return shelf.Entries[at], nil
}

func (m *Memory) Remove(scope Scope, id string) (Entry, error) {
	gone, err := m.Find(scope, id)
	if err != nil {
		return Entry{}, err
	}
	if _, err := m.keep(gone, true); err != nil {
		return Entry{}, err
	}
	shelf := m.shelf(scope)
	shelf.Entries = slices.DeleteFunc(shelf.Entries, func(e Entry) bool { return e.ID == id })
	return gone, nil
}

func (m Memory) mint() (string, error) {
	minted := filepath.Join(m.home, dirName, mintedDir)
	if err := os.MkdirAll(minted, 0o755); err != nil {
		return "", err
	}
	highest := 0
	if given, err := os.ReadFile(filepath.Join(m.home, dirName, lastIDFile)); err == nil {
		highest, _ = strconv.Atoi(strings.TrimSpace(string(given)))
	}
	for _, e := range m.All() {
		highest = max(highest, e.number())
	}
	for n := highest + 1; ; n++ {
		id := entryPrefix + strconv.Itoa(n)
		taken, err := os.OpenFile(filepath.Join(minted, id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return id, taken.Close()
	}
}

func Rule(statement string) (string, error) {
	rule := strings.TrimSpace(statement)
	if words, named := PersonIn(rule); named {
		return "", fmt.Errorf("the statement names or describes the person (%q); write the rule itself, naming no one, such as: Replies stay short and plain", words)
	}
	switch {
	case rule == "":
		return "", errors.New("the statement is empty; write the rule itself")
	case strings.ContainsAny(rule, "\r\n"):
		return "", errors.New("the statement is more than one line; write one rule on one line, never the whole message")
	}
	if err := WordsAtMost("the statement", rule, konst.MemoryRuleWordsMost); err != nil {
		return "", fmt.Errorf("%w; write the rule itself and nothing else, never the whole message", err)
	}
	return rule, nil
}

func WordsAtMost(subject, text string, most int) error {
	words := strings.Fields(text)
	if len(words) <= most {
		return nil
	}
	return fmt.Errorf("%s is %d words, %d over the %d; trimmed to %d it reads: %q", subject, len(words), len(words)-most, most, most, strings.Join(words[:most], " "))
}

func PersonIn(statement string) (string, bool) {
	described := regexp.MustCompile(`(?i)\b(?:the\s+(?:person|user|owner|human|developer)|(?:person|user|owner)'s)\b`)
	named := regexp.MustCompile(`(?:(?i:\b(?:he|she|they))|^\s*\p{Lu}\pL*)\s+(?i:wants?|prefers?|likes?|asks?|asked|said|says|needs?|expects?|hates?|told|keeps)\b`)
	found := described.FindString(statement) + named.FindString(statement)
	return found, found != ""
}
