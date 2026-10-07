package memory

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const (
	dirName     = "memory"
	entryPrefix = "m"
	fileSuffix  = ".yaml"
	lastIDFile  = "last-id"
	blockHead   = "remembered for you, global first, then this project, which outranks global where they disagree:"
)

type Scope string

const (
	Global  Scope = "global"
	Project Scope = "project"
)

type Kind string

const (
	KindPerson    Kind = "person"
	KindProject   Kind = "project"
	KindReference Kind = "reference"
)

func (k Kind) valid() bool { return k == KindPerson || k == KindProject || k == KindReference }

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
	File    string    `json:"file"`
}

func (e Entry) Ref() string { return "[memory#" + e.ID + "]" }

func (e Entry) line() string {
	return "- " + e.Ref() + " " + e.At.Format(time.DateOnly) + ": " + e.Text + "\n"
}

func (e Entry) Cost() int { return len(e.line()) }

func (e Entry) Undo() string {
	if e.Scope == Global {
		return "tofu memory remove --global " + e.ID
	}
	return "tofu memory remove " + e.ID
}

func (e Entry) Saved() string {
	return fmt.Sprintf("%s saved to memory, %s, by %s: %s. The words it came from: %q. It is in the system message from the next turn, so the person does not need to save it again. Undo: %s",
		e.Ref(), e.Scope, e.By, strings.TrimSuffix(e.Text, "."), e.Said, e.Undo())
}

type Shelf struct {
	Scope   Scope   `json:"scope"`
	Dir     string  `json:"dir"`
	Entries []Entry `json:"entries"`
}

func (s Shelf) Bytes() int {
	total := 0
	for _, e := range s.Entries {
		total += e.Cost()
	}
	return total
}

type Memory struct {
	Global, Project Shelf
}

type FullError struct {
	Shelf Shelf
	Need  int
}

func (e FullError) Error() string {
	return fmt.Sprintf("the %s memory holds %d of %d bytes, and this entry needs %d", e.Shelf.Scope, e.Shelf.Bytes(), konst.MemoryScopeBytes, e.Need)
}

func GlobalDir() (string, error) {
	home, err := sys.HomeConfigDir()
	return filepath.Join(home, dirName), err
}

func Open(project string) (Memory, error) {
	global, err := GlobalDir()
	if err != nil {
		return Memory{}, err
	}
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		return Memory{}, err
	}
	return Read(global, filepath.Join(state, dirName))
}

func Block(project string) (string, error) {
	m, err := Open(project)
	return m.Block(), err
}

func Read(globalDir, projectDir string) (Memory, error) {
	global, err := readShelf(Global, globalDir)
	if err != nil {
		return Memory{}, err
	}
	project, err := readShelf(Project, projectDir)
	return Memory{Global: global, Project: project}, err
}

func (m *Memory) shelf(scope Scope) *Shelf {
	switch scope {
	case Global:
		return &m.Global
	case Project:
		return &m.Project
	}
	panic("memory: unknown scope " + string(scope))
}

func (m Memory) Block() string {
	entries := append(slices.Clone(m.Global.Entries), m.Project.Entries...)
	if len(entries) == 0 {
		return ""
	}
	var block strings.Builder
	block.WriteString(blockHead + "\n")
	for _, e := range entries {
		block.WriteString(e.line())
	}
	return block.String()
}

