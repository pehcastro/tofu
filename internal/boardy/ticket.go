package boardy

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Status string

const (
	Triage  Status = "triage"
	Backlog Status = "backlog"
	Todo    Status = "todo"
	Doing   Status = "doing"
	Review  Status = "review"
	Done    Status = "done"
	Dropped Status = "dropped"
	Blocked Status = "blocked"
)

func Statuses() []Status {
	return []Status{Triage, Backlog, Todo, Doing, Review, Done, Dropped, Blocked}
}

func (Status) Enum() []string { return enum(Statuses()...) }

func enum[T ~string](values ...T) []string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = string(value)
	}
	return names
}

func ParseStatus(raw string) (Status, error) {
	if status := Status(raw); slices.Contains(Statuses(), status) {
		return status, nil
	}
	return "", fmt.Errorf("%q is not a status: %v", raw, Statuses())
}

func (s Status) Live() bool { return s != Done && s != Dropped }

type Kind string

const (
	Story   Kind = "story"
	Task    Kind = "task"
	Bug     Kind = "bug"
	Subtask Kind = "subtask"
)

func (Kind) Enum() []string { return enum(Story, Task, Bug, Subtask) }

func ParseKind(raw string) (Kind, error) {
	if kinds := Kind("").Enum(); !slices.Contains(kinds, raw) {
		return "", fmt.Errorf("%q is not a ticket type: %s", raw, strings.Join(kinds, ", "))
	}
	return Kind(raw), nil
}

type Priority string

const DefaultPriority Priority = "P2"

func (Priority) Enum() []string { return []string{"P0", "P1", "P2", "P3", "P4"} }

func ParsePriority(raw string) (Priority, error) {
	if !slices.Contains(Priority("").Enum(), raw) {
		return "", fmt.Errorf("%q is not a priority: P0 to P4", raw)
	}
	return Priority(raw), nil
}

