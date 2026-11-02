package boardy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"tofu/internal/rule"
	"tofu/internal/skill"
)

type Epic struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Done      bool   `json:"done"`
	Milestone string `json:"milestone,omitempty"`
	Text      string `json:"text,omitempty"`
	Gated     bool   `json:"gated,omitempty"`
	Research  string `json:"research,omitempty"`
	Design    string `json:"design,omitempty"`
	First     string `json:"first,omitempty"`
}

type Milestone struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Due   Day    `json:"due,omitempty"`
	Text  string `json:"text,omitempty"`
}

type Day string

func DayOf(at time.Time) Day {
	if at.IsZero() {
		return ""
	}
	return Day(at.Format(time.DateOnly))
}

func parseDay(raw string) (Day, error) {
	if raw == "" {
		return "", nil
	}
	if _, err := time.Parse(time.DateOnly, raw); err != nil {
		return "", err
	}
	return Day(raw), nil
}

type planFile struct {
	fields map[string]string
	text   string
}

func (s Store) Epics(key string) ([]Epic, error) {
	files, err := s.planFiles(key, "epics")
	epics := make([]Epic, 0, len(files))
	for _, file := range files {
		epics = append(epics, Epic{ID: file.fields["id"], Title: file.fields["title"], Done: file.fields["done"] == "true", Milestone: file.fields["milestone"], Text: file.text,
			Gated: file.fields["gated"] == "true", Research: file.fields["research"], Design: file.fields["design"], First: file.fields["first"]})
	}
	return epics, err
}

func (s Store) SaveEpic(key string, epic Epic) error {
	epics, _ := s.Epics(key)
	at := slices.IndexFunc(epics, func(e Epic) bool { return e.ID == epic.ID })
	m := Managed{Local: Local{Store: s}}
	planned := subject{event: rule.HookEpicCreate, key: key, ticket: Ticket{Front: Front{Title: epic.Title, Epic: epic.ID}}}
	if at < 0 {
		if err := m.gate(&planned); err != nil {
			return err
		}
		hooks, err := s.hooks(key)
		if err != nil {
			return err
		}
		epic.Gated = slices.ContainsFunc(hooks, func(h rule.Hook) bool { return runsCheck(h, checkEpicReady) })
	}
	if err := s.savePlan(key, "epics", epic.ID, epic.Text, "title", epic.Title, "done", fmt.Sprint(epic.Done), "milestone", epic.Milestone,
		"gated", fmt.Sprint(epic.Gated), "research", epic.Research, "design", epic.Design, "first", epic.First); err != nil {
		return err
	}
	switch {
	case at < 0:
		return m.after(planned)
	case epic.Done && !epics[at].Done && epic.Milestone != "" && !slices.ContainsFunc(epics, func(e Epic) bool { return e.ID != epic.ID && e.Milestone == epic.Milestone && !e.Done }):
		planned.event, planned.ticket.Title = rule.HookMilestoneReach, epic.Milestone
		return m.after(planned)
	}
	return nil
}

func (s Store) Research(key, id, note string) (Epic, error) {
	if strings.TrimSpace(note) == "" {
		return Epic{}, errors.New("a research note says what the rivals do and the options it found")
	}
	return s.shaped(key, id, func(epic *Epic) { epic.Research = note })
}

func (s Store) Design(key, id, design, first string) (Epic, error) {
	if strings.TrimSpace(design) == "" || strings.TrimSpace(first) == "" {
		return Epic{}, errors.New("a design says the shape and names the smallest first version that runs")
	}
	return s.shaped(key, id, func(epic *Epic) { epic.Design, epic.First = design, first })
}

func (s Store) shaped(key, id string, change func(*Epic)) (Epic, error) {
	epics, err := s.Epics(key)
	at := slices.IndexFunc(epics, func(e Epic) bool { return e.ID == id })
	if at < 0 {
		return Epic{}, errors.Join(err, fmt.Errorf("%s has no epic %s: tofu boardy epic new %s <title>", key, id, id))
	}
	change(&epics[at])
	return epics[at], s.SaveEpic(key, epics[at])
}

func (s Store) Milestones(key string) ([]Milestone, error) {
	files, err := s.planFiles(key, "milestones")
	milestones := make([]Milestone, 0, len(files))
	for _, file := range files {
		due, dueErr := parseDay(file.fields["due"])
		err = errors.Join(err, dueErr)
		milestones = append(milestones, Milestone{ID: file.fields["id"], Title: file.fields["title"], Due: due, Text: file.text})
	}
	return milestones, err
}

func (s Store) SaveMilestone(key string, milestone Milestone) error {
	return s.savePlan(key, "milestones", milestone.ID, milestone.Text, "title", milestone.Title, "due", string(milestone.Due))
}

func validPlanID(id string) bool {
	if id == "" || !unicode.IsLetter(rune(id[0])) {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	})
}

func (s Store) planFiles(key, collection string) ([]planFile, error) {
	dir := filepath.Join(s.BoardDir(key), collection)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var files []planFile
	for _, entry := range entries {
		if entry.IsDir() || editorScratch(entry.Name()) || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		file, parseErr := parsePlan(raw)
		if want := strings.TrimSuffix(entry.Name(), ".md"); parseErr == nil && file.fields["id"] != want {
			parseErr = fmt.Errorf("its front matter says id %s", file.fields["id"])
		}
		if readErr = errors.Join(readErr, parseErr); readErr != nil {
			err = errors.Join(err, fmt.Errorf("%s/%s: %w", collection, entry.Name(), readErr))
			continue
		}
		files = append(files, file)
	}
	slices.SortFunc(files, func(a, b planFile) int { return strings.Compare(a.fields["id"], b.fields["id"]) })
	return files, err
}

func parsePlan(raw []byte) (planFile, error) {
	front, body, found := skill.SplitFrontMatter(string(raw))
	if !found {
		return planFile{}, errors.New("no front matter between two --- lines")
	}
	file := planFile{fields: map[string]string{}, text: strings.TrimSpace(body)}
	for line := range strings.Lines(front) {
		name, value, found := strings.Cut(line, ":")
		if !found {
			return file, fmt.Errorf("front matter line %q has no key", strings.TrimSpace(line))
		}
		file.fields[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return file, nil
}

func (s Store) savePlan(key, collection, id, text string, pairs ...string) error {
	if !validPlanID(id) {
		return fmt.Errorf("%q is not an id: letters, digits and dashes, starting with a letter", id)
	}
	var b strings.Builder
	b.WriteString("---\nid: " + id + "\n")
	for i := 0; i+1 < len(pairs); i += 2 {
		if value := strings.Join(strings.Fields(pairs[i+1]), " "); value != "" {
			b.WriteString(pairs[i] + ": " + value + "\n")
		}
	}
	b.WriteString("---\n")
	if text = strings.TrimSpace(text); text != "" {
		b.WriteString("\n" + text + "\n")
	}
	path := filepath.Join(s.BoardDir(key), collection, id+".md")
	return s.locked(key, collection+"-"+id, func() error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return writeAtomic(path, []byte(b.String()))
	})
}