func (m *Memory) Add(e Entry, replace string) (Entry, error) {
	if strings.TrimSpace(e.Text) == "" || strings.ContainsAny(e.Text, "\r\n") {
		return Entry{}, errors.New("a memory entry is one line of text")
	}
	if !e.Kind.valid() {
		return Entry{}, fmt.Errorf("kind is %s, %s or %s, not %q", KindPerson, KindProject, KindReference, e.Kind)
	}
	shelf := m.shelf(e.Scope)
	kept := shelf.Entries
	e.ID = m.nextID()
	if replace != "" {
		if _, err := m.Find(e.Scope, replace); err != nil {
			return Entry{}, err
		}
		e.ID, kept = replace, slices.DeleteFunc(slices.Clone(kept), func(old Entry) bool { return old.ID == replace })
	}
	if (Shelf{Entries: kept}).Bytes()+e.Cost() > konst.MemoryScopeBytes {
		return Entry{}, FullError{Shelf: *shelf, Need: e.Cost()}
	}
	e.File = filepath.Join(shelf.Dir, e.ID+fileSuffix)
	if err := sys.WriteFile(e.File, e.encode(), 0o644); err != nil {
		return Entry{}, err
	}
	if replace == "" {
		if err := sys.WriteFile(filepath.Join(m.Global.Dir, lastIDFile), []byte(strconv.Itoa(e.number())), 0o644); err != nil {
			return Entry{}, err
		}
	}
	shelf.Entries = slices.Concat(kept, []Entry{e})
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
	if err := os.Remove(gone.File); err != nil {
		return Entry{}, err
	}
	shelf := m.shelf(scope)
	shelf.Entries = slices.DeleteFunc(shelf.Entries, func(e Entry) bool { return e.ID == id })
	return gone, nil
}

func (m Memory) nextID() string {
	given, _ := os.ReadFile(filepath.Join(m.Global.Dir, lastIDFile))
	highest, _ := strconv.Atoi(strings.TrimSpace(string(given)))
	for _, e := range append(slices.Clone(m.Global.Entries), m.Project.Entries...) {
		highest = max(highest, e.number())
	}
	return entryPrefix + strconv.Itoa(highest+1)
}

func readShelf(scope Scope, dir string) (Shelf, error) {
	shelf := Shelf{Scope: scope, Dir: dir}
	names, err := filepath.Glob(filepath.Join(dir, "*"+fileSuffix))
	if err != nil {
		return shelf, err
	}
	for _, name := range names {
		e, err := readEntry(name)
		if err != nil {
			return shelf, err
		}
		e.Scope = scope
		shelf.Entries = append(shelf.Entries, e)
	}
	slices.SortFunc(shelf.Entries, func(a, b Entry) int { return cmp.Compare(a.number(), b.number()) })
	return shelf, nil
}

func (e Entry) number() int {
	n, _ := strconv.Atoi(strings.TrimPrefix(e.ID, entryPrefix))
	return n
}

func (e Entry) encode() []byte {
	var out strings.Builder
	for _, field := range [][2]string{{"id", e.ID}, {"kind", string(e.Kind)}, {"text", e.Text}, {"said", e.Said}, {"session", e.Session}, {"at", e.At.Format(time.RFC3339)}, {"by", string(e.By)}} {
		if field[1] == "" {
			continue
		}
		value := field[1]
		if strings.ContainsAny(value, "\r\n\"") || strings.TrimSpace(value) != value {
			value = strconv.Quote(value)
		}
		out.WriteString(field[0] + ": " + value + "\n")
	}
	return []byte(out.String())
}

func readEntry(file string) (Entry, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{File: file}
	for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			value, err = strconv.Unquote(value)
		}
		if !found || err != nil {
			return Entry{}, fmt.Errorf("%s:%d: expected key: value", file, i+1)
		}
		switch strings.TrimSpace(key) {
		case "id":
			e.ID = value
		case "kind":
			e.Kind = Kind(value)
		case "text":
			e.Text = value
		case "said":
			e.Said = value
		case "session":
			e.Session = value
		case "at":
			e.At, err = time.Parse(time.RFC3339, value)
		case "by":
			e.By = By(value)
		default:
			return Entry{}, fmt.Errorf("%s:%d: unknown field %q", file, i+1, strings.TrimSpace(key))
		}
		if err != nil {
			return Entry{}, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
	}
	if want := strings.TrimSuffix(filepath.Base(file), fileSuffix); e.ID != want || e.Text == "" || !e.Kind.valid() {
		return Entry{}, fmt.Errorf("%s: an entry needs id %s, a kind of %s, %s or %s, and its text", file, want, KindPerson, KindProject, KindReference)
	}
	return e, nil
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