type Front struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Type       Kind      `json:"type"`
	Status     Status    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	Assignee   string    `json:"assignee,omitempty"`
	Priority   Priority  `json:"priority"`
	Points     int       `json:"points,omitempty"`
	Owns       []string  `json:"owns,omitempty"`
	Depends    []string  `json:"depends,omitempty"`
	Blocks     []string  `json:"blocks,omitempty"`
	Relates    []string  `json:"relates,omitempty"`
	Duplicates []string  `json:"duplicates,omitempty"`
	Epic       string    `json:"epic,omitempty"`
	Sprint     string    `json:"sprint,omitempty"`
	Labels     []string  `json:"labels,omitempty"`
	Component  string    `json:"component,omitempty"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
}

type Ticket struct {
	Front
	Problem    string   `json:"problem"`
	Scope      string   `json:"scope"`
	Acceptance []string `json:"acceptance"`
	Log        string   `json:"log"`
}

func (t Ticket) Board() string { key, _, _ := strings.Cut(t.ID, "-"); return key }

var ticketID = regexp.MustCompile(`^([A-Z][A-Z0-9]*)-([1-9][0-9]*)$`)

func SplitID(id string) (key string, number int, err error) {
	match := ticketID.FindStringSubmatch(id)
	if match == nil {
		return "", 0, fmt.Errorf("%q is not a ticket id like DEMO-1", id)
	}
	number, err = strconv.Atoi(match[2])
	return match[1], number, err
}

func (t Ticket) Markdown() []byte {
	var b strings.Builder
	b.WriteString("---\n")
	scalar := func(key, value string) {
		b.WriteString(strings.TrimSpace(key+": "+strings.Join(strings.Fields(value), " ")) + "\n")
	}
	list := func(key string, values []string) {
		raw, _ := json.Marshal(append([]string{}, values...))
		scalar(key, string(raw))
	}
	scalar("id", t.ID)
	scalar("title", t.Title)
	scalar("type", string(t.Type))
	scalar("status", string(t.Status))
	scalar("reason", t.Reason)
	scalar("assignee", t.Assignee)
	scalar("priority", string(t.Priority))
	scalar("points", strconv.Itoa(t.Points))
	list("owns", t.Owns)
	list("depends", t.Depends)
	list("blocks", t.Blocks)
	list("relates", t.Relates)
	list("duplicates", t.Duplicates)
	scalar("epic", t.Epic)
	scalar("sprint", t.Sprint)
	list("labels", t.Labels)
	scalar("component", t.Component)
	scalar("created", t.Created.UTC().Format(time.RFC3339))
	scalar("updated", t.Updated.UTC().Format(time.RFC3339))
	b.WriteString("---\n")
	section := func(heading, body string) {
		fmt.Fprintf(&b, "\n## %s\n", heading)
		if body = strings.TrimSpace(body); body != "" {
			fmt.Fprintf(&b, "\n%s\n", body)
		}
	}
	section("Problem", t.Problem)
	section("Scope", t.Scope)
	if len(t.Acceptance) == 0 {
		section(RevisionHeading(0), "")
	}
	for i, revision := range t.Acceptance {
		section(RevisionHeading(i), revision)
	}
	section("Log", t.Log)
	return []byte(strings.ReplaceAll(b.String(), "\r\n", "\n"))
}

func RevisionHeading(index int) string {
	if index == 0 {
		return "Acceptance"
	}
	return "Acceptance " + strconv.Itoa(index+1)
}

func ParseTicket(raw []byte) (Ticket, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	front, body, found := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !found {
		return Ticket{}, errors.New("no front matter between two --- lines")
	}
	var t Ticket
	if err := t.readFront(front); err != nil {
		return Ticket{}, err
	}
	return t, t.readSections(body)
}

func (t *Ticket) readFront(front string) error {
	seen := map[string]bool{}
	for line := range strings.Lines(front) {
		key, value, found := strings.Cut(strings.TrimRight(line, "\n"), ":")
		value = strings.TrimSpace(value)
		if !found {
			return fmt.Errorf("front matter line %q has no key", strings.TrimSpace(line))
		}
		if seen[key] {
			return fmt.Errorf("front matter names %s twice", key)
		}
		seen[key] = true
		if err := t.set(key, value); err != nil {
			return fmt.Errorf("front matter %s: %w", key, err)
		}
	}
	for _, key := range []string{"id", "title", "type", "status", "priority"} {
		if !seen[key] {
			return fmt.Errorf("front matter has no %s", key)
		}
	}
	return nil
}

func (t *Ticket) set(key, value string) error {
	var err error
	list := func(into *[]string) error {
		if value == "" || value == "[]" {
			return nil
		}
		return json.Unmarshal([]byte(value), into)
	}
	switch key {
	case "id":
		t.ID = value
		_, _, err = SplitID(value)
	case "title":
		t.Title = value
	case "type":
		t.Type, err = ParseKind(value)
	case "status":
		t.Status, err = ParseStatus(value)
	case "reason":
		t.Reason = value
	case "assignee":
		t.Assignee = value
	case "priority":
		t.Priority, err = ParsePriority(value)
	case "points":
		t.Points, err = strconv.Atoi(value)
	case "owns":
		err = list(&t.Owns)
	case "depends":
		err = list(&t.Depends)
	case "blocks":
		err = list(&t.Blocks)
	case "relates":
		err = list(&t.Relates)
	case "duplicates":
		err = list(&t.Duplicates)
	case "epic":
		t.Epic = value
	case "sprint":
		t.Sprint = value
	case "labels":
		err = list(&t.Labels)
	case "component":
		t.Component = value
	case "created":
		t.Created, err = time.Parse(time.RFC3339, value)
	case "updated":
		t.Updated, err = time.Parse(time.RFC3339, value)
	default:
		err = errors.New("is not a ticket field")
	}
	return err
}

func (t *Ticket) readSections(body string) error {
	sections := map[string]string{}
	var heading string
	for line := range strings.Lines(body) {
		name, isHeading := strings.CutPrefix(strings.TrimRight(line, "\n"), "## ")
		switch {
		case isHeading:
			heading = strings.TrimSpace(name)
			if _, twice := sections[heading]; twice {
				return fmt.Errorf("the section %s appears twice", heading)
			}
			sections[heading] = ""
			switch heading {
			case "Problem", "Scope", "Log":
			case RevisionHeading(len(t.Acceptance)):
				t.Acceptance = append(t.Acceptance, "")
			default:
				return fmt.Errorf("the section %q is not Problem, Scope, Acceptance, Acceptance <n> in order, or Log", heading)
			}
		case heading != "":
			sections[heading] += line
		case strings.TrimSpace(line) != "":
			return fmt.Errorf("text before the first section: %q", strings.TrimSpace(line))
		}
	}
	for _, name := range []string{"Problem", "Scope", "Acceptance", "Log"} {
		if _, found := sections[name]; !found {
			return fmt.Errorf("the section %s is missing", name)
		}
	}
	for i := range t.Acceptance {
		t.Acceptance[i] = strings.TrimSpace(sections[RevisionHeading(i)])
	}
	t.Problem, t.Scope, t.Log = strings.TrimSpace(sections["Problem"]), strings.TrimSpace(sections["Scope"]), strings.TrimSpace(sections["Log"])
	return nil
}
