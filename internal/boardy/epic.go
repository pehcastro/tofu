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
)

type Epic struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Done      bool   `json:"done"`
	Milestone string `json:"milestone,omitempty"`
	Text      string `json:"text,omitempty"`
}

type Milestone struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Due   time.Time `json:"due,omitzero"`
	Text  string    `json:"text,omitempty"`
}

type planFile struct {
	fields map[string]string
	text   string
}

func (s Store) Epics(key string) ([]Epic, error) {
	files, err := s.planFiles(key, "epics")
	epics := make([]Epic, 0, len(files))
	for _, file := range files {
		epics = append(epics, Epic{ID: file.fields["id"], Title: file.fields["title"], Done: file.fields["done"] == "true", Milestone: file.fields["milestone"], Text: file.text})
	}
	return epics, err
}

func (s Store) SaveEpic(key string, epic Epic) error {
	return s.savePlan(key, "epics", epic.ID, epic.Text, "title", epic.Title, "done", fmt.Sprint(epic.Done), "milestone", epic.Milestone)
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
	return s.savePlan(key, "milestones", milestone.ID, milestone.Text, "title", milestone.Title, "due", formatDay(milestone.Due))
}

func parseDay(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.DateOnly, raw)
}

func formatDay(day time.Time) string {
	if day.IsZero() {
		return ""
	}
	return day.Format(time.DateOnly)
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
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	front, body, found := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !found {
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
		b.WriteString(pairs[i] + ": " + strings.TrimSpace(pairs[i+1]) + "\n")
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
