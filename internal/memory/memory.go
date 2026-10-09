package memory

import (
	"errors"
	"fmt"
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

func (e Entry) viewed() string { return string(e.Kind) + ": " + e.Ref() + " " + e.Text }

func (e Entry) Undo() string { return "tofu memory remove --scope " + string(e.Scope) + " " + e.ID }

func (e Entry) Saved() string {
	return fmt.Sprintf("%s saved to memory, %s, by %s: %s. The words it came from: %q. It is in the system message from the next turn, so the person does not need to save it again. Undo: %s",
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
	home, state, repo, project               string
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
	shelf := m.shelf(e.Scope)
	e.ID, e.File = m.nextID(), ""
	if e.Scope.local() {
		e.ID = fmt.Sprintf("%s%s%04x", e.ID, idSeparator, rand.IntN(localIDTags))
	}
	if replace != "" {
		old, err := m.Find(e.Scope, replace)
		if err != nil {
			return Entry{}, err
		}
		e.ID, e.File = replace, old.File
	}
	if len(e.viewed()) > konst.MemtreeLineBytes {
		return Entry{}, fmt.Errorf("the entry reads as %d bytes, over the %d a memory line holds; write the rule itself and nothing else", len(e.viewed()), konst.MemtreeLineBytes)
	}
	e, err := m.keep(e, false)
	if err != nil {
		return Entry{}, err
	}
	if replace == "" {
		if err := sys.WriteFile(filepath.Join(m.home, dirName, lastIDFile), []byte(strconv.Itoa(e.number())), 0o644); err != nil {
			return Entry{}, err
		}
	}
	shelf.Entries = append(slices.DeleteFunc(shelf.Entries, func(old Entry) bool { return old.ID == e.ID }), e)
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

func (m Memory) nextID() string {
	highest := 0
	if given, err := os.ReadFile(filepath.Join(m.home, dirName, lastIDFile)); err == nil {
		highest, _ = strconv.Atoi(strings.TrimSpace(string(given)))
	}
	for _, e := range m.All() {
		highest = max(highest, e.number())
	}
	return entryPrefix + strconv.Itoa(highest+1)
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
	case len(rule) > konst.MemoryRuleBytes:
		return "", fmt.Errorf("the statement is %d bytes, over the %d a rule may hold; write the rule itself and nothing else, never the whole message", len(rule), konst.MemoryRuleBytes)
	}
	return rule, nil
}

func PersonIn(statement string) (string, bool) {
	described := regexp.MustCompile(`(?i)\b(?:the\s+(?:person|user|owner|human|developer)|(?:person|user|owner)'s)\b`)
	named := regexp.MustCompile(`(?:(?i:\b(?:he|she|they))|^\s*\p{Lu}\pL*)\s+(?i:wants?|prefers?|likes?|asks?|asked|said|says|needs?|expects?|hates?|told|keeps)\b`)
	found := described.FindString(statement) + named.FindString(statement)
	return found, found != ""
}
